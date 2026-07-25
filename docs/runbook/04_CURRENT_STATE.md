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

## Working on this fork (uncommitted / in-flight at runbook generation)

Evidence: dirty worktree with Slack/rivers/vault/TUI changes (~900+ LOC tracked delta plus untracked packages). Treat as **active WIP**, not released product.

| Area | Status |
|---|---|
| Rivers model (`messages-default`, Slack rivers) | Implemented in `internal/river`, `internal/db/rivers*` |
| Vault (DPAPI Windows) | Implemented; insecure path for tests |
| `pair slack --token` | Implemented |
| Slack live client + recent sync + text send | Implemented (`slacklive`, `app` Slack paths) |
| Bridge Slack adapter | Thin registry entry; text-send capability; **not** on V2 stack |
| TUI river switcher, filter/search, broadcast, media open/save/paste, reactions | Substantial TUI expansion |
| API: `/api/rivers`, conversation `river_id` filter | Present in fork changes |
| Docs: `docs/windows-tui.md` | Written for the Windows path |

## Explicitly incomplete / broken / out of scope

| Item | Notes |
|---|---|
| Slack → V2 ingest/outbox | No Slack decoder / V2 account wiring |
| Slack media / reactions / full RTM richness | Text-first; expand later |
| V2 primary as default | Still opt-in; story/person/viz unavailable when primary |
| Daemon honors `instance.lock` | Still backup/migrate-only |
| Native Windows GUI | Non-goal for om-tui |
| Live WhatsApp/Signal on Windows TUI product path | Documented non-goals |
| QR Google pair | Dead for many accounts — cookie method only |
| CLAUDE.md platform list | May lag Slack/rivers; prefer this runbook + `windows-tui.md` |

## Immediate next steps (suggested)

1. **Stabilize Slack river MVP** — pair → sync → TUI list/send → vault round-trip on one Windows machine; smoke checklist in `windows-tui.md`.
2. **Decide V2 posture for Slack** — either keep Slack legacy-only until V2 primary is real, or add decoder + outbox path before cutting over.
3. **Land or split the WIP** — Slack/vault/rivers vs pure TUI UX vs backup tweaks as separate commits/PRs if targeting upstream.
4. **Keep transport ownership tests green** when wiring Slack into `serve` supervisors.
5. **Refresh CLAUDE.md / README Windows section** once Slack ships so agents don't miss rivers.

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
