# om-tui Google Cookies (Chrome MV3)

Unpacked Chrome extension that keeps a native-messaging port open so the
om-tui daemon can pull live Google Messages cookies when auth expires — no
DevTools paste, as long as Chrome is open and signed in.

Required cookies (mautrix-gmessages Google Account fields):

- `SID` / `HSID` / `SSID` / `APISID` / `SAPISID` on `.google.com`
- `OSID` on `messages.google.com`

Optional when present: `__Secure-1PSIDTS` and related `__Secure-*PSID*` cookies.

Stable extension ID (from the embedded `key`): `akakhclmanbjmbojfbjcnakfbmobinee`

## Install (Windows dogfood)

1. Build om-tui and register the native host:

```powershell
go build -o om-tui.exe .
.\om-tui.exe chrome-cookie-host --install
```

2. Chrome → `chrome://extensions` → Developer mode → **Load unpacked** → select
   this directory (`extensions/google-cookies`). After updates, hit **Reload**
   on the extension card and accept any new host-permission prompt — apex
   `https://google.com/` is required (`*.google.com` alone misses host-only
   Gaia cookies like SID/HSID/APISID).

3. Keep Chrome signed in to the Google account that owns Messages for web.
   Open `https://messages.google.com` once in that profile so OSID exists
   alongside the five Gaia cookies. Leave Chrome running while you use om-tui.

4. Confirm `/api/status` shows `"cookie_bridge_online": true` under `google`
   while the extension is connected (daemon must be up).

The toolbar button is optional (force-push for debugging). Auto-heal is the
primary path: credential repair and the TUI call the bridge when
`auth_expired` (and not yet `needs_repair`).

## Failure modes

| Situation | Result |
|---|---|
| Chrome quit / extension disabled | Bridge offline; paste overlay still works |
| Not signed in / never opened Messages web | Bridge replies with missing-cookie error (often `OSID`); paste fallback |
| Same Chrome cookies still 401 twice | Daemon exhausts auto-refresh → paste |
| Daemon not running | Host backs off; reconnects when serve is up |
| Unpaired install | Paste / Gaia path unchanged |

Cookie values are never logged.
