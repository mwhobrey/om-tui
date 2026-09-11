# om-tui

Local-first, cross-platform terminal messaging client and universal message
database with a built-in MCP server. Live platforms: Google Messages
(SMS/RCS), WhatsApp, Signal, and Slack rivers. Imports: Google Chat,
iMessage, WhatsApp, Signal Desktop.

**This checkout** is `mwhobrey/om-tui` (`origin`), a fork of
`MaxGhenis/openmessage` (`upstream`) that drops upstream's macOS app and web
UI to focus solely on the TUI, across Windows, macOS, and Linux. Go module
path remains `github.com/maxghenis/openmessage` on purpose — see
[NOTICE.md](NOTICE.md) — so upstream diffs stay easy to compare and port.
Prefer **`docs/runbook/`** over this file when they disagree — the
federated runbook is the ground truth for stack, gotchas, and current
state.

## Architecture

```
├── cmd/                 Go CLI (pair, serve, tui, send, read, status, import, backup, migrate, repair)
├── internal/
│   ├── app/             Bootstrap, data dir, backfill, Slack rivers
│   ├── client/          libgm Google Messages protocol
│   ├── db/              Legacy SQLite (conversations, messages, contacts, rivers, drafts)
│   ├── river/           Account/workspace identity (messages-default, slack-<team>)
│   ├── vault/           Per-river sealed credentials (DPAPI Windows, Keychain macOS, Secret Service Linux)
│   ├── slacklive/       Slack Web API client + recent sync + text send
│   ├── bridge/          Transport contracts, capability registry, supervisors
│   ├── bridgeadapters/  Google / WhatsApp / Signal / Slack adapters
│   ├── tui/             Bubble Tea terminal UI (the only client this fork ships)
│   ├── localapi/        Authenticated daemon HTTP client (CLI / TUI / MCP client)
│   ├── importer/        gchat, imessage, whatsapp, signal desktop
│   ├── ingest/           V2 ingest workers, decoders, and Google device ID-space repair
│   ├── story/           Stats + narrative story generation
│   ├── tools/           MCP tools (24 tools)
│   ├── viz/             Relationship visualization renderer (self-contained HTML)
│   ├── storage/         V2 SQLite + blobs (staged cutover)
│   └── web/             Local HTTP+SSE API only — no bundled UI on this fork
└── docs/
    ├── runbook/         Federated agent/developer runbook (START HERE)
    ├── agent-runbook.md Live-install support (data dir, MCP fratricide, re-pair)
    └── tui.md           TUI + Slack rivers product path (all platforms)
```

There is no `macos/` (native app), `internal/web/static/` (React UI), or
`site/` (marketing site) in this fork — upstream maintains those. See
[NOTICE.md](NOTICE.md) for credit.

## Supporting a live install (READ FIRST for support/debug tasks)

If you are debugging a real user's install — sends failing, re-pairing, reading
their actual messages — read **[docs/agent-runbook.md](docs/agent-runbook.md)**
and **[docs/runbook/](docs/runbook/)** before touching anything. The traps that cost the most:

- **Data dir defaults.** macOS/Linux: `~/.local/share/openmessage/`.
  Windows: `%LOCALAPPDATA%\OpenMessage`. Pair / serve / tui / MCP must
  share one dir (`OPENMESSAGES_DATA_DIR` overrides).
- **Read live messages via the HTTP API** (`/api/conversations/<id>/messages`,
  `/api/search`, `/api/status`) — the daemon holds the WAL'd DB, so a direct
  `sqlite3` reader hits "unable to open database file (14)".
- **Re-pairing Google Messages:** QR is dead for many accounts. Press `p` and
  paste a `messages.google.com` curl (`Ctrl+V`); clear `session.json` to reach
  the pairing screen; don't over-reconnect (it throttles the account).
- **One transport owner.** `serve --mcp-stdio` is transportless by default; a
  second process with WhatsApp/Signal credentials logs the other out.
- **macOS/Linux vault is not yet at parity.** Slack river credentials use
  Keychain (macOS) / Secret Service via `secret-tool` (Linux) when available;
  if the backing tool is missing, secrets refuse to store outside
  `OPENMESSAGES_VAULT_INSECURE=1` (local testing only). See
  [docs/tui.md](docs/tui.md) "Known cross-platform gaps".

## Daily driver

TUI + rivers, on any OS. Full keys and smoke checklist:
**[docs/tui.md](docs/tui.md)**.

```bash
# Go is mise-managed on this box — see docs/runbook/03_RULES_AND_STANDARDS.md
go build -o om-tui .                       # om-tui.exe on Windows
./om-tui tui                               # unpaired: press p, paste a messages.google.com curl
./om-tui pair slack --token xoxp-... --name "Acme"
```

- Rivers: built-in `messages-default`; Slack rivers are `slack-<team-id>`.
- Credentials: `rivers/<id>/credentials.enc`, OS-backed sealing (see vault note above).
- TUI: `[` / `]` switch river; `/` jump filter; `Ctrl+F` message search; `o`/`s` open/save media; `p` Google Account pair when unpaired.
- API daemon: `serve --api --no-web` exposes `/api/rivers`, `/api/conversations?river_id=…`.

## Local CLI (read-only, no transports)

