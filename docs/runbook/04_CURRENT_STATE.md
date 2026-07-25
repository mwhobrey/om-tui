# Current state

Snapshot for this checkout (`mwhobrey/om-tui` fork of `MaxGhenis/openmessage`). Update this file when maturity shifts.

## Working (shipped upstream + usable here)

- Google Messages pair / live sync / send (legacy path); cookie-based account pair; self-heal paths on macOS
- WhatsApp + Signal live bridges on daemon shapes (macOS/Linux primary; respect single-owner rule)
- Local web UI + REST/SSE on `serve`
- MCP stdio **client mode** (transportless) + optional `--mcp-sse`
- Repair-free `app.NewClient` for `read` / `status` / MCP clients
- Multi-platform importers (gchat, imessage, whatsapp, signal desktop)
- Story / stats / viz MCP tools on **legacy** store
- Offline `backup` + `migrate` toward V2 (staged flags)
- macOS app packaging + CI (Go, race, E2E, Swift, gmessages fork drift)
- Windows defaults: `%LOCALAPPDATA%\OpenMessage`, TUI entrypoint

## Working on this fork (committed on `main`)

| Area | Status |
|---|---|
| Rivers model (`messages-default`, Slack rivers) | Shipped in `internal/river`, `internal/db/rivers*` |
| Vault (DPAPI Windows) | Shipped; insecure path for tests |
| `pair slack --token [--app-token]` | Shipped; optional `xapp` token enables Socket Mode |
| Slack identity + readable mrkdwn | Durable per-river user cache; DM/sender/mention/channel names resolved |
| Slack sync + unread + history | Incremental per-channel cursors, dedupe-safe IDs, lazy older pages, SSE invalidation |
| Slack threads | Dedicated TUI thread view, `conversations.replies`, reply-in-thread send |
| Slack realtime | Optional Socket Mode; polling remains startup/reconnect fallback |
| Bridge Slack adapter | Thin registry entry; text-send capability; **not** on V2 stack |
| TUI river switcher, filter/search, broadcast, media open/save/paste, reactions | Shipped; context-aware help; `Ctrl+K` universal palette (all-river jump, `>` commands, frecency, `commands.json`, `>msg contact::body`) |
| API: `/api/rivers`, conversation `river_id` filter | Shipped |
| Docs: `docs/windows-tui.md` + `docs/runbook/` | Present |

## Explicitly incomplete / broken / out of scope

| Item | Notes |
|---|---|
| Slack → V2 ingest/outbox | No Slack decoder / V2 account wiring |
| Slack media / reaction mutation / Block Kit | Text-first; expand later |
| V2 primary as default | Still opt-in; story/person/viz unavailable when primary |
| Daemon honors `instance.lock` | Still backup/migrate-only |
| Native Windows GUI | Non-goal for om-tui |
| Live WhatsApp/Signal on Windows TUI product path | Documented non-goals |
| QR Google pair | Dead for many accounts — cookie method only |
| CLAUDE.md / README / agent-runbook | Synced for rivers + Windows data dirs; prefer this runbook if anything drifts |
| Fork CI | Actions appear disabled on `mwhobrey/om-tui` |

## Immediate next steps (suggested)

1. **Dogfood the Slack daily-driver path** — names, unread filters, dedicated threads, older history, and optional Socket Mode.
2. **Decide V2 posture for Slack** — keep Slack legacy-only until V2 primary is real, or add decoder + outbox before cutover.
3. **Optional:** enable Actions on the fork, or add a Windows-focused smoke script that skips the POSIX-baseline failures.
4. ~~Refresh CLAUDE.md / README~~ — done; prefer this runbook when docs disagree.

## How to verify right now

Go comes from `mise` on this box — see the toolchain note in
[03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md).

```powershell
$env:PATH = "$env:LOCALAPPDATA\mise\installs\go\1.26.5\bin;" + $env:PATH
go build ./... ; go vet ./...
go test ./internal/river/ ./internal/vault/ ./internal/db/ ./internal/slacklive/ ./internal/localapi/ ./internal/tui/ ./internal/app/ -count=1
.\openmessage.exe status --json
# With a token:
.\openmessage.exe pair slack --token xoxp-... --app-token xapp-... --name "Test"
.\openmessage.exe serve --api --no-web
.\openmessage.exe tui
```

As of the rivers/TUI push: `go build ./...` and `go vet ./...` are clean, and every
package touched by that work passes on Windows. The remaining `go test ./...` failures
match the pre-existing Windows baseline documented in
[03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md).

Live support / re-pair / MCP fratricide: always start from [../agent-runbook.md](../agent-runbook.md).
