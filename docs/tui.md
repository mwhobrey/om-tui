# TUI + rivers

om-tui's only client is a Bubble Tea terminal UI with **rivers**
(account/workspace instances). Google Messages is the built-in river; Slack
workspaces are additional rivers. It runs on Windows, macOS, and Linux.

## Data directory

Default store location:

- Windows: `%LOCALAPPDATA%\OpenMessage`
- macOS/Linux: `~/.local/share/openmessage`

Override with `OPENMESSAGES_DATA_DIR`. Pairing, `serve`, and `tui` must
share the same directory.

River credentials live under `rivers/<river_id>/credentials.enc`. Sealing
is OS-backed and fails closed if the backing tool is missing:

- **Windows:** DPAPI (tied to your Windows user). Restoring a backup on
  another machine/user will not decrypt those blobs.
- **macOS:** login Keychain via the `security` CLI (`om-tui` /
  `river-vault-key`).
- **Linux:** Secret Service via `secret-tool` (install `libsecret-tools`
  and a provider such as gnome-keyring).

`OPENMESSAGES_VAULT_INSECURE=1` is the test-only fallback (machine-local
file-derived key in `internal/vault/seal_other.go`). Do not set it outside
local testing. Google Messages pairing itself (`session.json`) is not
sealed by this vault.

The macOS/Linux backends are shipped in tree and cross-compile-checked;
they have not yet been runtime-smoked on real hardware.

## Quick start

```bash
go build -o om-tui .        # om-tui.exe on Windows
./om-tui tui                # unpaired: press p, paste a messages.google.com curl with ctrl+v, tap the emoji
```

CLI pairing still works: `Get-Clipboard | ./om-tui pair --google` (Windows) or `pbpaste | ./om-tui pair --google`.

### Slack river

```bash
./om-tui pair slack --token xoxp-... --name "Acme"
# Optional realtime push (tokens must come from the same Slack app):
./om-tui pair slack --token xoxp-... --app-token xapp-... --name "Acme"
./om-tui serve --api --no-web   # or restart tui so it respawns the daemon
```

Use a Slack **user token** with scopes sufficient to list channels/DMs, read
history, and `chat:write` (e.g. `channels:history`, `channels:read`,
`groups:history`, `groups:read`, `im:history`, `im:read`, `mpim:history`,
`mpim:read`, `chat:write`, `users:read`). Tokens are stored only in the vault.

om-tui caches Slack profiles locally so DMs, senders, `<@mentions>`, and
channel references render as names instead of IDs. The cache refreshes from
`users.list` and falls back to `users.info`.

For realtime push, enable Socket Mode on the same Slack app, create an app-level
token with `connections:write`, and subscribe that app to `message.channels`,
`message.groups`, `message.im`, and `message.mpim`. Pass its `xapp-…` token with
`--app-token`. Socket Mode is optional: startup reconciliation and the 45-second
incremental poll remain active as a fallback.

In the TUI, press `[` / `]` to switch rivers. Streams (DMs / `#channels`) appear
in the list for the active river.

`tui` attaches to a local API daemon on `http://127.0.0.1:7007` (or `OPENMESSAGES_PORT`). If none is running, it starts:

```text
om-tui serve --api --no-web
```

and stops that child when you quit the TUI, including when you close the
terminal window. A daemon you started yourself with `serve` is left running.

## Standalone API daemon

```bash
./om-tui serve --api --no-web
./om-tui tui   # attaches; does not spawn a second daemon
```

`--api` exposes REST + SSE (`/api/status`, `/api/conversations?river_id=…`, `/api/rivers`, `/api/events`, …).

## TUI keys

Opening a conversation focuses the **composer**. Message actions that must work
there use **Ctrl** chords — bare letters type into the draft, and Shift is not a
usable modifier in some terminals for these panes.

The footer is **context-aware** (only shows chords that apply to the current
focus / river / Slack-thread / react-palette state) and always ends with
`ctrl+k commands`. Press **`Ctrl+K`** from any focus for the universal palette:

- **Jump (default):** type a name / `#channel` / `type:dm` / `is:unread` to fuzzy
  filter conversations across **all rivers**. Enter switches river if needed and
  opens the thread. Empty query ranks by frecency (then unread / recency).
- **Commands:** prefix with `>` (e.g. `>react`, `>msg alice::omw`) to run the
  same handlers as the chord table, plus optional user commands from
  `commands.json` in the data dir.
- While open, keys are exclusive to the palette (letters do not leak into the
  composer). `/` remains the active-river list jump filter.

