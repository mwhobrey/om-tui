# OpenMessage

Local-first messaging workspace and universal message database with a built-in
MCP server. Live platforms: Google Messages (SMS/RCS), WhatsApp, Signal, and
(on this fork) Slack rivers. Imports: Google Chat, iMessage, WhatsApp, Signal Desktop.

**This checkout** is the `mwhobrey/om-tui` fork (`origin`) of
`MaxGhenis/openmessage` (`upstream`). Go module path remains
`github.com/maxghenis/openmessage`. Prefer **`docs/runbook/`** over this file
when they disagree — the federated runbook is the ground truth for stack,
gotchas, and current state.

## Architecture

```
├── cmd/                 Go CLI (pair, serve, tui, send, read, status, import, backup, migrate)
├── internal/
│   ├── app/             Bootstrap, data dir, backfill, Slack rivers
│   ├── client/          libgm Google Messages protocol
│   ├── db/              Legacy SQLite (conversations, messages, contacts, rivers, drafts)
│   ├── river/           Account/workspace identity (messages-default, slack-<team>)
│   ├── vault/           Per-river sealed credentials (DPAPI on Windows)
│   ├── slacklive/       Slack Web API client + recent sync + text send
│   ├── bridge/          Transport contracts, capability registry, supervisors
│   ├── bridgeadapters/  Google / WhatsApp / Signal / Slack adapters
│   ├── tui/             Bubble Tea terminal UI (Windows daily driver)
│   ├── localapi/        Authenticated daemon HTTP client (CLI / TUI / MCP client)
│   ├── importer/        gchat, imessage, whatsapp, signal desktop
│   ├── story/           Stats + narrative story generation
│   ├── tools/           MCP tools (24 tools)
│   ├── viz/             Relationship visualization renderer (self-contained HTML)
│   ├── storage/         V2 SQLite + blobs (staged cutover)
│   └── web/             HTTP API + embedded React UI
├── macos/               Swift macOS app wrapper
├── docs/
│   ├── runbook/         Federated agent/developer runbook (START HERE)
│   ├── agent-runbook.md Live-install support (dual data dirs, MCP fratricide, re-pair)
│   └── windows-tui.md   Windows TUI + Slack rivers product path
├── site/                Static website (openmessage.ai)
└── vercel.json          Vercel config (root — NOT site/vercel.json)
```

## Supporting a live install (READ FIRST for support/debug tasks)

If you are debugging a real user's install — sends failing, re-pairing, reading
their actual messages — read **[docs/agent-runbook.md](docs/agent-runbook.md)**
and **[docs/runbook/](docs/runbook/)** before touching anything. The traps that cost the most:

- **Platform-specific data dirs.** macOS app live store:
  `~/Library/Application Support/OpenMessage/` via `OPENMESSAGES_DATA_DIR`.
  CLI default on macOS/Linux: `~/.local/share/openmessage/` (often stale).
  Windows default: `%LOCALAPPDATA%\OpenMessage`. Pair / serve / tui / MCP must
  share one dir.
- **Read live messages via the HTTP API** (`/api/conversations/<id>/messages`,
  `/api/search`, `/api/status`) — the daemon holds the WAL'd DB, so a direct
  `sqlite3` reader hits "unable to open database file (14)".
- **Re-pairing Google Messages:** QR is dead for many accounts; use Google Account
  pairing via the cookie method; clear `session.json` from **both** macOS data
  dirs to reach the pairing screen; don't over-reconnect (it throttles the account).
- **One transport owner.** `serve --mcp-stdio` is transportless by default; a
  second process with WhatsApp/Signal credentials logs the other out.

## Windows daily driver (this fork)

Windows targets the **TUI + rivers**, not the macOS app or React UI. Full keys,
Slack scopes, and smoke checklist: **[docs/windows-tui.md](docs/windows-tui.md)**.

```powershell
# Go is mise-managed on this box — see docs/runbook/03_RULES_AND_STANDARDS.md
go build -o openmessage.exe .
.\openmessage.exe pair                          # Google Messages
.\openmessage.exe pair slack --token xoxp-... --name "Acme"
.\openmessage.exe tui                           # spawns serve --api --no-web if needed
```

