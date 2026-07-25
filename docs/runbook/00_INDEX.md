# OpenMessage — Master Runbook

Local-first messaging workspace and universal message database. Stores conversations on-device, exposes them via localhost HTTP/SSE, a Bubble Tea TUI, a macOS app, and MCP clients.

## North Star

One local inbox for every messaging river you care about — searchable, agent-accessible, and owned by you — without cloud sync of your message history.

## This checkout

| Remote | URL |
|---|---|
| `origin` | `https://github.com/mwhobrey/om-tui.git` (Windows TUI / rivers fork) |
| `upstream` | `https://github.com/MaxGhenis/openmessage` |

Default branch: `main`. Go module path remains `github.com/maxghenis/openmessage`.

## Federated TOC

| File | Pull when you need… |
|---|---|
| [01_ARCHITECTURE.md](./01_ARCHITECTURE.md) | Stack, daemon/client split, v1↔v2, data flow |
| [02_COMPONENTS_AND_FILES.md](./02_COMPONENTS_AND_FILES.md) | Where code lives and package responsibilities |
| [03_RULES_AND_STANDARDS.md](./03_RULES_AND_STANDARDS.md) | Conventions, env vars, tests/CI, gotchas |
| [04_CURRENT_STATE.md](./04_CURRENT_STATE.md) | What works, WIP, next steps |

## Related operational docs (do not ignore)

| Doc | Role |
|---|---|
| [../agent-runbook.md](../agent-runbook.md) | Live-install support: dual data dirs, WAL, MCP transport ownership, re-pair recipes |
| [../windows-tui.md](../windows-tui.md) | Windows TUI + Slack rivers product path |
| [../migration-backup.md](../migration-backup.md) | Offline `backup` / cutover prep |
| [../release-checklist.md](../release-checklist.md) | Pre-release dogfood + privacy checklist |
| [../../CLAUDE.md](../../CLAUDE.md) | Agent-facing project overview (kept in sync with this runbook) |

## Fast paths

```bash
# Build
go build -o openmessage.exe .          # Windows
go build -o openmessage .

# Read-only CLI (no live transports; repair-free store open)
openmessage status --json
openmessage read "query" --limit 20

# Windows daily driver
openmessage pair
openmessage tui                        # spawns serve --api --no-web if needed

# Daemon shapes
openmessage serve                      # web UI (default port 7007)
openmessage serve --api --no-web       # API+SSE for TUI
openmessage serve --mcp-stdio          # transportless MCP client (default)

# Tests
go test ./...
go test -race ./...
```

## Platforms (by maturity)

| Platform | Live | Import | Notes |
|---|---|---|---|
| Google Messages (SMS/RCS) | yes | — | libgm / mautrix-gmessages fork |
| WhatsApp | yes | text export / Desktop | whatsmeow; single-owner session |
| Signal | yes | Desktop | local `signal-cli` ≥ 0.14.5 |
| Slack | yes (this fork) | — | rivers + vault; text send + recent sync; not on V2 ingest |
| Google Chat | — | Takeout | importer only |
| iMessage | — | `chat.db` | macOS Full Disk Access |
