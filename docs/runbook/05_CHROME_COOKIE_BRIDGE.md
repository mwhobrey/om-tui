# Chrome cookie bridge (auto-refresh)

Windows dogfood path that replaces the every-few-hours DevTools paste ritual
when Chrome is open and signed in. Paste remains the fallback.

## Components

| Piece | Location |
|---|---|
| MV3 extension | `extensions/google-cookies/` |
| Native messaging host | `om-tui chrome-cookie-host` (+ `--install`) |
| Daemon registry | `internal/cookiebridge` + `/api/google/cookie-bridge/*` |
| Self-heal hook | `canRefreshGoogleCookies` / `refreshGoogleSessionCookies` in `cmd/serve.go` |
| TUI immediate refresh | When `cookie_bridge_online` and `auth_expired`/`needs_repair` |

Native host name: `com.mwhobrey.om_tui.cookies`  
Stable extension ID: `akakhclmanbjmbojfbjcnakfbmobinee`

## Install

```powershell
go build -o om-tui.exe .
.\om-tui.exe chrome-cookie-host --install
```

Then Chrome → `chrome://extensions` → Developer mode → Load unpacked →
`extensions/google-cookies/`. Reload the extension after updates; accept the
apex `https://google.com/` host permission if prompted (`*.google.com` alone
misses host-only Gaia cookies). Open `https://messages.google.com` once in the
signed-in profile so SID/HSID/SSID/APISID/SAPISID **and** messages.google.com
`OSID` exist.

Keep `serve --api` (or `tui`, which spawns it) running. The extension initiates
`connectNative` and long-polls the daemon; the daemon never cold-starts Chrome.

## Timings (not a polling cycle)

| Path | Latency |
|---|---|
| Toolbar **Push cookies now** | Immediate: host POSTs `/api/google/cookie-bridge/push`, daemon rewrites `session.json` and rebuilds the Google supervisor (~1–5s if healthy; up to ~2.5m connect timeout if Google is wedged) |
| TUI sees `auth_expired` + `cookie_bridge_online` (and not `needs_repair`) | Immediate refresh (retries at most every 60s if it fails) |
| Supervisor credential repair | On auth failure; paced at **90s** min interval (`OPENMESSAGE_REPAIR_MIN_INTERVAL`) |
| Bridge cookie pull | **~3s** timeout per request |
| Extension alarm | Every **1m** — only reconnects the native port; does **not** push cookies |
| Same bridge cookies rejected twice | `canRefresh` flips false → `needs_repair` → paste (stops “refreshing via Chrome…” loops) |

There is no “wait for the next cycle” after a toolbar push. If the popup used to say that, it was lying — push either applied or failed.

## Heal order

1. `OPENMESSAGE_COOKIE_REFRESH_SCRIPT` if set
2. Extension bridge (≈3s timeout) when the host is waiting
3. `googlecookies.Refresh` (v10 / macOS keychain decrypt)
4. Paste overlay / `needs_repair` (unchanged)

`canRefreshGoogleCookies` is true when the bridge is **currently online**, not
merely when Chrome is installed.

## Failure modes

- Chrome quit → bridge offline → paste still offered
- Signed out of Google in Chrome → bridge error → paste
- Extension disabled / host not installed → same
- Bridge returns cookies Google still rejects (device unlinked / wrong
  account) → second identical pull exhausts auto-refresh → `needs_repair`
  and paste; chip says “press p to paste”, not “refreshing via Chrome…”
- Host exits immediately on connect → usually Chrome's Windows `--parent-window`
  arg; current host ignores it and `--install` writes a `.cmd` wrapper. Rebuild,
  re-run `chrome-cookie-host --install`, Reload the extension
- Unpaired → Gaia / paste path unchanged
- Do **not** allowlist `chrome-extension://` against general `/api/*`
  (`ProtectLocalControl` stays sealed; only the native host uses the control token)

## Verify

1. Host installed + extension loaded + Chrome signed in
2. `/api/status` → `google.cookie_bridge_online: true`
3. Expire / kill session cookies → supervisor or TUI refresh restores without paste
4. Quit Chrome → refresh fails; paste overlay still appears