These commands open the store directly (repair-free, via `app.NewClient` — no
startup repair writes to the shared live store) and start no live transports:

```bash
om-tui read "<query>" [--limit N] [--phone NUMBER] [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--json]
om-tui search ...                                            # alias for read
om-tui status [--json]                                       # per-platform counts + sync freshness
```

`status` is the fast way to check coverage before trusting a search. Date
filtering lives in the store via `SearchFilter`/`SearchMessagesFiltered`.

## Multi-platform import

```bash
om-tui import gchat /path/to/Takeout/Google\ Chat/Groups/ --email you@gmail.com
om-tui import gchat-conversation /path/to/messages.json --email you@gmail.com
om-tui import imessage                     # reads ~/Library/Messages/chat.db (needs Full Disk Access)
om-tui import whatsapp /path/to/chat.txt --name "Your Name"
om-tui import signal [support-dir]         # Signal Desktop history
```

### Google device ID-space repair

```bash
om-tui repair google-idspace --since <RFC3339|unix-ms> [--account google-primary] [--apply] [--json] [--report path]
```

Ported from upstream: a phone swap or backup restore re-keys Google's
device-local conversation/message IDs, which without this repair causes
duplicate messages and threads wrongly rebound. See `internal/ingest/idspace.go`,
`repair_idspace.go`, and `internal/storage/sqlite/{content_dedupe,participants_ensure,rebind}.go`.

### MCP serving modes

`serve --mcp-stdio` (the shape MCP hosts spawn per session) runs as a
**transportless client**: zero transport supervisors — the app daemon owns all
live connections. Reads stay local (v2 when the daemon reports v2-primary);
sends/reactions/`get_status` route through the daemon HTTP API
(`internal/localapi`). Store opens via `app.NewClient` (no repair sweeps).
See [docs/agent-runbook.md](docs/agent-runbook.md) ("MCP serving").

### MCP tools

24 tools registered (see `internal/tools/tools.go` `RegisterWithOptions`):
- `get_messages`, `get_conversation`, `search_messages` — cross-platform by default
- `list_conversations` — optional `source_platform` filter (sms, gchat, imessage, whatsapp, signal, slack)
- `get_person_messages` / `get_person_messages_range` — cross-platform person history (V2-primary via `ReadSource`)
- `import_messages` — import from any supported source
- `conversation_stats`, `generate_story`, `person_stats`, `generate_person_story`, `generate_viz`, `render_story`
- `send_message`, `send_to_conversation`, `send_media_to_conversation`, `send_group_message`
- `react_to_message`, `draft_message`, `download_media`, `list_contacts`, `resolve_contact_routes`, `get_status`

Story/stats/viz tools are unavailable while V2 is the serving store. Person-history tools stay live.

### HTTP API (high-signal)

- `GET /api/status` — platform + Slack river connection snapshot
- `GET /api/rivers` — registered rivers with unread aggregates
- `GET /api/conversations?limit=50&river_id=…` — list (optional river / `source_platform` filter)
- `GET /api/conversations/<id>/messages` — thread
- `GET /api/search?q=…` — search across platforms
- `GET /api/media/<message_id>` — download attachment (requires `MediaID`)

No static UI is served from this fork — `internal/web` is the API/SSE layer
only. `gifs.go`/`linkpreview.go` handlers exist but are currently dormant
(no TUI consumer yet).

### Schema

Messages and conversations have `source_platform`
(sms/gchat/imessage/whatsapp/signal/telegram/slack) and messages have
`source_id` for dedup. Conversations also have `river_id`. Unified contacts
map people across platforms. Rivers table + vault store account/workspace
instances.

## Testing

```bash
go test ./...
go test -race ./...
go build .
```

On Windows, many packages fail from POSIX assumptions in tests (not product
bugs) — see the baseline table in
[docs/runbook/03_RULES_AND_STANDARDS.md](docs/runbook/03_RULES_AND_STANDARDS.md).
Packages this fork cares about (`tui`, `river`, `vault`, `db`, `app`, `localapi`)
must stay green locally.

## Relationship visualization (`generate_viz`)

Self-contained HTML combining dashboards + narrative. Key packages:
`internal/viz/`, `internal/story/stats.go`. MCP: `generate_viz`, `render_story`.
Export confined to `OPENMESSAGES_EXPORT_DIR` unless
`OPENMESSAGES_ALLOW_ANY_EXPORT_PATH=1`.

## Agentic story generation (`/generate-story`)

Claude Code slash command: `.claude/commands/generate-story.md`. Uses
`person_stats` → `get_person_messages_range` → grounded chapters → `render_story`.

## Key files

- `docs/runbook/` — architecture, components, standards, current state
- `docs/tui.md` — TUI keys, rivers, smoke checklist, cross-platform gaps
- `docs/agent-runbook.md` — live-install support traps
- `NOTICE.md` — fork origin + upstream library credit
- `internal/app/app.go` — data-dir resolution (`DefaultDataDir`)
- `internal/river/`, `internal/vault/`, `internal/app/slack.go`, `internal/slacklive/`
- `internal/tui/` — Bubble Tea UI
- `internal/db/db.go` — legacy schema (incl. `river_id`, rivers table)
- `internal/tools/tools.go` — MCP registration
