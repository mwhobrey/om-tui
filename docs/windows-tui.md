# Windows: om-tui (Rivers)

OpenMessage on Windows targets a terminal UI with **rivers** (account/workspace
instances). Google Messages is the built-in river; Slack workspaces are
additional rivers. There is no macOS app or React web UI on this path.

## Data directory

Default store location:

`%LOCALAPPDATA%\OpenMessage`

Override with `OPENMESSAGES_DATA_DIR`. Pairing, `serve`, and `tui` must share the same directory.

River credentials live under `rivers/<river_id>/credentials.enc`, sealed with
**Windows DPAPI** (tied to your Windows user). Restoring a backup on another
machine/user will not decrypt those blobs.

## Quick start

```powershell
go build -o openmessage.exe .
.\openmessage.exe pair
.\openmessage.exe tui
```

### Slack river

```powershell
.\openmessage.exe pair slack --token xoxp-... --name "Acme"
.\openmessage.exe serve --api --no-web   # or restart tui so it respawns the daemon
```

Use a Slack **user token** with scopes sufficient to list channels/DMs, read
history, and `chat:write` (e.g. `channels:history`, `channels:read`,
`groups:history`, `groups:read`, `im:history`, `im:read`, `mpim:history`,
`mpim:read`, `chat:write`, `users:read`). Tokens are stored only in the vault.

In the TUI, press `[` / `]` to switch rivers. Streams (DMs / `#channels`) appear
in the list for the active river.

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

`--api` exposes REST + SSE (`/api/status`, `/api/conversations?river_id=…`, `/api/rivers`, `/api/events`, …) without the React static UI.

## TUI keys

| Key | Action |
|---|---|
| `[` / `]` | Switch river (Messages, Slack workspaces, …) |
| `j` / `k` / arrows | Move in the conversation list; in **thread** focus, select a message |
| mouse wheel | Scroll the pane under the cursor (list or thread). Mouse capture also prevents Windows Terminal from smearing the alt-screen buffer. |
| `/` | Jump: filter the left column by name / `#channel` / contact / id; **Enter** opens; **Esc** clears |
| `Ctrl+F` | Search message text (full-pane results; Enter opens the hit’s thread) |
| `Space` | Multi-select conversations for broadcast (`*` mark) |
| `m` | Focus composer to message all selected chats |
| `c` | Clear multi-select |
| `Enter` | Open thread / send text (to all selected chats if multi-select is active) / send file path / run message search |
| `Tab` | Cycle list → thread → composer |
| `e` | React (thread focus): open emoji palette, then `1`–`9` to add/remove |
| `a` | Attach file (Windows picker). In the composer use `Ctrl+A` so you can still type `a`. Composer text becomes the caption. |
| `Ctrl+V` | Paste media from the OS clipboard (Explorer file copy or screenshot/image) and send; falls back to pasting text into the composer |
| `o` / `Ctrl+O` | Open latest media (or the selected message in thread focus). Works from list/thread; in the composer when the draft is empty (or always via Ctrl+O). |
| `s` / `Ctrl+S` | Save media (same focus rules as open). |
| `r` | Reconnect Google Messages |
| `Esc` | Back to conversation list / clear jump filter / leave search |
| `q` / `Ctrl+C` | Quit |

Unread counts show in the list; opening a thread marks it read. Inbound senders get distinct colors (your messages stay cyan). Composer drafts are kept per conversation while the TUI is open (switching threads restores them; send clears that draft). Long message bodies and the composer wrap within the thread pane (`Enter` still sends; use `Alt+Enter` for a newline if your terminal maps it through).

### Broadcast (multi-send)

In the **conversation list**:

1. **`Space`** — toggle the highlighted chat (`*` prefix)
2. Select as many as you want
3. **`m`** (or `Tab` to composer) — write one message
4. **`Enter`** — sends that text to every selected chat
5. **`c`** or **`Esc`** — clear the selection

