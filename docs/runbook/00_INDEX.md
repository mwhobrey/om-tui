# om-tui — Master Runbook

Local-first, cross-platform terminal messaging client and universal message
database. Stores conversations on-device, exposes them via localhost
HTTP/SSE, a Bubble Tea TUI, and MCP clients. Fork of
[MaxGhenis/openmessage](https://github.com/MaxGhenis/openmessage) — see
[../../NOTICE.md](../../NOTICE.md) for full credit; om-tui drops the
upstream macOS app and web UI to focus solely on the TUI.

## North Star

One local inbox for every messaging river you care about — searchable,
agent-accessible, and owned by you — without cloud sync of your message
history, from one terminal client on Windows, macOS, or Linux.

## This checkout

| Remote | URL |
|---|---|
| `origin` | `https://github.com/mwhobrey/om-tui.git` |
| `upstream` | `https://github.com/MaxGhenis/openmessage` |

Default branch: `main`. Go module path remains
`github.com/maxghenis/openmessage` (kept unchanged on purpose — see
NOTICE.md — so upstream diffs stay easy to compare and port).

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
| [../agent-runbook.md](../agent-runbook.md) | Live-install support: data dir, WAL, MCP transport ownership, re-pair recipes |
| [../tui.md](../tui.md) | TUI + Slack rivers product path (all platforms) |
| [../migration-backup.md](../migration-backup.md) | Offline `backup` / cutover prep |
| [../release-checklist.md](../release-checklist.md) | Pre-release dogfood + privacy checklist |
| [../../SECURITY.md](../../SECURITY.md) | Vulnerability reporting (private advisory) |
| [../../CONTRIBUTING.md](../../CONTRIBUTING.md) | Issues belong here, not upstream; PR + CI gate |
| [../../NOTICE.md](../../NOTICE.md) | Fork origin + upstream library credit |
| [../../CLAUDE.md](../../CLAUDE.md) | Agent-facing project overview (kept in sync with this runbook) |

## Fast paths

```bash
# Build
go build -o om-tui.exe .          # Windows
go build -o om-tui .              # macOS/Linux

# Read-only CLI (no live transports; repair-free store open)
./om-tui status --json
./om-tui read "query" --limit 20

# Daily driver
./om-tui tui                      # spawns serve --api --no-web if needed
                                  # unpaired: p, paste a messages.google.com curl

# Standalone daemon
./om-tui serve --api --no-web     # API+SSE for TUI/MCP
./om-tui serve --mcp-stdio        # transportless MCP client (default)

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
| Slack | yes | — | rivers + vault; text send + recent sync; not on V2 ingest |
| Google Chat | — | Takeout | importer only |
| iMessage | — | `chat.db` | macOS Full Disk Access |