- Rivers: built-in `messages-default`; Slack rivers are `slack-<team-id>`.
- Credentials: `rivers/<id>/credentials.enc` (DPAPI — not portable across machines/users).
- TUI: `[` / `]` switch river; `/` jump filter; `Ctrl+F` message search; `o`/`s` open/save media.
- API daemon: `serve --api --no-web` exposes `/api/rivers`, `/api/conversations?river_id=…`.

## Local CLI (read-only, no transports)

These commands open the store directly (repair-free, via `app.NewClient` — no
startup repair writes to the shared live store) and start no live transports:

```bash
openmessage read "<query>" [--limit N] [--phone NUMBER] [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--json]
openmessage search ...                                            # alias for read
openmessage status [--json]                                       # per-platform counts + sync freshness
```

`status` is the fast way to check coverage before trusting a search. Date
filtering lives in the store via `SearchFilter`/`SearchMessagesFiltered`.

## Multi-platform import

```bash
openmessage import gchat /path/to/Takeout/Google\ Chat/Groups/ --email you@gmail.com
openmessage import gchat-conversation /path/to/messages.json --email you@gmail.com
openmessage import imessage                     # reads ~/Library/Messages/chat.db (needs Full Disk Access)
openmessage import whatsapp /path/to/chat.txt --name "Your Name"
openmessage import signal [support-dir]         # Signal Desktop history
```

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
- `get_person_messages` / `get_person_messages_range` — cross-platform person history
- `import_messages` — import from any supported source
- `conversation_stats`, `generate_story`, `person_stats`, `generate_person_story`, `generate_viz`, `render_story`
- `send_message`, `send_to_conversation`, `send_media_to_conversation`, `send_group_message`
- `react_to_message`, `draft_message`, `download_media`, `list_contacts`, `resolve_contact_routes`, `get_status`

Person/story/viz tools are unavailable while V2 is the serving store.

### HTTP API (high-signal)

- `GET /api/status` — platform + Slack river connection snapshot
- `GET /api/rivers` — registered rivers with unread aggregates
- `GET /api/conversations?limit=50&river_id=…` — list (optional river / `source_platform` filter)
- `GET /api/conversations/<id>/messages` — thread
- `GET /api/search?q=…` — search across platforms
- `GET /api/media/<message_id>` — download attachment (requires `MediaID`)

### Schema

Messages and conversations have `source_platform`
(sms/gchat/imessage/whatsapp/signal/telegram/slack) and messages have
`source_id` for dedup. Conversations also have `river_id`. Unified contacts
map people across platforms. Rivers table + vault store account/workspace
instances.

## Vercel deployment (openmessage.ai)

**CRITICAL: Always deploy from the repo root.** Config lives at root
`vercel.json`, not `site/vercel.json`. Scope: `max-ghenis-projects`.

```bash
cd /path/to/openmessage && vercel --prod
curl -s -o /dev/null -w "%{http_code}" https://openmessage.ai
```

## Building the macOS app

```bash
./macos/build.sh
```

This builds: Go universal binary (arm64+amd64) → Swift app → .app bundle → .dmg

**Dev builds get a distinct bundle identity.** Plain `./macos/build.sh` stamps
`com.openmessage.app.dev`, names the bundle `OpenMessage (dev)`, and emits
`OpenMessage-dev.dmg`, so a stale build can never shadow the installed app in
LaunchServices — by id or by name (this caused two live outages —
see [docs/agent-runbook.md](docs/agent-runbook.md) "Bundle-id shadowing").
Anything installable or shippable **must** set `RELEASE=1`:

```bash
RELEASE=1 ./macos/build.sh
```

Building from a nested `.claude/worktrees/*` checkout also needs `GOWORK=off`
(Go otherwise finds `~/openmessage/go.work` and resolves the main module to the
parent).

To install locally (requires `RELEASE=1` above):
```bash
cp -R macos/build/OpenMessage.app /Applications/ && xattr -cr /Applications/OpenMessage.app
```

Not used on the Windows TUI path.

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
- `docs/windows-tui.md` — Windows TUI / Slack rivers
- `docs/agent-runbook.md` — live-install support traps
- `internal/app/app.go` — data-dir resolution (`DefaultDataDir`)
- `internal/river/`, `internal/vault/`, `internal/app/slack.go`, `internal/slacklive/`
- `internal/tui/` — Bubble Tea UI
- `internal/db/db.go` — legacy schema (incl. `river_id`, rivers table)
- `internal/tools/tools.go` — MCP registration
- `macos/OpenMessage/Sources/BackendManager.swift` — macOS app backend launcher
