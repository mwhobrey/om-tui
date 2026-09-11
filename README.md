# om-tui

om-tui is a local-first terminal messaging client: one inbox for Google
Messages (SMS/RCS), WhatsApp, Signal, and Slack, driven entirely from a
Bubble Tea TUI on Windows, macOS, or Linux. It's a fork of
[MaxGhenis/openmessage](https://github.com/MaxGhenis/openmessage), which
also ships a native macOS app and a localhost web UI; om-tui drops both to
focus on one thing — a fast, cross-platform terminal client — while staying
a drop-in MCP server for Claude Code and other MCP clients. Full credit and
licensing for everything this is built on lives in [NOTICE.md](NOTICE.md).

## What it does

- **Google Messages** — pair your Android phone and read/send SMS + RCS locally
- **Live WhatsApp** — link WhatsApp as a live companion device on your machine
- **Live Signal** — link Signal locally and keep its threads in the same inbox
- **Slack rivers** — readable users/DMs, unread filters, dedicated reply threads, lazy history, text send, and optional Socket Mode realtime
- **One local inbox** — search, route-aware threads, favorites, media, reactions, drafts, scheduled sends, and grouped contacts
- **Rivers** — every account/workspace (Google Messages, each Slack team) is an isolated, independently-paired identity in the same inbox
- **MCP-ready** — expose the same local inbox to Claude Code and other MCP clients over stdio, Streamable HTTP, or SSE
- **Local storage** — SQLite, your data stays on your machine

## Quick start

### Prerequisites

- **Go 1.26.6+** ([install](https://go.dev/dl/); this repo often uses [mise](https://mise.jdx.dev/); see `go.mod`)
- **Google Messages** on your Android phone

### Build and pair

```bash
git clone https://github.com/mwhobrey/om-tui.git
cd om-tui
go build -o om-tui .          # om-tui.exe on Windows
./om-tui tui                  # unpaired: press p, paste a messages.google.com curl, tap the emoji
./om-tui pair slack --token xoxp-... --name "Acme"   # optional, add --app-token xapp-... for Socket Mode
```

Google Account pairing is the path that works for most accounts. In the TUI,
press `p`, then paste a `curl` / Cookie header from `messages.google.com`
(`Ctrl+V`). Chrome auto-read is parked: current Chrome encrypts those cookies.
CLI paste still works when the daemon is down:

```bash
pbpaste | ./om-tui pair --google      # macOS
wl-paste | ./om-tui pair --google     # Linux Wayland (or: xclip -o)
```

```powershell
Get-Clipboard | .\om-tui.exe pair --google   # Windows
```

The CLI accepts either a JSON cookie object or a full `curl` command, then
prompts you to confirm an emoji on your phone.

QR pairing still works on some accounts: `./om-tui pair` prints a QR code
to scan under **Google Messages > Settings > Device pairing > Pair a device**.
If Google only offers account pairing, use the cookie method above.

Default data dir: `~/.local/share/openmessage` (macOS/Linux) or
`%LOCALAPPDATA%\OpenMessage` (Windows). The TUI transparently spawns
`serve --api --no-web` if no daemon is already running.

### Optional: link WhatsApp or Signal

From the TUI, switch rivers with `[` / `]` and follow the connection prompts
for WhatsApp or Signal — both run as local companion-device bridges and land
in the same inbox as everything else.

### Connect to Claude Code

Add to `~/.mcp.json`:

```json
{
  "mcpServers": {
    "om-tui": {
      "command": "/path/to/om-tui",
      "args": ["serve", "--mcp-stdio"]
    }
  }
}
```

Restart Claude Code — the MCP tools appear automatically. To connect over
HTTP instead, start with `--mcp-sse` and point a client at
`http://127.0.0.1:7007/mcp` (Streamable HTTP) or `/mcp/sse` (SSE).

## TUI

- `[` / `]` — switch rivers (Google Messages, each paired Slack workspace)
- `/` — jump filter across conversations
- `Ctrl+F` — message search
- `o` / `s` — open / save media
- Standard message flows: read, reply, react, forward, drafts, scheduled sends

Full keys and a smoke checklist: [docs/tui.md](docs/tui.md).

## MCP tools

| Tool | Description |
|------|-------------|
| `get_messages` | Recent messages with filters (phone, date range, limit) |
| `get_conversation` | Messages in a specific conversation |
| `search_messages` | Full-text search across all messages |
| `send_message` | Send a direct text by platform. Defaults to SMS/RCS and also supports direct WhatsApp/Signal recipients |
| `send_to_conversation` | Send a text reply directly to an existing conversation ID |
| `send_media_to_conversation` | Send a local file attachment to an existing conversation ID |
| `send_group_message` | Send to a group conversation by conversation ID |
| `react_to_message` | Add, remove, or switch a reaction on an existing message |
| `set_message_transcript` | Save or update a transcript for an audio/media message |
| `list_conversations` | List recent conversations |
| `list_contacts` | List/search contacts |
| `resolve_contact_routes` | Resolve a person or phone number to the available SMS/WhatsApp/Signal routes |
| `get_status` | Google Messages, WhatsApp, Signal, and Slack river connection status |
| `download_media` | Return the local `/api/media/<message-id>` URL and metadata for an attachment |
| `draft_message` | Save a draft for later review/send |
| `import_messages` | Import Google Chat, iMessage, WhatsApp export, or Signal Desktop history |
| `get_person_messages` | Load messages for conversations matching a person's name |
| `get_person_messages_range` | Load a bounded time range of messages matching a person's name |
| `conversation_stats` | Summarize conversation counts, cadence, and activity |
| `person_stats` | Summarize a person's cross-route activity |
| `generate_story` | Generate a narrative summary for one conversation |
| `generate_person_story` | Generate a narrative summary for a person across routes |
| `generate_viz` | Generate a local HTML visualization from a conversation |
| `render_story` | Render a story/viz HTML artifact with optional local photos |

## MCP examples

- List recent Signal threads: `list_conversations(source_platform="signal")`
- Search WhatsApp for a keyword: `search_messages(query="airbnb")`
- Send a direct Signal message: `send_message(platform="signal", recipient="+15551230000", message="On my way")`
- Send a text into a route-aware thread: `send_to_conversation(conversation_id="whatsapp:15551234567@s.whatsapp.net", message="On my way")`
- Send a photo from disk: `send_media_to_conversation(conversation_id="signal-group:abc123", file_path="/tmp/photo.jpg", caption="Here")`
- React to a message: `react_to_message(conversation_id="signal-group:abc123", message_id="signal:...", emoji="🔥")`
- Resolve a person's available routes before sending: `resolve_contact_routes(query="Taylor")`
- Review one person's cross-platform history: `get_person_messages(name="Taylor", limit=50)`
- Import Signal Desktop history: `import_messages(source="signal", path="$HOME/Library/Application Support/Signal", name="Your Name", address="+15551230000")`

## Configuration

| Env var | Default | Purpose |
|---------|---------|---------|
| `OPENMESSAGES_DATA_DIR` | macOS/Linux: `~/.local/share/openmessage`; Windows: `%LOCALAPPDATA%\OpenMessage` | Data directory (DB + session + rivers vault) |
| `OPENMESSAGES_LOG_LEVEL` | `info` | Log level (debug/info/warn/error/trace) |
| `OPENMESSAGES_PORT` | `7007` | Local API port |
| `OPENMESSAGES_HOST` | `127.0.0.1` | Host/interface to bind the local API server to |
| `OPENMESSAGES_VAULT_INSECURE` | unset | Non-Windows test-only vault (never set in production) |
| `OPENMESSAGES_MY_NAME` | system user name | Display name for outgoing imported iMessage/WhatsApp messages |
| `OPENMESSAGES_STARTUP_BACKFILL` | `auto` | Startup history sync mode: `auto`, `shallow`, `deep`, or `off` |
| `OPENMESSAGES_BACKFILL_DISCOVER_ORPHANS` | `0` | Opt in to deep backfill's Phase C (contact-based orphan discovery). **Off by default** because it creates an empty SMS thread on your phone for each contact without prior message history. Enable with `1`/`true`/`yes`/`on` only if you understand the side effect. |
| `OPENMESSAGE_COOKIE_REFRESH_SCRIPT` | unset | Optional command run before reconnect when Google session cookies expire. If unset, om-tui backs off and prompts for manual re-pair instead of hammering Google's auth endpoint. |
| `OPENMESSAGES_WINDOWS_NOTIFICATIONS` | on for Windows `serve` | Enable/disable Windows toast notifications for fresh inbound live messages (`1`/`0`). Daemon-side (including `serve --api` / TUI-spawned daemon). Respects conversation mute / mentions mode. |
| `OPENMESSAGES_WINDOWS_TOAST_APP_ID` | PowerShell's registered AUMID | Windows toast AppUserModelID. Unregistered IDs often show nothing; default uses PowerShell's AUMID so toasts actually appear (branded as PowerShell). |
| `OPENMESSAGES_SIGNAL_TMP_SWEEP` | enabled | Set to `0` to disable the cleanup of stale signal-cli temp directories (run dirs plus `libsignal*` dirs older than 24h that pre-v0.2.10 builds leaked into the system temp dir). |
| `OPENMESSAGES_SIGNAL_CLI` | auto-detected `signal-cli` | Optional path to a specific `signal-cli` binary. |
| `OPENMESSAGES_GOOGLE_AVATAR_SYNC` | enabled | Set to `0` to disable Google contact avatar sync. The older singular `OPENMESSAGE_GOOGLE_AVATAR_SYNC` name is also accepted. |
| `OPENMESSAGES_EXPORT_DIR` | `~/Documents/OpenMessage` | Directory for `generate_viz` / `render_story` HTML outputs and photo inputs when using the default confined export mode. |
| `OPENMESSAGES_ALLOW_ANY_EXPORT_PATH` | unset (off) | Set to `1`/`true`/`yes`/`on` to allow viz/story tools to read photos from, or write HTML to, arbitrary local paths. |
| `OPENMESSAGE_TELEMETRY` | unset (off) | Set to `1` to send one anonymous heartbeat per launch (max one per 24h). Reports only: random install ID, version, OS/arch, and which platforms are paired. No message content, no contact info, no IP-based identity. See `internal/telemetry/`. |

`OPENMESSAGES_HOST` defaults to localhost for safety. If you bind the
server to another interface for LAN testing, keep it on a trusted network —
the local API and MCP endpoints are meant for same-origin/local clients.

## Architecture

- **libgm** handles the Google Messages protocol (pairing, encryption, long-polling)
- **whatsmeow** handles live WhatsApp pairing, sync, text/media send, receipts, typing, and avatars through a separate local session store
- **signal-cli** powers the local Signal linked-device bridge, message sync, media, and reactions
- **slack-go** + sealed river vault power Slack workspace rivers (text send + recent history sync)
- **Rivers** are account/workspace instances (`messages-default`, `slack-<team-id>`); credentials live under `rivers/<id>/credentials.enc`
- **SQLite** (WAL mode, pure Go) stores messages, conversations, contacts, and rivers locally; real-time events from each platform are written as they arrive
- The Bubble Tea **TUI** talks to a local daemon (`serve --api --no-web`) over the same HTTP+SSE API used by MCP tools
- WhatsApp Desktop and Signal Desktop imports remain available as backfill/repair paths when a live bridge isn't active
- On first run, a deep backfill fetches full SMS/RCS history in the background; later runs do a lighter incremental sync by default
- Scheduled messages live in SQLite, are claimed atomically by the background scheduler, and retry when the target platform is temporarily disconnected
- MCP tool handlers read from SQLite for queries and route sends through the same local runtime
- MCP HTTP transport supports both Streamable HTTP at `/mcp` and SSE at `/mcp/sse` when enabled with `--mcp-sse`; stdio is available with `--mcp-stdio`
- Auth tokens auto-refresh and persist to `session.json`; expired Google cookies can be refreshed by an optional script or surfaced as a guided re-pair flow

## Development

```bash
go test ./...        # Run all tests
go test -race ./...  # Run with the race detector
go build .            # Build binary
./om-tui pair        # Pair with phone
./om-tui tui          # Terminal UI (spawns serve --api --no-web if needed)
```

Debugging a live install (failing sends, re-pairing, the two-data-dir
gotcha, signal-cli version)? See [docs/agent-runbook.md](docs/agent-runbook.md).

Architecture, components, and current fork state: [docs/runbook/](docs/runbook/).

## Credit

om-tui is a fork of [MaxGhenis/openmessage](https://github.com/MaxGhenis/openmessage).
Full attribution for that project and every library this is built on —
mautrix/gmessages, whatsmeow, signal-cli, slack-go, mcp-go — is in
[NOTICE.md](NOTICE.md).

## Contributing / security

Issues and PRs: [CONTRIBUTING.md](CONTRIBUTING.md). Vulnerability reports:
[SECURITY.md](SECURITY.md) (private advisory, not a public issue).

## License

[Unlicense](LICENSE) (public domain).
