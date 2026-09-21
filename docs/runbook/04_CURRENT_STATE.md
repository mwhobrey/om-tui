# Current state

Snapshot for this checkout (`mwhobrey/om-tui` fork of `MaxGhenis/openmessage`). Update this file when maturity shifts.

## Working (shipped upstream + usable here)

- Google Messages pair / live sync / send (legacy path); cookie-based account pair
- WhatsApp + Signal live bridges on daemon shapes (respect single-owner rule)
- Local REST/SSE API on `serve` (TUI + MCP clients; no bundled web UI on this fork)
- MCP stdio **client mode** (transportless) + optional `--mcp-sse`
- Repair-free `app.NewClient` for `read` / `status` / MCP clients
- Multi-platform importers (gchat, imessage, whatsapp, signal desktop)
- Story / stats / viz MCP tools on **legacy** store
- Offline `backup` + `migrate` toward V2 (staged flags)
- Cross-platform TUI entrypoint (Windows/macOS/Linux); data dir defaults per-OS

## Working on this fork (committed on `main`)

| Area | Status |
|---|---|
| Rivers model (`messages-default`, `whatsapp-default`, `signal-default`, extra `whatsapp-N`/`signal-N`, Slack rivers) | Shipped in `internal/river`, `internal/db/rivers*`, `internal/app/live_rivers.go` |
| Extra live WhatsApp / Signal pair | Palette `>add whatsapp` / `>add signal`; isolated session dirs; namespaced conversation IDs; extra start is Slack-shaped `ConnectIfPaired`; media/avatar download uses the conversation's river bridge |
| Vault (DPAPI Windows, Keychain macOS, Secret Service Linux) | Shipped for Slack (and optional Google session copy); insecure path for tests only |
| `pair slack --token [--app-token]` | Shipped; optional `xapp` token enables Socket Mode |
| WhatsApp / Signal TUI QR pair | Shipped; `[` / `]` to the river, `p` shows the QR (sixel in Windows Terminal, Kitty protocol in Kitty/Ghostty/WezTerm, half-block cells elsewhere); default sessions stay in `whatsapp-session.db` / `signal-cli/`; extras under `rivers/<id>/` |
| Slack identity + readable mrkdwn | Durable per-river user cache; DM/sender/mention/channel names resolved |
| Slack sync + unread + history | Incremental per-channel cursors, dedupe-safe IDs, lazy older pages, SSE invalidation |
| Slack threads | Dedicated TUI thread view, `conversations.replies`, reply-in-thread send |
| Slack realtime | Optional Socket Mode; polling remains startup/reconnect fallback |
| Bridge Slack adapter | Thin registry entry; text-send + reactions + media download/send; V2 account bootstrap per `slack-<team>` when ingest is on |
| TUI Google Account pairing (`p`, paste curl, emoji confirm) | Shipped this branch; Chrome auto-read parked (v20). A paste against an existing `session.json` refreshes cookies and reconnects first; Gaia + phone emoji is the unpaired / reconnect-failed fallback. `Esc` cancels, `q` closes the overlay |
| TUI-owned daemon dies with the TUI (Windows job object) | Shipped this branch; closing the terminal no longer leaves `om-tui.exe` serving |
| Daemon holds `instance.lock` | Store-owning `serve` acquires `<data-dir>/instance.lock` before `app.New`. MCP-stdio clients and `--demo` do not. `backup` / `migrate` / `repair --apply` / a second serve refuse while it is held |
| API: `/api/rivers`, conversation `river_id` filter | Shipped |
| Google device ID-space reset repair (`repair google-idspace`) | Ported from upstream |
| Docs: `docs/tui.md` + `docs/runbook/` + `NOTICE.md` | Present |
| macOS app, web UI, marketing site | Removed — see NOTICE.md; upstream maintains its own |

## Explicitly incomplete / broken / out of scope

