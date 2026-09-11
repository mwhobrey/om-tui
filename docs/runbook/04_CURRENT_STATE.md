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
| Rivers model (`messages-default`, Slack rivers) | Shipped in `internal/river`, `internal/db/rivers*` |
| Vault (DPAPI Windows, Keychain macOS, Secret Service Linux) | Shipped; insecure path for tests only |
| `pair slack --token [--app-token]` | Shipped; optional `xapp` token enables Socket Mode |
| Slack identity + readable mrkdwn | Durable per-river user cache; DM/sender/mention/channel names resolved |
| Slack sync + unread + history | Incremental per-channel cursors, dedupe-safe IDs, lazy older pages, SSE invalidation |
| Slack threads | Dedicated TUI thread view, `conversations.replies`, reply-in-thread send |
| Slack realtime | Optional Socket Mode; polling remains startup/reconnect fallback |
| Bridge Slack adapter | Thin registry entry; text-send capability; **not** on V2 stack |
| TUI Google Account pairing (`p`, paste curl, emoji confirm) | Shipped this branch; Chrome auto-read parked (v20). Daemon parks Google then runs Gaia; `Esc` cancels, `q` closes the overlay |
| TUI-owned daemon dies with the TUI (Windows job object) | Shipped this branch; closing the terminal no longer leaves `om-tui.exe` serving |
| API: `/api/rivers`, conversation `river_id` filter | Shipped |
| Google device ID-space reset repair (`repair google-idspace`) | Ported from upstream |
| Docs: `docs/tui.md` + `docs/runbook/` + `NOTICE.md` | Present |
| macOS app, web UI, marketing site | Removed — see NOTICE.md; upstream maintains its own |

## Explicitly incomplete / broken / out of scope

| Item | Notes |
|---|---|
| Slack → V2 ingest/outbox | No Slack decoder / V2 account wiring |
| Slack media / reaction mutation / Block Kit | Text-first; expand later |
| V2 primary as default | Still opt-in; story/stats/viz unavailable when primary; person-history MCP tools read V2 |
| Daemon honors `instance.lock` | Still backup/migrate-only |
| Native GUI / desktop notifications on macOS/Linux | Non-goal for om-tui; Windows-only toasts today |
| QR Google pair | Dead for many accounts — TUI paste / `pair --google` cookie method |
| TUI Google Chrome cookie auto-read | Parked: current Chrome encrypts Gaia cookies (v20). Pair by pasting a `messages.google.com` curl |
| CLAUDE.md / README / agent-runbook | Synced for the TUI-only, cross-platform fork; prefer this runbook if anything drifts |

## Immediate next steps (suggested)

1. **Keep watching the Slack daily-driver** — already in use and working after a multi-day idle; still worth checking names, unread, threads, older history, and Socket Mode after reconnects.
2. **Decide V2 posture for Slack** — keep Slack legacy-only until V2 primary is real, or add decoder + outbox before cutover.
3. Smoke-test the macOS/Linux vault backends (Keychain, Secret Service) on real hardware — only cross-compile-checked so far, not runtime-verified.
4. V2 parity for remaining story/stats/viz MCP tools (`conversation_stats`, `person_stats`, `generate_story`, `generate_person_story`, `generate_viz`, `render_story`). `get_person_messages` / `_range` already read through `ReadSource`.

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
