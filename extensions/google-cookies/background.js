// Native messaging host registered by `om-tui chrome-cookie-host --install`.
const NATIVE_HOST = "com.mwhobrey.om_tui.cookies";

// Required by mautrix-gmessages Google Account login / cookie reauth:
// five .google.com Gaia cookies + messages.google.com OSID.
const REQUIRED = ["SID", "HSID", "SSID", "APISID", "SAPISID", "OSID"];

// Helpful when present (mautrix lists __Secure-1PSIDTS on .google.com).
const OPTIONAL = [
  "__Secure-1PSIDTS",
  "__Secure-1PSID",
  "__Secure-3PSID",
  "__Secure-1PSIDCC",
  "__Secure-3PSIDCC",
];

const WANTED = REQUIRED.concat(OPTIONAL);

// *.google.com match patterns do NOT include the apex host. Query both.
const COOKIE_QUERIES = [
  { url: "https://google.com/" },
  { url: "https://www.google.com/" },
  { url: "https://accounts.google.com/" },
  { url: "https://messages.google.com/" },
  { domain: "google.com" },
  { domain: ".google.com" },
];

const RECONNECT_ALARM = "om-tui-cookie-bridge-reconnect";
const RECONNECT_MINUTES = 1;
const FORCE_PUSH_TIMEOUT_MS = 25000;

let port = null;
let connecting = false;
let lastDisconnectError = "";
const pendingForce = new Map();

chrome.runtime.onInstalled.addListener(() => {
  ensureConnected();
});

chrome.runtime.onStartup.addListener(() => {
  ensureConnected();
});

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === RECONNECT_ALARM) {
    ensureConnected();
  }
});

chrome.alarms.create(RECONNECT_ALARM, { periodInMinutes: RECONNECT_MINUTES });

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message?.op === "forcePush") {
    forcePushToDaemon()
      .then(() => sendResponse({ ok: true }))
      .catch((err) => {
        sendResponse({ ok: false, error: String(err?.message || err) });
      });
    return true;
  }
  if (message?.op === "status") {
    ensureConnected();
    sendResponse({
      connected: port !== null,
      lastError: lastDisconnectError || "",
    });
    return false;
  }
  return false;
});

function ensureConnected() {
  if (port || connecting) {
    return;
  }
  connecting = true;
  lastDisconnectError = "";
  try {
    port = chrome.runtime.connectNative(NATIVE_HOST);
  } catch (err) {
    connecting = false;
    port = null;
    lastDisconnectError = String(err?.message || err);
    console.warn("om-tui cookie bridge: connectNative failed", lastDisconnectError);
    return;
  }
  connecting = false;

  port.onMessage.addListener((msg) => {
    handleHostMessage(msg).catch((err) => {
      console.warn("om-tui cookie bridge: host message failed", String(err));
    });
  });

  port.onDisconnect.addListener(() => {
    const err = chrome.runtime.lastError?.message;
    if (err) {
      lastDisconnectError = err;
      console.warn("om-tui cookie bridge: disconnected", err);
    } else {
      lastDisconnectError = "disconnected";
    }
    port = null;
    for (const [, reject] of pendingForce) {
      reject(new Error("native host disconnected during push"));
    }
    pendingForce.clear();
  });

  try {
    port.postMessage({ op: "hello" });
  } catch (err) {
    lastDisconnectError = String(err?.message || err);
    port = null;
  }
}

async function handleHostMessage(msg) {
  if (!msg || typeof msg !== "object") {
    return;
  }
  if (msg.op === "forceCookiesResult") {
    const entry = pendingForce.get(msg.id || "");
    if (entry) {
      pendingForce.delete(msg.id || "");
      if (msg.error) {
        entry.reject(new Error(msg.error));
      } else {
        entry.resolve();
      }
    }
    return;
  }
  if (msg.op === "getGoogleCookies") {
    const id = msg.id || "";
    try {
      const cookies = await collectCookies();
      port?.postMessage({ op: "getGoogleCookiesResult", id, cookies });
    } catch (err) {
      port?.postMessage({
        op: "getGoogleCookiesResult",
        id,
        error: String(err?.message || err),
      });
    }
  }
}

