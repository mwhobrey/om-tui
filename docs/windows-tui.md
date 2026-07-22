# Windows: Google Messages TUI

OpenMessage on Windows targets **Google Messages** with a terminal UI. There is no macOS app, iMessage, or React web UI on this path.

## Data directory

Default store location:

`%LOCALAPPDATA%\OpenMessage`

Override with `OPENMESSAGES_DATA_DIR`. Pairing, `serve`, and `tui` must share the same directory.

## Quick start

```powershell
go build -o openmessage.exe .
.\openmessage.exe pair
.\openmessage.exe tui
```

`tui` attaches to a local API daemon on `http://127.0.0.1:7007` (or `OPENMESSAGES_PORT`). If none is running, it starts:

```text
openmessage serve --api --no-web
```

and stops that child when you quit the TUI. A daemon you started yourself is left running.

## Standalone API daemon

```powershell
.\openmessage.exe serve --api --no-web
.\openmessage.exe tui   # attaches; does not spawn a second daemon
```

`--api` exposes REST + SSE (`/api/status`, `/api/conversations`, `/api/events`, …) without the React static UI.

## TUI keys

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move in the conversation list |
| `Enter` | Open thread / send compose / run search |
| `Tab` | Cycle list → thread → composer |
| `/` | Search |
| `o` | Open latest media in the thread (default Windows app) |
| `s` | Save latest media under Documents (or `OPENMESSAGES_EXPORT_DIR`) |
| `r` | Reconnect Google Messages |
| `Esc` | Back to conversation list / leave search |
| `q` / `Ctrl+C` | Quit |

Unread counts show in the list; opening a thread marks it read.

## Media

Attachments render as typed placeholders: `[image]`, `[video]`, `[audio]`, or `[file]`, plus any caption text.

- **`o`** downloads via `GET /api/media/{message_id}` into a temp cache and opens with the default app (`cmd /c start` on Windows).
- **`s`** downloads to `%USERPROFILE%\Documents\OpenMessage\media\` (override root with `OPENMESSAGES_EXPORT_DIR`). The status line shows the saved path.
- Target is the **latest media message** in the loaded thread (no picker yet).

Inline terminal previews and media *send* are out of scope for this path.

## Smoke checklist

1. `go build -o openmessage.exe .`
2. `.\openmessage.exe serve --api --no-web` in one terminal (temp `OPENMESSAGES_DATA_DIR` is fine)
3. `curl http://127.0.0.1:7007/api/status` returns JSON (not the web app HTML)
4. `.\openmessage.exe tui` attaches (“Attached to local API daemon…”)
5. Quit TUI — standalone daemon still running
6. Stop daemon; run `tui` alone — it spawns `serve --api` and shows conversations once paired
7. Press `r` when disconnected; unpaired state tells you to run `openmessage pair`
8. In a thread with an image: `o` opens Photos (or default viewer); `s` writes under Documents\OpenMessage\media

## Next up (backlog)

- **Windows toast notifications** for incoming messages (macOS already has `internal/notify`). Prefer daemon-side toasts from `serve --api` so they work without the TUI focused; respect conversation mute / `notification_mode`. Hook via existing SSE `messages` events or the Google event handler.

## Non-goals on Windows

- Native GUI / tray app
- iMessage, WhatsApp live, Signal
- Inline Kitty/sixel/ASCII media thumbs (for now)
- Media send from the TUI
- Signed MSI installer
