# Rules and standards

## Coding conventions

- Go is the source of truth for product behavior. Match existing package style: small focused packages under `internal/`, CLI thin wrappers in `cmd/`.
- Prefer surgical diffs. Do not rewrite working legacy paths "for cleanliness" while V2 cutover is staged.
- **v1 (`messages.db`) is frozen except critical bug fixes.** New Slack/archive work goes through V2 ingest (codec → decoder → inbox). Do not grow the legacy Slack schema, add Slack features on `messages.db`, or teach `web`/`tools` about raw Slack SDK types.
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
5. **V2 primary reads through `v2read`**, including contacts/people/stats/story. Do not "fix" empty PRIMARY surfaces by reading frozen `messages.db` after cutover. Transcripts and imports write v2 (`message_extras` / `SyncInto`). Favorite/mute persist on the v2 conversation row. Drafts, tabs, contact CRM metadata, `/api/new-conversation` (including SMS groups), PRIMARY `send_message` / `send_group_message` minting, and Google contact **sync** (v2 identities, provenance `address_book`) write v2. Remaining fail-closed write is scheduled-send — do not persist that into v1.
6. **Windows vault is DPAPI-bound** to user/machine — backups of `credentials.enc` are useless on another box without re-pair.

## Environment variables (high-signal)

| Variable | Role |
|---|---|
| `OPENMESSAGES_DATA_DIR` | Store root (override defaults) |
| `OPENMESSAGES_LOG_LEVEL` | Zerolog level |
| `OPENMESSAGES_HOST` / `OPENMESSAGES_PORT` | Loopback bind |
| `OPENMESSAGES_DEMO` | Isolated fake-data store; no live transports |
| `OPENMESSAGES_V2_SEND` / `_INGEST` / `_PRIMARY` | Staged V2 enablement. PRIMARY defaults **on**; set `OPENMESSAGES_V2_PRIMARY=0` to serve v1. PRIMARY implies send+ingest unless those are explicitly `0` (rejected). |
| `OPENMESSAGES_SIGNAL_CLI` | signal-cli binary path |
| `OPENMESSAGES_JAVA_HOME` | JDK 25+ for signal-cli (else auto-discovered) |
| `OPENMESSAGE_COOKIE_REFRESH_SCRIPT` / `OPENMESSAGE_CHROME_PROFILE` | Google self-heal |
| `OPENMESSAGE_REPAIR_MIN_INTERVAL` | Pace Google repair |
| `OPENMESSAGES_EXPORT_DIR` / `OPENMESSAGES_ALLOW_ANY_EXPORT_PATH` | Viz/export path policy |
| `OPENMESSAGES_VAULT_INSECURE` | Non-Windows test-only vault |
| `OPENMESSAGES_WINDOWS_NOTIFICATIONS` / `_TOAST_APP_ID` | Windows toasts |
| `OPENMESSAGE_TELEMETRY` | Opt-in heartbeat |
| `OPENMESSAGES_KLIPY_API_KEY` / `KLIPY_API_KEY` | GIF search |
| `OPENMESSAGES_TUI_GRAPHICS` | Pair-overlay QR: `sixel`, `kitty`, or `cells` (auto: WT sixel, Kitty/Ghostty/WezTerm kitty, else cells) |

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
| `%1 is not a valid Win32 application` | tests exec a `.sh` cookie-refresh script |
| static asset / link preview / media resolve failures | path separator and mode assumptions |

Packages that **do** pass on Windows and should stay passing: `cmd` binary tests,
`internal/app`, `db`, `river`, `vault`, `tui`, `localapi`, `bridge`,
`bridgeadapters/*`, `importer`, `story`, `viz`, `v2read`, `v2keys`, `notify`,
`telemetry`, `googlecookies`.

CI runs on `ubuntu-latest`, so the real gate is green there. `main` is
branch-protected: PRs required, `Go Test` and `Go Race` must pass and be
up to date before merging.

### CI (`.github/workflows/`)

| Workflow | What |
|---|---|
| `test.yml` | `go test` + **40% coverage floor**, race suite, lightweight `go build`/`go test` on `macos-latest`, `govulncheck ./...` (not a required merge check) |
| `release.yml` | Tagged/manual cross-platform CLI artifacts + checksums |
| `gmessages-fork-drift.yml` | Weekly: pinned gmessages fork stays exactly one carried patch over recorded base |

Repo security surface (GitHub settings + files, not required CI):

