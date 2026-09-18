# Architecture

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go **1.26.6** (`go.mod`) |
| Module | `github.com/maxghenis/openmessage` |
| Legacy DB | SQLite via `modernc.org/sqlite` — `messages.db`, WAL, FK on, 5s busy timeout |
| V2 DB | `<data-dir>/v2/store.sqlite3` + `<data-dir>/v2/blobs` |
| Google Messages | `go.mau.fi/mautrix-gmessages` → replaced by `github.com/MaxGhenis/gmessages` |
| WhatsApp | `go.mau.fi/whatsmeow` |
| Signal | local `signal-cli` process bridge |
| Slack | `github.com/slack-go/slack` + `internal/slacklive` |
| MCP | `github.com/mark3labs/mcp-go` |
| TUI | Bubble Tea / Bubbles / Lip Gloss |
| Logging | `zerolog` |
| Vault | DPAPI (Windows), Keychain (macOS), Secret Service via `secret-tool` (Linux) in `internal/vault`; insecure test path via `OPENMESSAGES_VAULT_INSECURE=1` |

## Design patterns

- **Daemon vs client:** one process owns live transports and the write-heavy repair sweeps; other processes attach read-only / proxy writes.
- **Bridge + adapters:** `internal/bridge` defines contracts, capability registry, dispatch leases, supervisors. Platform glue lives in `internal/bridgeadapters/{google,whatsapp,signal,slack}`.
- **Rivers:** account/workspace instances (`internal/river`). Built-in rivers are `messages-default`, `whatsapp-default`, and `signal-default`. Extra live accounts are `whatsapp-2` / `signal-2` (palette `>add …`); Slack rivers are `slack-<team-id>`. Extra WhatsApp/Signal sessions live under `rivers/<id>/` with namespaced conversation IDs. Google Messages is one live client.
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

**Rule:** exactly one process may hold WhatsApp / signal-cli / Google live sessions. A second MCP stdio process with transports will log out WhatsApp and corrupt Signal. See [../agent-runbook.md](../agent-runbook.md). Store-owning `serve` also holds `<data-dir>/instance.lock` so `backup` / `migrate` / `repair --apply` / a second daemon fail closed; MCP-stdio clients and `--demo` skip the lock.

## Storage: legacy (v1) vs V2

| Mode | Trigger | Reads | Writes / sends |
|---|---|---|---|
| Legacy primary (default) | unset V2 flags | `messages.db` | direct bridge + legacy rows |
| V2 send/ingest (shadow) | `OPENMESSAGES_V2_SEND` / `_INGEST` | mostly legacy | durable outbox/inbox; projector → legacy |
| V2 primary | default on after migrate (set `OPENMESSAGES_V2_PRIMARY=0` to stay on v1). Fresh empty data dirs create `v2/store.sqlite3`. A non-empty `messages.db` without a real v2 store still requires `om-tui migrate`. | `v2read` | outbox only. Requires Slack migrate + outbox so TUI send on PRIMARY uses `/api/v1/outbox`. `v2read` now scopes search by river (messages and conversation titles), derives unread from local-installation cursors, `/api/mark-read` writes those cursors natively, and contacts/people/stats/story plus person/story/viz MCP tools read through the same seam. Transcripts land in v2 `message_extras`; `import_messages` dual-writes v1 then `SyncInto` the requested platform. Favorite and mute persist on v2 conversations. MCP `react_to_message` on the daemon uses the v2 outbox when primary. MCP `send_media_to_conversation` submits native v2 conversation IDs; `send_message` / `send_group_message` fail closed rather than minting a v1 thread. `draft_message`, Google contact sync, legacy `/api/drafts`, tabs, and `/api/new-conversation` stay frozen-v1 and 409. |

Cutover path: `om-tui backup` → `om-tui migrate` → restart (PRIMARY is the default; `OPENMESSAGES_V2_PRIMARY=0` keeps v1). Details in [../migration-backup.md](../migration-backup.md). Frozen `messages.db` is a rollback fossil only — PRIMARY does not project new rows back into it.

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

Google live frames tee at the adapter. Startup/shallow/deep backfill (`FetchMessages`) tees the same way via `GoogleHistoryIngest` so PRIMARY readers see the offline gap. Legacy `storeMessage` still writes frozen `messages.db`.

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

### Send (Slack)

```
v1 serving store (default):
  API/TUI → app.SendSlackText(reply_to_id) → slacklive.Client.SendText
  → chat.postMessage [thread_ts]

V2 primary:
  TUI SubmitText → SubmitTextV2 → outbox → slack adapter SendText
  → reconstructs slack:TEAM:CHANNEL from account + remote channel id
  → slacklive.Client.SendText
  TUI SubmitMedia → SubmitMediaV2 → outbox → slack adapter SendMedia
  → files.getUploadURLExternal + upload + files.completeUploadExternal
  TUI Ctrl+E → SubmitReactionV2 → reactions.add/remove
  TUI o/s → DownloadMedia → files.info + private URL

Slack ingest (v1 serving store):
  users.list/users.info → durable per-river identity cache
  conversations.list → stream metadata
  conversations.history + per-channel cursors → legacy messages.db
  optional Socket Mode events → same ingest path
  conversations.replies → dedicated TUI reply-thread view

Slack V2 (ingest on, or after migrate):
  slacklive capture → slack.event frames → ingest.SlackDecoder
  → v2 accounts keyed by river id (`slack-<team>`)
  migrate maps slack:TEAM:CHANNEL → remote channel id, message SourceID → ts
```

v1 is frozen except critical bugs. Slack Block Kit is interactive in the TUI: URL buttons open in the browser; app-owned controls (Approve, workflow, selects without a URL) open the Slack desktop deep link for that message, because Slack has no public “click this button as the user” API. Do not grow `messages.db` for it. Capture prefers Block Kit flatten over Slack's short `text` fallback, stores the JSON on v2 `message_extras`, and `v2read` re-renders layout plus `block_actions`. Slack reaction mutation, file download/send, and Block Kit/attachment text fallback run on live capture (v1 store + V2 ingress).

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
| Chrome (optional) | Pasted Gaia cookies for TUI/CLI pair; native self-heal when decryptable | Current Windows Chrome stores v20 app-bound cookies om-tui cannot unwrap (Chrome elevation COM path-validates callers). Paste refreshes an existing session; Gaia re-pair only if reconnect fails |
| WhatsApp account | companion device | logout if second process links |
| `signal-cli` ≥ 0.14.5 + JRE 25 | Signal live | poison-message crash-loop on older signal-cli; class-file 69 on Java < 25 |
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
