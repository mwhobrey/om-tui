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
| `pair slack --token` | Shipped |
| Slack live client + recent sync + text send | Shipped (`slacklive`, `app` Slack paths) |
| Bridge Slack adapter | Thin registry entry; text-send capability; **not** on V2 stack |
| TUI river switcher, filter/search, broadcast, media open/save/paste, reactions | Shipped; ghost/`s`-key media bugs fixed |
| API: `/api/rivers`, conversation `river_id` filter | Shipped |
| Docs: `docs/windows-tui.md` + `docs/runbook/` | Present |

## Explicitly incomplete / broken / out of scope

| Item | Notes |
|---|---|
| Slack → V2 ingest/outbox | No Slack decoder / V2 account wiring |
| Slack media / reactions / full RTM richness | Text-first; expand later |
| Slack realtime / websocket events | Recent-history sync on start; live push not confirmed |
| V2 primary as default | Still opt-in; story/person/viz unavailable when primary |
| Daemon honors `instance.lock` | Still backup/migrate-only |
| Native Windows GUI | Non-goal for om-tui |
| Live WhatsApp/Signal on Windows TUI product path | Documented non-goals |
| QR Google pair | Dead for many accounts — cookie method only |
| CLAUDE.md / README / agent-runbook | Synced for rivers + Windows data dirs; prefer this runbook if anything drifts |
| Fork CI | Actions appear disabled on `mwhobrey/om-tui` |

## Immediate next steps (suggested)

1. **Keep dogfooding Slack + TUI** — file polish bugs as they appear (ghosts, unread flapping, Slack send quirks).
2. **Decide V2 posture for Slack** — keep Slack legacy-only until V2 primary is real, or add decoder + outbox before cutover.
3. **Optional:** enable Actions on the fork, or add a Windows-focused smoke script that skips the POSIX-baseline failures.
4. ~~Refresh CLAUDE.md / README~~ — done; prefer this runbook when docs disagree.

## How to verify right now

Go comes from `mise` on this box — see the toolchain note in
[03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md).

```powershell
$env:PATH = "$env:LOCALAPPDATA\mise\installs\go\1.26.5\bin;" + $env:PATH
go build ./... ; go vet ./...
go test ./internal/river/ ./internal/vault/ ./internal/db/ ./internal/tui/ ./internal/app/ -count=1
.\openmessage.exe status --json
# With a token:
.\openmessage.exe pair slack --token xoxp-... --name "Test"
.\openmessage.exe serve --api --no-web
.\openmessage.exe tui
```

As of the rivers/TUI push: `go build ./...` and `go vet ./...` are clean, and every
package touched by that work passes on Windows. The remaining `go test ./...` failures
match the pre-existing Windows baseline documented in
[03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md).

Live support / re-pair / MCP fratricide: always start from [../agent-runbook.md](../agent-runbook.md).