- Dependabot: weekly grouped `gomod` + `github-actions` PRs ([`.github/dependabot.yml`](../../.github/dependabot.yml)); alerts and security-update PRs enabled
- `SECURITY.md` + private vulnerability reporting (do not file public issues for unreleased vulns)
- CodeQL default setup on `main` / PRs (GitHub SAST; not a required merge check)
- Secret scanning + push protection enabled
- `govulncheck` is not a required merge check. Stdlib findings track the patch Go in `go.mod` (1.26.6+ as of this bump). This box's mise pin is 1.26.5; GOTOOLCHAIN will fetch 1.26.6 when needed.

### Regression tests that encode hard rules

- MCP stdio starts **zero** transport supervisors
- `app.NewClient` performs **no** store repair writes
- Built binary MCP client shape starts no transports

Do not weaken these when "simplifying" serve.

## Gotchas (read before support work)

1. **WAL lock** — direct `sqlite3` → error 14 or missing recent rows. Use `/api/*`.
2. **MCP + transports = fratricide** — WhatsApp logout / Signal deauth within seconds.
3. **Pin MCP `OPENMESSAGES_DATA_DIR`** in `~/.mcp.json`. PRIMARY is the compiled default; set `OPENMESSAGES_V2_PRIMARY=0` only to keep MCP on frozen v1. After cutover, a missing `v2/store.sqlite3` next to a non-empty `messages.db` means you still need `om-tui migrate`.
4. **Google QR is dead** for many accounts — cookie / Google Account pairing.
5. **Clear `session.json` and `session.json.bak`** to force unpaired, or a stale one causes an immediate post-pair 401 (see agent-runbook.md).
6. **Don't thrash Google reconnect/pair** — account throttling.
7. **`instance.lock` is exclusive** for store-owning `serve`, `backup`, `migrate`, and `repair --apply`. MCP-stdio clients and `--demo` do not take it (N sessions; demo uses a throwaway dir). Stale JSON in the file is diagnostic — the OS lock on the FD is authoritative.
8. **`go.work` overrides** can make dependency bumps look ignored.
9. **Keep PATH binary = daemon binary** — schema migrations from a newer CLI against an older running daemon are hostile.
10. **Non-goals** (this fork): native GUI/tray, iMessage live sync (import-only), inline media previews, signed installers — see [../tui.md](../tui.md).
11. **lipgloss `Height` vs `MaxHeight`:** `Height` is content-box (borders add outside). `MaxHeight` caps the final rendered block **including** borders. Setting both to the same value clips the bottom border and two content rows — the source of the first-contact preview ghost on Windows Terminal. Cap with `mainH + borderY`.
12. **TUI Google pairing is paste-only for now.** Current Chrome on Windows stores Gaia cookies as v20 (app-bound). Native DPAPI cannot unwrap that, CDP against a temp copy returns none, and Chrome's elevation COM path-validates callers — silent v20 unwrap from om-tui.exe is not viable. Overlay: `messages.google.com` → F12 → Network → Copy as cURL → `ctrl+v`. If `session.json` already has paired auth, that paste rewrites cookies and reconnects (no phone tap). Gaia + emoji only when unpaired or reconnect fails. `POST /api/google/pair` is the same entry. Silent cookie refresh (`googlecookies.Refresh`) still tries native decrypt for v10. Never launch Chrome with remote debugging against the live User Data dir. Kill and restart `om-tui` only when the overlay shows `failed` or has sat more than five minutes (`googlePairPhoneTimeout`); a shorter wait is still a live phone confirmation.

## MCP tools (24)

Registered in `internal/tools/tools.go` → `RegisterWithOptions`:

`get_messages`, `get_conversation`, `search_messages`, `send_message`, `send_to_conversation`, `send_media_to_conversation`, `react_to_message`, `set_message_transcript`, `list_conversations`, `list_contacts`, `resolve_contact_routes`, `get_status`, `draft_message`, `download_media`, `import_messages`, `get_person_messages`, `conversation_stats`, `generate_story`, `person_stats`, `generate_person_story`, `generate_viz`, `get_person_messages_range`, `render_story`, `send_group_message`

Canonical read tools (`get_messages`, `get_conversation`, `search_messages`, `list_conversations`, `list_contacts`, `resolve_contact_routes`, `download_media`, person/story/viz) read through `v2read` when V2 is the serving store. `set_message_transcript` writes v2 `message_extras` when primary. `import_messages` still fills v1 then `SyncInto`s that platform into the opened v2 store. `send_media_to_conversation` submits native v2 IDs; `send_message` / `send_group_message` mint or reuse a v2 thread (MCP stdio goes through the daemon's `/api/new-conversation` + outbox). `list_contacts` merges conversation participants with Google address-book identities. `draft_message` writes the v2 `drafts` table when primary. Message-content results prepend an untrusted-content warning.