| Item | Notes |
|---|---|
| Slack → V2 ingest/outbox | Decoder, per-team migrate, adapter `SendText` + `SendReaction` + `DownloadMedia` + `SendMedia`, TUI PRIMARY send, river-scoped search, unread + native mark-read, Slack Ctrl+T/PgUp live-ID remap both ways, Slack reaction mutation, file download/send, Block Kit layout via `message_extras` plus TUI `b`/`Ctrl+B` (URL buttons in-browser; app-owned controls via `slack://` — Slack has no public user-token click API), person/story/viz MCP, leftover v1 read surfaces, transcripts, platform `import_messages` SyncInto, favorite/mute, and MCP `react_to_message` on PRIMARY. Extra WA/Signal rivers migrate and ingest onto their own v2 accounts. PRIMARY is the compiled default after migrate; `OPENMESSAGES_V2_PRIMARY=0` stays on frozen v1. |
| V2 primary as default | Default on after a published `v2/store.sqlite3`. Fresh empty data dirs bootstrap an empty v2 store. A non-empty `messages.db` plus a missing/stub v2 store still requires `om-tui migrate`. Set `OPENMESSAGES_V2_PRIMARY=0` to serve v1. Contacts, people, stats/story, person/story/viz MCP, transcripts, import sync, conversation `source_platform` filters, conversation-name search, favorite/mute, MCP `react_to_message`, MCP `send_media_to_conversation`, drafts, tabs, contact CRM metadata, `/api/new-conversation` (DMs and SMS groups), PRIMARY `send_message` / `send_group_message` minting, and Google contact **sync** (`/api/contacts/sync` → v2 identities) use v2 when primary. TUI `n` (list focus) / palette **New chat** mints or reuses a DM via `/api/new-conversation` (SMS, WhatsApp, Signal). Type a phone number, or pick a contact already in the store (address book after a Google sync). Windows `backup` uses a drive-letter-safe SQLite DSN and file-copies on genuine VACUUM OOM. |
| Native GUI / desktop notifications on macOS/Linux | Non-goal for om-tui; Windows-only toasts today |
| QR Google pair | Dead for many accounts — TUI paste / `pair --google` cookie method |
| TUI Google Chrome cookie auto-read | Parked: current Chrome encrypts Gaia cookies (v20 / app-bound). Chrome's elevation COM path-validates callers, so silent unwrap from om-tui.exe is not viable. Paste a `messages.google.com` curl. If this PC is already paired, paste rewrites cookies and reconnects (no phone tap). The TUI auto-opens that paste overlay when `auth_expired` / `needs_repair` latches; Esc dismisses until the session is healthy again. Gaia + emoji only when there is no session or reconnect fails. `SESSION_COOKIE_INVALID` after hours is expected on Windows until cookies are pasted |
| Signal QR on Windows | `script` PTY wrapper was Unix-only; Windows now runs `signal-cli link` directly. 0.14.8 needs JRE 25 — om-tui injects a discovered JDK (scoop `temurin25-jdk`) over stale `JAVA_HOME` |
| CLAUDE.md / README / agent-runbook | Synced for the TUI-only, cross-platform fork; prefer this runbook if anything drifts |

## Immediate next steps (suggested)

1. **This Windows install is on PRIMARY.** `om-tui migrate` published `%LOCALAPPDATA%\OpenMessage\v2\store.sqlite3`. Frozen `messages.db` is the rollback fossil; shadow ingest was moved to `v2.shadow-*`. File-copy backup is in `migration-backups/`. Start the TUI from the rebuilt `om-tui.exe` so transports attach to v2 ingest/outbox. Google history backfill (startup FetchMessages) tees into v2; live frames already did. `OPENMESSAGES_V2_PRIMARY=0` is the v1 escape hatch. Google contact **sync** writes v2 identities; scheduled-send stays fail-closed. On Windows, Google cookies still expire (v20); paste refreshes the existing session without a phone tap unless the device was unlinked.
2. Smoke-test the macOS/Linux vault backends (Keychain, Secret Service) on real hardware — only cross-compile-checked so far, not runtime-verified.

## How to verify right now

Go comes from `mise` on this box — see the toolchain note in
[03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md).

```powershell
$env:PATH = "$env:LOCALAPPDATA\mise\installs\go\1.26.5\bin;" + $env:PATH
go build ./... ; go vet ./...
go test ./internal/river/ ./internal/vault/ ./internal/db/ ./internal/slacklive/ ./internal/localapi/ ./internal/tui/ ./internal/app/ -count=1
.\om-tui.exe status --json
# With a token:
.\om-tui.exe pair slack --token xoxp-... --app-token xapp-... --name "Test"
.\om-tui.exe serve --api --no-web
.\om-tui.exe tui
```

As of the rivers/TUI push: `go build ./...` and `go vet ./...` are clean, and every
package touched by that work passes on Windows. The remaining `go test ./...` failures
match the pre-existing Windows baseline documented in
[03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md).

Live support / re-pair / MCP fratricide: always start from [../agent-runbook.md](../agent-runbook.md).