| Key | Action |
|---|---|
| `Ctrl+K` | Universal palette (jump or `>` commands; `j`/`k`/arrows move; Enter runs; Esc closes) |
| `[` / `]` | Switch river (Messages, Slack workspaces, …) — list focus |
| `j` / `k` / arrows | Move in the conversation list; in **thread** focus, select a message |
| mouse wheel | Scroll the pane under the cursor (list or thread) |
| `/` | Jump: filter by name / `#channel` / contact / id. Slack also accepts `type:dm`, `type:channel`, `is:unread`, and combinations such as `type:dm is:unread alice`. |
| `Ctrl+F` | Search message text (works from list / thread / composer) |
| `Space` | Multi-select conversations for broadcast (`*` mark) — list focus |
| `m` | Focus composer to message all selected chats — list focus |
| `c` | Clear multi-select — list focus |
| `Enter` | Open thread / send text (to all selected chats if multi-select is active) / send file path / run message search |
| `Tab` | Cycle list → thread → composer |
| `Ctrl+T` | Open the selected Slack message's dedicated reply thread |
| `PgUp` / `Ctrl+U` | Fetch an older page for the active Slack channel (composer or thread focus) |
| `Ctrl+E` | React: open emoji palette on the selected message, then `1`–`9` to add/remove (composer or thread; bare `e` only in thread focus) |
| `Ctrl+A` | Attach file (OS file picker on Windows; drop a path elsewhere). Composer text becomes the caption. Bare `a` works from list/thread only. |
| `p` | Pair Google Messages when unpaired (overlay: paste a `messages.google.com` curl with `Ctrl+V`, then tap the emoji on your phone) |
| `Ctrl+V` | Pair overlay: paste cookies. Otherwise paste media from the OS clipboard and send; falls back to pasting text into the composer |
| `Ctrl+O` | Open latest media (or the selected message in thread focus) with the OS default app (`open` on macOS, `xdg-open` on Linux, `cmd /c start` on Windows). Bare `o` works from list/thread only — in the composer letters always type. |
| `Ctrl+S` | Save media (same focus rules as open) |
| `Ctrl+R` / `r` | Reconnect Google Messages (`r` from list/thread; `Ctrl+R` also from composer) |
| `Esc` | Return from a Slack reply thread to its channel / back to conversation list / clear jump filter / leave search |
| `q` / `Ctrl+C` | Quit |

Unread counts show in the list; opening a thread marks it read. Inbound senders get distinct colors (your messages stay cyan). Composer drafts are kept per conversation while the TUI is open (switching threads restores them; send clears that draft). Long message bodies and the composer wrap within the thread pane; the composer grows up to 6 rows and scrolls with the cursor so the caret stays visible (`Enter` still sends; use `Alt+Enter` for a newline if your terminal maps it through).

### Broadcast (multi-send)

In the **conversation list**:

1. **`Space`** — toggle the highlighted chat (`*` prefix)
2. Select as many as you want
3. **`m`** (or `Tab` to composer) — write one message
4. **`Enter`** — sends that text to every selected chat
5. **`c`** or **`Esc`** — clear the selection

Broadcast is text-only in v1 (clear selection to send media to a single open thread).

### Palette files (data dir)

Under the data dir (see above):

- `palette-frecency.json` — auto-written usage scores for jump targets and commands
- `commands.json` — optional user shortcuts (reloaded each time the palette opens):

```json
{
  "commands": [
    {
      "id": "omw-alice",
      "label": "OMW Alice",
      "keywords": ["omw", "alice"],
      "action": "send",
      "river_id": "messages-default",
      "conversation_id": "…",
      "body": "omw"
    },
    {
      "id": "standup",
      "label": "Post standup",
      "keywords": ["standup"],
      "action": "send",
      "river_id": "slack-T…",
      "conversation_id": "slack:…",
      "body": "{{args}}",
      "accepts_args": true
    }
  ]
}
```

`action` is `send` or `open`. Quick send from the palette: `>msg contact::message text`.

## Media

Attachments render as typed placeholders: `[image]`, `[video]`, `[audio]`, or `[file]`, plus any caption text.

### Receive / open

- **`Ctrl+O`** downloads via `GET /api/media/{message_id}` into a temp cache and opens with the OS default app. Bare `o` works from list/thread only.
- **`Ctrl+S`** downloads to `<data dir>/media/` under your documents/export dir (override root with `OPENMESSAGES_EXPORT_DIR`). The status line shows the saved path.
- Target is the **selected** media message in thread focus (`j`/`k`), otherwise the
  latest downloadable attachment in the loaded thread. MimeType-only stubs are not
  downloadable; text/empty rows no longer silently fall back to another message.

### Send

Uses the daemon's media send (`/api/v1/outbox/media` or legacy `/api/send-media`).

- **`Ctrl+A`** — file picker (Windows) or path entry; optional caption = current composer text. Bare `a` from list/thread only.
- **Path + Enter** — if the composer is an existing local file path (quotes/`~` ok), Enter sends that file.
- **`Ctrl+V`** — reads the OS clipboard: copied file first, else image/screenshot saved to a temp PNG, else text inserted into the composer.