Broadcast is text-only in v1 (clear selection to send media to a single open thread).

## Media

Attachments render as typed placeholders: `[image]`, `[video]`, `[audio]`, or `[file]`, plus any caption text.

### Receive / open

- **`o`** downloads via `GET /api/media/{message_id}` into a temp cache and opens with the default app (`cmd /c start` on Windows).
- **`s`** downloads to `%USERPROFILE%\Documents\OpenMessage\media\` (override root with `OPENMESSAGES_EXPORT_DIR`). The status line shows the saved path.
- Target is the **selected** media message in thread focus (`j`/`k`), otherwise the
  latest downloadable attachment in the loaded thread. MimeType-only stubs are not
  downloadable; text/empty rows no longer silently fall back to another message.

### Send

Uses the daemon's media send (`/api/v1/outbox/media` or legacy `/api/send-media`).

- **`a` / `Ctrl+A`** — Windows file picker; optional caption = current composer text.
- **Path + Enter** — if the composer is an existing local file path (quotes/`~`/`%USERPROFILE%` ok), Enter sends that file.
- **`Ctrl+V`** — reads the OS clipboard: Explorer-copied file first, else image/screenshot saved to a temp PNG, else text inserted into the composer.

Inline terminal previews remain out of scope.

### Reactions

In **thread** focus (`Tab` onto the message pane):

- **`j` / `k`** — select a message (highlighted with `>`)
- **`e`** — open the Google Messages emoji palette (status line shows `1👍 2❤️ …`)
- **`1`–`9`** — add that reaction; press the same slot again to remove
- **`Esc`** — close the palette

Existing reactions render inline with reactor names when known (e.g. `👍 Alice ❤️ you`); otherwise they keep the compact count form (`👍2`).

## Smoke checklist

1. `go build -o openmessage.exe .`
2. `.\openmessage.exe serve --api --no-web` in one terminal (temp `OPENMESSAGES_DATA_DIR` is fine)
3. `curl http://127.0.0.1:7007/api/status` returns JSON (not the web app HTML)
4. `.\openmessage.exe tui` attaches (“Attached to local API daemon…”)
5. Quit TUI — standalone daemon still running
6. Stop daemon; run `tui` alone — it spawns `serve --api` and shows conversations once paired
7. Press `r` when disconnected; unpaired state tells you to run `openmessage pair`
8. In a thread with an image: `o` opens Photos (or default viewer); `s` writes under Documents\OpenMessage\media
9. Attach: `a` (or `Ctrl+A` in composer) opens a file picker and sends; paste a path and Enter; or copy a file/screenshot and `Ctrl+V`
10. Thread focus: `k`/`j` select a message, `e` then `1` reacts; same digit again removes

## Notifications

`serve --api` shows Windows toast notifications for fresh inbound Google Messages (daemon-side, so they work even when the TUI is unfocused or closed).

- Respects per-conversation `notification_mode` (`all` / `mentions` / `muted`)
- Skips outgoing messages and duplicates by message id
- Default **on** on Windows; set `OPENMESSAGES_WINDOWS_NOTIFICATIONS=0` to disable, or `=1` to force on
- Optional `OPENMESSAGES_WINDOWS_TOAST_APP_ID` overrides the toast AppUserModelID (defaults to PowerShell's registered AUMID so toasts actually show; unregistered IDs like `OpenMessage.Cli` often silently no-op)
- Restart the API daemon after rebuilding so it picks up notifier changes (`tui` will respawn an owned child)

Unread badges in the conversation list come from `unread_count`. Fresh inbound Google messages increment it; opening a thread (or receiving while that thread is open) clears it via `/api/mark-read`.

## Non-goals on Windows

- Native GUI / tray app
- iMessage, WhatsApp live, Signal
- Inline Kitty/sixel/ASCII media thumbs (for now)
- Signed MSI installer