async function forcePushToDaemon() {
  const cookies = await collectCookies();
  const nativePort = await waitForNativePort();
  const id = crypto.randomUUID();
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pendingForce.delete(id);
      reject(
        new Error(
          "daemon push timed out (is `om-tui serve --api` running with the cookie-bridge build?)"
        )
      );
    }, FORCE_PUSH_TIMEOUT_MS);
    pendingForce.set(id, {
      resolve: () => {
        clearTimeout(timer);
        resolve();
      },
      reject: (err) => {
        clearTimeout(timer);
        reject(err);
      },
    });
    try {
      nativePort.postMessage({ op: "forceCookies", id, cookies });
    } catch (err) {
      clearTimeout(timer);
      pendingForce.delete(id);
      reject(err);
    }
  });
}

async function collectCookies() {
  const rows = await loadGoogleCookieRows();
  const byName = new Map();
  for (const row of rows) {
    if (!WANTED.includes(row.name) || !row.value) {
      continue;
    }
    const existing = byName.get(row.name);
    const score = hostScore(row.name, row.domain);
    if (!existing || score < existing.score) {
      byName.set(row.name, { value: row.value, score });
    }
  }
  const cookies = {};
  const missing = [];
  const found = [];
  for (const name of REQUIRED) {
    const hit = byName.get(name);
    if (!hit) {
      missing.push(name);
      continue;
    }
    found.push(name);
    cookies[name] = hit.value;
  }
  for (const name of OPTIONAL) {
    const hit = byName.get(name);
    if (hit) {
      cookies[name] = hit.value;
    }
  }
  if (missing.length) {
    // Names only — never log cookie values.
    throw new Error(
      "missing Google cookies (sign in to Google in this Chrome profile, then open https://messages.google.com once): " +
        missing.join(", ") +
        (found.length ? " (have: " + found.join(", ") + ")" : "")
    );
  }
  return cookies;
}

function waitForNativePort(timeoutMs = 3000) {
  ensureConnected();
  if (port) {
    return Promise.resolve(port);
  }
  return new Promise((resolve, reject) => {
    const started = Date.now();
    const timer = setInterval(() => {
      ensureConnected();
      if (port) {
        clearInterval(timer);
        resolve(port);
        return;
      }
      if (Date.now() - started >= timeoutMs) {
        clearInterval(timer);
        const detail = lastDisconnectError || "host exited or is not installed";
        reject(
          new Error(
            "native host not connected (" +
              detail +
              "). Rebuild om-tui, run: om-tui chrome-cookie-host --install, then Reload the extension."
          )
        );
      }
    }, 50);
  });
}

async function loadGoogleCookieRows() {
  const seen = new Set();
  const rows = [];
  const queries = COOKIE_QUERIES.slice();
  for (const name of WANTED) {
    queries.push({ name });
  }
  for (const query of queries) {
    let batch = [];
    try {
      batch = await chrome.cookies.getAll(query);
    } catch (_) {
      continue;
    }
    for (const row of batch) {
      if (!row?.name || !isGoogleCookieDomain(row.domain)) {
        continue;
      }
      const key =
        row.name +
        "\0" +
        row.domain +
        "\0" +
        (row.path || "/") +
        "\0" +
        String(!!row.partitionKey);
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      rows.push(row);
    }
  }
  return rows;
}

function isGoogleCookieDomain(domain) {
  const d = String(domain || "")
    .toLowerCase()
    .replace(/^\./, "");
  return d === "google.com" || d.endsWith(".google.com");
}

// Lower score wins. Gaia five must come from .google.com (mautrix domains);
// OSID must come from messages.google.com.
function hostScore(name, domain) {
  const d = String(domain || "").toLowerCase();
  if (name === "OSID") {
    if (d === "messages.google.com" || d === ".messages.google.com") return 0;
    return 9;
  }
  if (d === ".google.com" || d === "google.com") return 0;
  if (d === "accounts.google.com" || d === ".accounts.google.com") return 2;
  if (d === "messages.google.com" || d === ".messages.google.com") return 5;
  return 9;
}

ensureConnected();
