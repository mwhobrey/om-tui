# Rules and standards

## Coding conventions

- Go is the source of truth for product behavior. Match existing package style: small focused packages under `internal/`, CLI thin wrappers in `cmd/`.
- Prefer surgical diffs. Do not rewrite working legacy paths "for cleanliness" while V2 cutover is staged.
- New platform work goes through `bridge` contracts + adapters; do not teach `web` or `tools` about raw SDK clients.
- Tests live next to code (`*_test.go`). Characterization / contract tests exist for Google, WhatsApp, MCP parity, and serve shapes — keep them green when changing transport ownership.
- No new dependencies without a clear need; state the install/`go get` explicitly.
- Commit format for this fork's personal workflow: `[TICKET] :gitmoji: type(scope): summary` (see personas base). Upstream MaxGhenis history may differ.

## Error handling

- Outgoing legacy rows use status progression: `OUTGOING_SENDING` → `OUTGOING_SENT` / `OUTGOING_DELIVERED`, or `OUTGOING_FAILED:<STATUS>`.
- Google linked-device death: `google.connected=true` can lie; after repeated send failures `needs_repair` surfaces. Prefer self-heal (cookie refresh) before re-pair.
- MCP / CLI client mode: if the daemon is down, **fail with an actionable start-daemon error** — never fall back to opening transports.
- V2 ingest: retryable DB errors stay in inbox; poison frames go to quarantine (`internal/ingest`).
- Backup/migrate: distinct exit codes for active backend, lock, preflight, copy, verify failures ([../migration-backup.md](../migration-backup.md)).

## State management rules

1. **One data dir per install.** Pair, serve, tui, MCP must share `OPENMESSAGES_DATA_DIR`.
2. **One transport owner.** Daemon owns Google/WA/Signal/(Slack). MCP stdio is transportless by default.
3. **`app.New` vs `app.NewClient`:** only store-owning processes run repair sweeps.
4. **Do not sqlite3 the live DB** while the daemon holds WAL — use HTTP API.
5. **V2 primary freezes legacy reads** for story/person/viz MCP tools; do not "fix" by reading stale `messages.db` after cutover.
6. **Windows vault is DPAPI-bound** to user/machine — backups of `credentials.enc` are useless on another box without re-pair.

## Environment variables (high-signal)

| Variable | Role |
|---|---|
| `OPENMESSAGES_DATA_DIR` | Store root (override defaults) |
| `OPENMESSAGES_LOG_LEVEL` | Zerolog level |
| `OPENMESSAGES_HOST` / `OPENMESSAGES_PORT` | Loopback bind |
| `OPENMESSAGES_DEMO` | Isolated fake-data store; no live transports |
| `OPENMESSAGES_V2_SEND` / `_INGEST` / `_PRIMARY` | Staged V2 enablement |
| `OPENMESSAGES_SIGNAL_CLI` | signal-cli binary path |
| `OPENMESSAGE_COOKIE_REFRESH_SCRIPT` / `OPENMESSAGE_CHROME_PROFILE` | Google self-heal |
| `OPENMESSAGE_REPAIR_MIN_INTERVAL` | Pace Google repair |
| `OPENMESSAGES_EXPORT_DIR` / `OPENMESSAGES_ALLOW_ANY_EXPORT_PATH` | Viz/export path policy |
| `OPENMESSAGES_VAULT_INSECURE` | Non-Windows test-only vault |
| `OPENMESSAGES_WINDOWS_NOTIFICATIONS` / `_TOAST_APP_ID` | Windows toasts |
| `OPENMESSAGE_TELEMETRY` | Opt-in heartbeat |
| `OPENMESSAGES_KLIPY_API_KEY` / `KLIPY_API_KEY` | GIF search |

Full operational recipes: [../agent-runbook.md](../agent-runbook.md).

## Testing & CI

### Local

```bash
go test ./...
go test -race ./...
go build .
npm ci && npx playwright install --with-deps chromium && npm run test:e2e
# macOS only:
swift test --package-path macos/OpenMessage
bash macos/build.sh
```

### CI (`.github/workflows/`)

| Workflow | What |
|---|---|
| `test.yml` | `go test` + **40% coverage floor**, race suite, Playwright E2E (Node 22), site build, Swift build/test + package |
| `release.yml` | Tagged/manual CLI artifacts + macOS DMG / checksums |
| `gmessages-fork-drift.yml` | Weekly: pinned gmessages fork stays exactly one carried patch over recorded base |

### Regression tests that encode hard rules

- MCP stdio starts **zero** transport supervisors
- `app.NewClient` performs **no** store repair writes
- Built binary MCP client shape starts no transports

Do not weaken these when "simplifying" serve.

## Gotchas (read before support work)

1. **Two data dirs on macOS** — App Support vs `~/.local/share/openmessage`. CLI without env hits the stale one.
2. **WAL lock** — direct `sqlite3` → error 14 or missing recent rows. Use `/api/*`.
3. **MCP + transports = fratricide** — WhatsApp logout / Signal deauth within seconds.
4. **Pin MCP `OPENMESSAGES_DATA_DIR`** (and `OPENMESSAGES_V2_PRIMARY=1` post-cutover) in `~/.mcp.json`.
5. **Google QR is dead** for many accounts — cookie / Google Account pairing.
6. **Clear `session.json` in both dirs** to force unpaired, or migration restores it.
7. **Don't thrash Google reconnect/pair** — account throttling.
8. **`instance.lock` is advisory** for backup/migrate only; daemon does not yet honor it.
9. **`go.work` overrides** can make dependency bumps look ignored.
10. **Keep PATH binary = app binary** — schema migrations from a newer CLI against an older app are hostile.
11. **Windows non-goals** (this fork): native GUI/tray, iMessage, live WA/Signal on Windows TUI path, inline media previews, MSI — see [../windows-tui.md](../windows-tui.md).

## MCP tools (24)

Registered in `internal/tools/tools.go` → `RegisterWithOptions`:

`get_messages`, `get_conversation`, `search_messages`, `send_message`, `send_to_conversation`, `send_media_to_conversation`, `react_to_message`, `set_message_transcript`, `list_conversations`, `list_contacts`, `resolve_contact_routes`, `get_status`, `draft_message`, `download_media`, `import_messages`, `get_person_messages`, `conversation_stats`, `generate_story`, `person_stats`, `generate_person_story`, `generate_viz`, `get_person_messages_range`, `render_story`, `send_group_message`

Person/story/viz tools return unavailable while V2 is the serving store. Message-content results prepend an untrusted-content warning.
