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
│   ├── river/           Account/workspace identity (messages-default, whatsapp-default, signal-default, slack-<team>)
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
    ├── ROADMAP.md       Triaged backlog, upstream ports, and tracking roadmap
    ├── agent-runbook.md Live-install support (data dir, MCP fratricide, re-pair)
    └── tui.md           TUI + rivers product path (all platforms)
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
- **Re-pairing Google Messages:** Prefer the Chrome cookie bridge when installed
  ([docs/runbook/05_CHROME_COOKIE_BRIDGE.md](docs/runbook/05_CHROME_COOKIE_BRIDGE.md)).
  QR is dead for many accounts. Press `p` and paste a `messages.google.com`
  curl (`Ctrl+V`) when the bridge is offline. If this PC is already paired,
  that paste refreshes cookies with no phone tap. Clear `session.json` only
  to force a full unpaired Gaia pair; don't over-reconnect (it throttles).
- **One transport owner.** `serve --mcp-stdio` is transportless by default; a
 second process with WhatsApp/Signal credentials logs the other out.
 Store-owning `serve` holds `<data-dir>/instance.lock` so backup/migrate/repair
 and a second daemon refuse while it is up; MCP-stdio clients do not take it.
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
./om-tui chrome-cookie-host --install      # Windows: register Chrome native host
# then Chrome → chrome://extensions → Load unpacked → extensions/google-cookies/
# full steps: docs/runbook/05_CHROME_COOKIE_BRIDGE.md
./om-tui pair slack --token xoxp-... --name "Acme"
```

- Rivers: built-in `messages-default`, `whatsapp-default`, `signal-default`; extras are `whatsapp-2` / `signal-2` (`Ctrl+K` → `>add …`); Slack rivers are `slack-<team-id>`. Google Messages is one live river.
- **v1 (`messages.db`) is frozen except critical bugs.** Slack interactive Block Kit: TUI `b`/`Ctrl+B` opens URL buttons and Slack-deep-links app-owned controls. Slack V2 migrate + outbox, leftover PRIMARY writes (drafts, tabs, contact CRM metadata, `/api/new-conversation`, send minting), GET `/api/conversations/{id}`, and Windows backup DSN/VACUUM fallback are wired. V2 is the serving default after `migrate` (or a fresh empty install); `OPENMESSAGES_V2_PRIMARY=0` keeps v1. Google contact **sync** writes v2 identities (`/api/contacts/sync`); scheduled-send stays fail-closed. On Windows, Google paste against an existing session refreshes cookies (no phone tap unless the device was unlinked).
- Credentials: `rivers/<id>/credentials.enc`, OS-backed sealing (see vault note above).
- TUI: `[` / `]` switch river; `/` jump filter; `Ctrl+F` message search; `o`/`s` open/save media; `p` pair the active river (Google paste / WhatsApp QR / Signal QR)
- API daemon: `serve --api --no-web` exposes `/api/rivers`, `/api/conversations?river_id=…`.

## Local CLI (read-only, no transports)

These commands open the store directly (repair-free, via `app.NewClient` — no
startup repair writes to the shared live store) and start no live transports:

```bash
om-tui read "<query>" [--limit N] [--phone NUMBER] [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--json]
om-tui search ...                                            # alias for read
om-tui status [--json]                                       # per-platform counts + sync freshness
om-tui export <conversation> [--format json|yaml|csv] [--since D] [--until D] [--out PATH|-]  # one thread/channel to a file
```

`status` is the fast way to check coverage before trusting a search: it lists
each platform's message count and latest sent/received timestamps, and flags a
platform that has stopped syncing in the AGE column. "Nd behind": its latest
message trails the newest overall by ≥3 days. "silent Nh": its transport has
delivered nothing for N hours, longer than its own baseline explains. That is
the `internal/freshness` silence rule behind `/api/status`
`freshness.<platform>.silence`, and it also catches a stall of the newest
platform, which the relative rule never can (Google, 2026-10-06). When the
running app serves the same data dir and reports a verdict measured on the
source this command reads, `status` shows the app's (daemon truth, as for
sends). Otherwise it judges locally with `freshness.EvaluateSilence` on the
source the daemon would read: the v2 inbox when reads come from the v2 store,
the legacy store's incoming messages otherwise. It reuses the stores it already
opened. The text under the table says who judged and why (the app isn't
running or didn't answer within 3s, serves another data dir, predates the
check, or measures v2 while the CLI reads legacy), and names any verdict
judged without a baseline (only the 72h floor applies) or platform with no
recorded activity. `--json` adds each row's `behind_days`, `silence` (the
daemon's field names, built by `freshness.SilenceBlock`) and
`silence_judged_by` (`daemon` | `local`), plus `silence_note`. sms and rcs rows
both carry Google's verdict. A stale row means the daemon isn't syncing that
platform, or the phone behind it has stopped delivering; searches over that
window will miss messages.

On a v2-primary store `status` also prints a "Google SMS:" line: a ⚠ there
means no incoming SMS for longer than usual while RCS still arrives, which
per-platform freshness cannot see; on 2026-10-07 it was the phone's IMS stack
(runbook: "SMS stopped while RCS works"). `--json` carries it as
`google_sms_path`. It is judged locally from the v2 inbox, so it never reports
`google_unreachable`; `/api/status` is the authority.

"Read-only" here means no startup repair sweeps (`repairStartupArtifacts`) and
no transports. The v2 store is opened with `sqlite.OpenWithoutMigrating`, which
never migrates: a binary at a different schema version than the store refuses
it (an older one reports the store as newer than supported; a newer one asks
you to reopen the app).

`read` resolves each hit's sender (name → number → conversation id) so results
are legible without a second lookup, and accepts
`--since`/`--until` (YYYY-MM-DD, local time; `--until` is inclusive to end of
day) to scope a search to a date window. Date filtering lives in the store via
`SearchFilter`/`SearchMessagesFiltered`.

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
- `get_person_messages` / `get_person_messages_range` — cross-platform person history
- `import_messages` — import from any supported source
- `conversation_stats`, `generate_story`, `person_stats`, `generate_person_story`, `generate_viz`, `render_story`
- `send_message`, `send_to_conversation`, `send_media_to_conversation`, `send_group_message`
- `react_to_message`, `draft_message`, `download_media`, `list_contacts`, `resolve_contact_routes`, `get_status`

Person/story/viz tools, `list_contacts`, `resolve_contact_routes`, and `download_media` read through `v2read` when V2 is the serving store. `set_message_transcript` and `import_messages` write v2 (`message_extras` / incremental `SyncInto`) when primary. MCP `react_to_message` uses the v2 outbox when the daemon is primary. MCP `send_media_to_conversation` submits native v2 conversation IDs. MCP stdio `send_message` / `send_group_message` mint via the daemon (`/api/new-conversation` + outbox). `list_contacts` merges conversation participants with the Google address book (daemon `POST /api/contacts/sync` writes v2 identities). `draft_message` writes the v2 `drafts` table when primary.

### HTTP API (high-signal)

- `GET /api/status` — platform + Slack river connection snapshot
- `GET /api/rivers` — registered rivers with unread aggregates
- `GET /api/conversations?limit=50&river_id=…` — list (optional river / `source_platform` filter)
- `GET /api/conversations/<id>/messages` — thread
- `GET /api/search?q=…` — search across platforms
- `GET /api/media/<message_id>` — download attachment (requires `MediaID`)

- `GET /api/search/messages?q=...` — **message-level** search: raw message DTOs
- `POST /api/react` — `{conversation_id, message_id, emoji, action}`. On a v2-primary daemon it queues the reaction on the outbox and waits up to 8 s.
- `POST /api/v1/outbox/reactions` — the durable twin (v2-primary only, `idempotency_key` required).

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
