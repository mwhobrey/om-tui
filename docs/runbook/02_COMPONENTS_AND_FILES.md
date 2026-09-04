# Components and files

## Top-level layout

```
cmd/                 CLI commands + serve composition + V2 stack wiring
internal/            Application implementation (all product logic)
docs/                Operational docs + this runbook/
scripts/             Support scripts (cookie watchdog, etc.)
.github/workflows/   test, release, gmessages-fork-drift
main.go              Command dispatch + usage text
```

## `cmd/` — CLI surface

| File / area | Responsibility |
|---|---|
| `serve.go` | Compose web / API / MCP / transports; daemon vs client shape |
| `tui.go` | Attach/spawn API daemon; launch Bubble Tea |
| `pair.go` / `pair_slack.go` | Google pair; Slack river pair + vault |
| `read.go`, status, thread(s) | Repair-free store reads |
| `send*.go` | Outgoing text/media via daemon or local |
| `backup.go`, migrate | Offline legacy backup + V2 transform |
| `import` paths | Multi-platform importers entry |
| platform helpers | `instance_lock_*`, `signals_*`, `umask_*`, Windows neterr |

Authoritative usage strings live in `main.go`.

## `internal/` — core packages

### Bootstrap & platforms

| Package | Responsibility |
|---|---|
| `app/` | Data-dir resolution, `New` vs `NewClient`, platform start, sends, scheduler, Slack rivers |
| `client/` | Google libgm session + event → legacy DB mapping |
| `whatsapplive/` | Live WhatsApp bridge |
| `signallive/` | signal-cli bridge + recovery |
| `slacklive/` | Slack Web API client, recent sync, text send |
| `river/` | River identity model (`messages-default`, `slack-<team>`) |
| `vault/` | Encrypted per-river credentials (DPAPI on Windows) |
| `googlecookies/` | macOS Chrome cookie refresh for Google self-heal |
| `notify/` | macOS / Windows notifications |

### Storage

| Package | Responsibility |
|---|---|
| `db/` | Legacy schema/queries: conversations, messages, contacts, drafts, schedules, tabs, **rivers** |
| `storage/sqlite/` | V2 schema, migrations, inbox/outbox/reactions/attachments/repos |
| `storage/blob/` | V2 blob store |
| `v2read/` | V2 `readsource` implementation |
| `v2wire/` | Legacy↔V2 mirror, projector, notifier |
| `v2keys/` | Deterministic V2 IDs / identity keys |
| `readsource/` | Shared read abstraction for MCP/CLI |

### Messaging pipeline

| Package | Responsibility |
|---|---|
| `bridge/` | Contracts, capability registry, supervisors, dispatch leases |
| `bridgeadapters/` | Google / WhatsApp / Signal / Slack adapters; legacy metadata stub |
| `ingest/` | Durable inbox sink, decoders, worker, quarantine, counters |
| `messaging/` | Durable V2 message service / dispatcher |
| `media/`, `whatsappmedia/` | Attachment policy + WhatsApp media refs |

### Surfaces

| Package | Responsibility |
|---|---|
| `web/` | Loopback REST, SSE, MCP HTTP, static UI, auth |
| `localapi/` | Authenticated daemon HTTP client (CLI / TUI / MCP client) |
| `tui/` | Bubble Tea UI: rivers, list/thread/composer, media, reactions, broadcast |
| `tools/` | MCP tool registration + handlers (24 tools) |

### Import / analytics / migration

| Package | Responsibility |
|---|---|
| `importer/` | gchat, imessage, whatsapp, signal desktop |
| `story/` | Stats + narrative generation |
| `viz/` | Self-contained HTML relationship visualizations |
| `migration/`, `cutover/` | Legacy→V2 transform / validation / cutover |
| `livetransport/` | Tagged live-ingest verification tests |
| `telemetry/` | Opt-in anonymous heartbeat |

## Key config / state locations

| What | Where |
|---|---|
| Data dir override | `OPENMESSAGES_DATA_DIR` |
| Legacy DB | `<data-dir>/messages.db` (+ `-wal`/`-shm`) |
| V2 store | `<data-dir>/v2/store.sqlite3` |
| Sessions | `session.json`, `whatsapp-session.db`, `signal-cli/` |
| River vault | `rivers/<river-id>/credentials.enc` |
| Control auth | `control.token` |
| Go deps | `go.mod` / `go.sum` (watch for accidental `go.work`) |
| Vercel | root `vercel.json` (not `site/vercel.json`) |
| gmessages fork pin | replace in `go.mod` + `.github/workflows/gmessages-fork-drift.yml` |

## Routing / entry cheat sheet

| Want to… | Start here |
|---|---|
| Change serve shapes | `cmd/serve.go` |
| Change data-dir defaults | `internal/app/app.go` → `DefaultDataDir` |
| Add MCP tool | `internal/tools/` + `RegisterWithOptions` |
| Add HTTP route | `internal/web/api.go` |
| Add bridge platform | `internal/bridge` contracts + `bridgeadapters/<name>` + supervisor wiring in serve/V2 stack |
| Change TUI keys/layout | `internal/tui/` |
| Change Slack pair/send | `cmd/pair_slack.go`, `internal/app/slack.go`, `internal/slacklive/` |
| Change vault seal | `internal/vault/seal_windows.go` / `seal_darwin.go` / `seal_linux.go` / `seal_other.go` |
| Legacy schema | `internal/db/db.go` |
| V2 schema | `internal/storage/sqlite/migrations.go` |
