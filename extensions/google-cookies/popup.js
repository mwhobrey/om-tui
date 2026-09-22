const statusEl = document.getElementById("status");
const pushBtn = document.getElementById("push");

function setStatus(text) {
  statusEl.textContent = text;
}

chrome.runtime.sendMessage({ op: "status" }, (resp) => {
  if (chrome.runtime.lastError) {
    setStatus(chrome.runtime.lastError.message);
    return;
  }
  if (resp?.connected) {
    setStatus("Bridge connected");
    return;
  }
  setStatus(
    resp?.lastError
      ? "Bridge offline: " + resp.lastError
      : "Bridge offline — rebuild + chrome-cookie-host --install, then Reload"
  );
});

pushBtn.addEventListener("click", () => {
  setStatus("Pushing to daemon…");
  chrome.runtime.sendMessage({ op: "forcePush" }, (resp) => {
    if (chrome.runtime.lastError) {
      setStatus(chrome.runtime.lastError.message);
      return;
    }
    if (!resp?.ok) {
      setStatus(resp?.error || "push failed");
      return;
    }
    setStatus("Applied — session cookies rewritten + reconnect started");
  });
});
