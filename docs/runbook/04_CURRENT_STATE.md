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
| Extra live WhatsApp / Signal pair | Palette `>add whatsapp` / `>add signal`; isolated session dirs; namespaced conversation IDs; extra start is Slack-shaped `ConnectIfPaired` |
| Vault (DPAPI Windows, Keychain macOS, Secret Service Linux) | Shipped for Slack (and optional Google session copy); insecure path for tests only |
| `pair slack --token [--app-token]` | Shipped; optional `xapp` token enables Socket Mode |
| WhatsApp / Signal TUI QR pair | Shipped; `[` / `]` to the river, `p` shows the QR (sixel in Windows Terminal, Kitty protocol in Kitty/Ghostty/WezTerm, half-block cells elsewhere); default sessions stay in `whatsapp-session.db` / `signal-cli/`; extras under `rivers/<id>/` |
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
| V2 primary as default | Still opt-in; story/person/viz unavailable when primary |
| Daemon honors `instance.lock` | Still backup/migrate-only |
| Native GUI / desktop notifications on macOS/Linux | Non-goal for om-tui; Windows-only toasts today |
| QR Google pair | Dead for many accounts — TUI paste / `pair --google` cookie method |
| TUI Google Chrome cookie auto-read | Parked: current Chrome encrypts Gaia cookies (v20). Pair by pasting a `messages.google.com` curl. Windows v20 cannot self-heal; `SESSION_COOKIE_INVALID` after hours is expected |
| Signal QR on Windows | `script` PTY wrapper was Unix-only; Windows now runs `signal-cli link` directly. 0.14.8 needs JRE 25 — om-tui injects a discovered JDK (scoop `temurin25-jdk`) over stale `JAVA_HOME` |
| CLAUDE.md / README / agent-runbook | Synced for the TUI-only, cross-platform fork; prefer this runbook if anything drifts |

## Immediate next steps (suggested)

1. **Dogfood WhatsApp / Signal rivers in the TUI** — `[` / `]` onto the river, `p` to scan the QR, send/receive, `r` reconnect.
2. **Dogfood the Slack daily-driver path** — names, unread filters, dedicated threads, older history, and optional Socket Mode.
3. **Decide V2 posture for Slack** — keep Slack legacy-only until V2 primary is real, or add decoder + outbox before cutover.
4. Smoke-test the macOS/Linux vault backends (Keychain, Secret Service) on real hardware — only cross-compile-checked so far, not runtime-verified.
5. V2 parity for `get_person_messages` / person-story MCP tools (deferred while V2 is the serving store; PR #13 in flight).

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
