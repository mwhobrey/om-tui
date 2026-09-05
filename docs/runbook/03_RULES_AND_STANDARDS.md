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
```

### Go toolchain on this Windows box

Go is **not** on the system `PATH`. It is managed by `mise` (`go 1.26.5`, pinned in
`~/.config/mise/config.toml`). Either prefix commands or prepend the install bin:

```powershell
mise exec -- go test ./...
# or, once per shell session:
$env:PATH = "$env:LOCALAPPDATA\mise\installs\go\1.26.5\bin;" + $env:PATH
```

### Windows test baseline — many failures are expected

`go test ./...` on Windows fails in roughly 15 packages for POSIX assumptions in the
tests, **not** product bugs. Verified identical on a clean pre-change worktree, so
treat these as the baseline and compare against it rather than against green:

| Failure signature | Cause |
|---|---|
| `VACUUM INTO ... SQL logic error: out of memory (1)` | backup/migrate tests against Windows paths |
| `mode ... = 0777, want 0700` | Unix permission-bit assertions (`v2/`, blobs, control token) |
| `sync <dir>: Access is denied` | directory fsync on publish/rollback |
| `executable file not found in %PATH%` | binary tests build `openmessage` without `.exe` |
| `%1 is not a valid Win32 application` | tests exec a `.sh` cookie-refresh script |
| static asset / link preview / media resolve failures | path separator and mode assumptions |

Packages that **do** pass on Windows and should stay passing: `internal/app`, `db`,
`river`, `vault`, `tui`, `localapi`, `bridge`, `bridgeadapters/*`, `importer`, `story`,
`viz`, `v2read`, `v2keys`, `notify`, `telemetry`, `googlecookies`.

CI runs on `ubuntu-latest`, so the real gate is green there. `main` is
branch-protected: PRs required, `Go Test` and `Go Race` must pass and be
up to date before merging.

### CI (`.github/workflows/`)

| Workflow | What |
|---|---|
| `test.yml` | `go test` + **40% coverage floor**, race suite, lightweight `go build`/`go test` on `macos-latest` |
| `release.yml` | Tagged/manual cross-platform CLI artifacts + checksums |
| `gmessages-fork-drift.yml` | Weekly: pinned gmessages fork stays exactly one carried patch over recorded base |

### Regression tests that encode hard rules

- MCP stdio starts **zero** transport supervisors
- `app.NewClient` performs **no** store repair writes
- Built binary MCP client shape starts no transports

Do not weaken these when "simplifying" serve.

## Gotchas (read before support work)

1. **WAL lock** — direct `sqlite3` → error 14 or missing recent rows. Use `/api/*`.
2. **MCP + transports = fratricide** — WhatsApp logout / Signal deauth within seconds.
3. **Pin MCP `OPENMESSAGES_DATA_DIR`** (and `OPENMESSAGES_V2_PRIMARY=1` post-cutover) in `~/.mcp.json`.
4. **Google QR is dead** for many accounts — cookie / Google Account pairing.
5. **Clear `session.json`** to force unpaired, or a stale one causes an immediate post-pair 401 (see agent-runbook.md).
6. **Don't thrash Google reconnect/pair** — account throttling.
7. **`instance.lock` is advisory** for backup/migrate only; daemon does not yet honor it.
8. **`go.work` overrides** can make dependency bumps look ignored.
9. **Keep PATH binary = daemon binary** — schema migrations from a newer CLI against an older running daemon are hostile.
10. **Non-goals** (this fork): native GUI/tray, iMessage live sync (import-only), inline media previews, signed installers — see [../tui.md](../tui.md).
11. **lipgloss `Height` vs `MaxHeight`:** `Height` is content-box (borders add outside). `MaxHeight` caps the final rendered block **including** borders. Setting both to the same value clips the bottom border and two content rows — the source of the first-contact preview ghost on Windows Terminal. Cap with `mainH + borderY`.

## MCP tools (24)

Registered in `internal/tools/tools.go` → `RegisterWithOptions`:

`get_messages`, `get_conversation`, `search_messages`, `send_message`, `send_to_conversation`, `send_media_to_conversation`, `react_to_message`, `set_message_transcript`, `list_conversations`, `list_contacts`, `resolve_contact_routes`, `get_status`, `draft_message`, `download_media`, `import_messages`, `get_person_messages`, `conversation_stats`, `generate_story`, `person_stats`, `generate_person_story`, `generate_viz`, `get_person_messages_range`, `render_story`, `send_group_message`

Person/story/viz tools return unavailable while V2 is the serving store. Message-content results prepend an untrusted-content warning.
