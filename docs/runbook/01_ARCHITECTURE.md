# Architecture

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go **1.25.0** (`go.mod`) |
| Module | `github.com/maxghenis/openmessage` |
| Legacy DB | SQLite via `modernc.org/sqlite` — `messages.db`, WAL, FK on, 5s busy timeout |
| V2 DB | `<data-dir>/v2/store.sqlite3` + `<data-dir>/v2/blobs` |
| Google Messages | `go.mau.fi/mautrix-gmessages` → replaced by `github.com/MaxGhenis/gmessages` |
| WhatsApp | `go.mau.fi/whatsmeow` |
| Signal | local `signal-cli` process bridge |
| Slack | `github.com/slack-go/slack` + `internal/slacklive` (fork WIP) |
| MCP | `github.com/mark3labs/mcp-go` |
| TUI | Bubble Tea / Bubbles / Lip Gloss |
| Logging | `zerolog` |
| Vault | DPAPI on Windows (`internal/vault`); insecure test path via `OPENMESSAGES_VAULT_INSECURE=1` |
| macOS shell | Swift package under `macos/OpenMessage` |
| Local web UI | Embedded static assets in `internal/web/static` |
| Marketing site | `site/` → Vercel (`vercel.json` at **repo root**, `outputDirectory`-style build of `site`) |
| E2E | Playwright (`npm run test:e2e`) |

## Design patterns

- **Daemon vs client:** one process owns live transports and the write-heavy repair sweeps; other processes attach read-only / proxy writes.
- **Bridge + adapters:** `internal/bridge` defines contracts, capability registry, dispatch leases, supervisors. Platform glue lives in `internal/bridgeadapters/{google,whatsapp,signal,slack}`.
- **Rivers:** account/workspace instances (`internal/river`). Built-in Messages river is `messages-default`; Slack rivers are `slack-<team-id>`.
- **Durable V2 pipeline (staged):** inbox → decoder → normalized events → V2 repos; outbox → `messaging.MessageService` → bridge dispatch.
- **Legacy compatibility:** when V2 send is on but not primary, a projector mirrors confirmed V2 sends into legacy `messages.db` for legacy UI.

## Daemon vs client

| Entry | Constructor | Transports | Startup repair |
|---|---|---|---|
| `serve` (web / api / mcp-sse / `--transports`) | `app.New` | owns Google/WA/Signal/(Slack) | yes |
| `send`, `import`, `pair`, … | `app.New` | as needed | yes |
| `serve --mcp-stdio` (default) | `app.NewClient` | **none** — proxies to daemon | **no** |
| `read` / `status` | `app.NewClient` | none | **no** |
| `tui` | attaches via `localapi` | uses daemon | n/a |

**Rule:** exactly one process may hold WhatsApp / signal-cli / Google live sessions. A second MCP stdio process with transports will log out WhatsApp and corrupt Signal. See [../agent-runbook.md](../agent-runbook.md).

## Storage: legacy (v1) vs V2

| Mode | Trigger | Reads | Writes / sends |
|---|---|---|---|
| Legacy primary (default) | unset V2 flags | `messages.db` | direct bridge + legacy rows |
| V2 send/ingest (shadow) | `OPENMESSAGES_V2_SEND` / `_INGEST` | mostly legacy | durable outbox/inbox; projector → legacy |
| V2 primary | `OPENMESSAGES_V2_PRIMARY=1` (+ migrated store) | `v2read` | outbox only; story/person/viz MCP tools **unavailable** |

Cutover path: `openmessage backup` → `openmessage migrate` → set `OPENMESSAGES_V2_PRIMARY=1`. Details in [../migration-backup.md](../migration-backup.md).

## Data flow

### Inbound (legacy Google)

```
libgm event → client.EventHandler → db upsert (message/conversation)
  → recency/unread → web event broker → UI/SSE
```

### Inbound (V2 live)

```
bridge supervisor → ingest.Sink (durable inbox)
  → single worker → platform decoder (google|whatsapp|signal)
  → V2 repositories (+ quarantine on poison)
  → v2wire notifier → UI invalidation
```

### Send (legacy)

```
CLI / web / MCP → app send OR daemon /api/send|/api/send-media|/api/react
  → platform bridge → legacy row status updates
```

### Send (V2)

```
/api/v1/outbox/* or V2 MCP → mirror metadata → durable outbox
  → messaging.MessageService → bridge registry dispatch
  → (if not primary) projector → legacy visibility
```

### Send (Slack, current fork)

```
API/TUI → app.SendSlackText(reply_to_id) → resolve river
  → slacklive.Client.SendText → chat.postMessage [thread_ts]

Slack ingest:
  users.list/users.info → durable per-river identity cache
  conversations.list → stream metadata
  conversations.history + per-channel cursors → legacy messages.db
  optional Socket Mode events → same ingest path
  conversations.replies → dedicated TUI reply-thread view
```

Not wired into V2 ingest/outbox yet.

### MCP client mode (`serve --mcp-stdio`)

```
MCP host spawns stdio process
  → probe daemon /api/status (data dir + v2-primary truth)
  → local reads (repair-free)
  → sends/reactions/status → daemon HTTP (localapi)
```

Never opens its own WhatsApp/Signal/Google connection unless `--transports` is forced.

### HTTP surface

Loopback server (default `127.0.0.1:7007`, override `OPENMESSAGES_HOST` / `OPENMESSAGES_PORT`):

- REST + SSE under `/api/*`
- Optional MCP Streamable HTTP `/mcp` and SSE `/mcp/sse` (`--mcp-sse`)
- Auth via `control.token` in the data dir (`internal/localapi` / web control)

## External dependencies

| Dependency | Purpose | Failure mode |
|---|---|---|
| Android phone + Google Messages | SMS/RCS linked device | zombie session / `needs_repair` |
| Chrome cookies (optional) | Google account pair + self-heal | auth_expired until refresh |
| WhatsApp account | companion device | logout if second process links |
| `signal-cli` ≥ 0.14.5 | Signal live | poison-message crash-loop on older |
| Slack user token (`xoxp-…`) | Slack river history/read/send + identity | DPAPI-bound vault on Windows |
| Slack app token (`xapp-…`, optional) | Socket Mode realtime events | Polling remains fallback |
| Claude / MCP host | agent tools | misconfigured transports → fratricide |
| Vercel | marketing site only | unrelated to local inbox |

## Default ports / paths

| Item | Default |
|---|---|
| HTTP | `127.0.0.1:7007` |
| CLI data dir (macOS/Linux) | `~/.local/share/openmessage` |
| CLI data dir (Windows) | `%LOCALAPPDATA%\OpenMessage` |
| macOS app data dir | `~/Library/Application Support/OpenMessage` |
| Legacy DB | `<data-dir>/messages.db` |
| Google session | `<data-dir>/session.json` |
| WhatsApp session | `<data-dir>/whatsapp-session.db` |
| Signal | `<data-dir>/signal-cli/` |
| River credentials | `<data-dir>/rivers/<id>/credentials.enc` |
| Control token | `<data-dir>/control.token` |