Inline terminal previews remain out of scope.

### Reactions

From the **composer** (default after opening a chat) or **thread** focus:

- **`j` / `k`** — select a message first (Tab to thread pane, or leave selection on the latest)
- **`Ctrl+E`** — open the Google Messages emoji palette (status line shows `1👍 2❤️ …`). Bare `e` also works in thread focus only.
- **`1`–`9`** — add that reaction; press the same slot again to remove
- **`Esc`** — close the palette

Existing reactions render inline with reactor names when known (e.g. `👍 Alice ❤️ you`); otherwise they keep the compact count form (`👍2`).
Reaction mutation and media transfer are currently Google Messages features;
Slack remains text-first.

## Slack daily-driver behavior

- Stream metadata distinguishes DMs, group DMs, public channels, and private channels.
- Unread counts increment only for newly ingested inbound messages; repeated polls do not inflate them. Opening a stream clears the local unread count.
- Channel history sync is cursor-based. The background poll fetches only newer messages; `PgUp` / `Ctrl+U` fetches older history for the active stream.
- Press `Ctrl+T` on a Slack message to fetch `conversations.replies` into a dedicated view. Messages sent there use Slack's `thread_ts`; `Esc` returns to the channel.
- `Ctrl+F` searches only the active river's locally synced corpus.
- Slack files, reaction mutation, Block Kit rendering, and V2 ingest/outbox remain deferred.

## Smoke checklist

1. `go build -o om-tui .`
2. `./om-tui serve --api --no-web` in one terminal (temp `OPENMESSAGES_DATA_DIR` is fine)
3. `curl http://127.0.0.1:7007/api/status` returns JSON
4. `./om-tui tui` attaches ("Attached to local API daemon…")
5. Quit TUI — standalone daemon still running
6. Stop daemon; run `tui` alone — it spawns `serve --api` and shows conversations once paired
7. Press `p` when unpaired to pair Google Messages: paste a `messages.google.com` curl with `Ctrl+V`, then confirm the emoji on your phone; `Esc`/`q` cancels if it stalls; `r` reconnects an existing session
8. In a thread with an image: `o` opens the OS default viewer; `s` writes under the export dir's `media/` folder
9. Attach: `Ctrl+A`, paste a path and Enter, or copy a file/screenshot and `Ctrl+V`
10. Reactions: with a message selected, `Ctrl+E` then `1` reacts; same digit again removes
11. Slack: `Ctrl+T` opens a reply thread; `PgUp` / `Ctrl+U` from the composer loads older history
12. From the composer (default after open): `Ctrl+F` search, `Ctrl+R` reconnect, `Ctrl+A` attach — bare letters stay typable
13. `Ctrl+K` opens the palette: bare text jumps across rivers; `>react` / `>msg name::hi` run commands; Esc closes without changing the draft; footer stays context-aware
14. Optional `commands.json` custom send/open shortcuts appear under `>`

## Notifications

- **Windows**: `serve --api` shows toast notifications for fresh inbound Google Messages (daemon-side, so they work even when the TUI is unfocused or closed). Respects per-conversation `notification_mode` (`all` / `mentions` / `muted`); skips outgoing messages and duplicates by message id. Default **on**; set `OPENMESSAGES_WINDOWS_NOTIFICATIONS=0` to disable. Optional `OPENMESSAGES_WINDOWS_TOAST_APP_ID` overrides the toast AppUserModelID (defaults to PowerShell's registered AUMID so toasts actually show).
- **macOS/Linux**: no native desktop notification path yet from the daemon (the macOS notification code that existed for the native app is being removed along with that app — see NOTICE.md) — another known cross-platform gap.
- Restart the API daemon after rebuilding so it picks up notifier changes (`tui` will respawn an owned child).

Unread badges in the conversation list come from `unread_count`. Fresh inbound Google messages increment it; opening a thread (or receiving while that thread is open) clears it via `/api/mark-read`.

## Known cross-platform gaps

- **Vault**: Keychain (macOS) and Secret Service (Linux) backends are shipped but not yet runtime-verified on real hardware (see "Data directory" above). Windows DPAPI is the daily-driver path today.
- **Google pairing**: current Chrome on Windows encrypts Gaia cookies (v20 / app-bound). The TUI does not auto-read Chrome; paste a `messages.google.com` curl.
- **Notifications**: Windows toast notifications only; no native macOS/Linux equivalent yet.
- **File picker**: `Ctrl+A` opens a native picker on Windows; macOS/Linux currently require typing/pasting a path.

## Non-goals

- Native GUI / tray app
- iMessage live sync (import-only)
- Inline Kitty/sixel/ASCII media thumbs (for now)
- Signed installers
