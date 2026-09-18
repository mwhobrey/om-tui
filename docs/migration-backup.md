# Migration backup (Wave 4, R1)

The first cutover step is an offline, verified backup of the legacy data:

```sh
openmessage backup
```

The command uses the same data-directory resolution as the app:
`OPENMESSAGES_DATA_DIR` when set, otherwise
`~/.local/share/openmessage`. By default it writes
`<data-dir>/migration-backups/<UTC-stamp>/`; use `--to <directory>` to choose
an unused destination. `--json` emits one JSON object for automation.

Stop the OpenMessage backend before running the command. R1 checks the current
daemon endpoint at `http://127.0.0.1:7007/api/status` and holds an exclusive,
non-blocking advisory lock on `<data-dir>/instance.lock` for the full backup.
The lock file's JSON body is diagnostic and can remain after a crash; the OS
lock on the open file descriptor, not that body, is authoritative. Store-owning
`serve` (web / api / mcp-sse / `--mcp-stdio --transports`) takes the same lock
before `app.New`, so a running daemon is refused even when it was started on
another port or without HTTP. MCP-stdio clients and `--demo` do not hold it.
The HTTP health probe remains a belt-and-suspenders check for a listener on
the configured `OPENMESSAGES_HOST`/`OPENMESSAGES_PORT`.

The command requires free space equal to at least twice the legacy
`messages.db` size. It creates `messages.db` with SQLite `VACUUM INTO`, runs
`PRAGMA quick_check` on that copy, and copies any present `session.json`,
`signal-cli/`, `whatsapp-session.db`, and `openmessage.db` state. All copied
files are recorded with source path, byte size, mode, and SHA-256.

`manifest.json` is atomically written last. Its presence marks a complete,
verified backup; a timestamped directory without it is incomplete and must not
be used as cutover evidence.

Stable exit codes are:

- `0`: backup complete
- `2`: refused because a backend may be running or the instance lock is held
- `3`: path/free-space preflight failed
- `4`: backup or copy failed
- `5`: copied database verification failed

The command never transforms or deletes the source database. Migration is a
separate later step.

## Rollback fossil after PRIMARY cutover

`om-tui migrate` publishes `v2/store.sqlite3` and leaves `messages.db` in place.
PRIMARY is the compiled default; set `OPENMESSAGES_V2_PRIMARY=0` only to serve
that frozen v1 file. The legacy projector is off on PRIMARY, so post-cutover
inbound and outbound rows live only in v2. Unsetting the flag does **not**
replay them into `messages.db`.

If serving is wrong and you have not sent or received anything you care about,
unset PRIMARY (or set `=0`) and start the old binary on frozen v1. If you
already used PRIMARY in anger, stay on v2 or restore a `migration-backups/`
directory — do not expect v1 to contain the new rows. Do not delete
`messages.db` until you explicitly decide the fossil is waste.

On Windows, `backup` uses SQLite `VACUUM INTO` and can OOM. If that fails,
stop every store-owning process and file-copy `messages.db`, `messages.db-wal`,
`messages.db-shm`, `session.json`, and river sessions into
`<data-dir>/migration-backups/<stamp>/` instead of skipping backup.

A 4KiB `v2/store.sqlite3` is a shadow-ingest stub, not a migrated inbox.
Rename `v2/` aside before `migrate`; otherwise the command refuses to overwrite
canonical v2 state.

Windows `file://C:/...` SQLite DSNs (drive letter parsed as a host) fail at
open with `SQL logic error: out of memory (1)`. Migrate's read-only snapshot
DSN uses `file:///C:/...`. Finalize `Sync` opens the staged store read-write;
a read-only handle returns `Access is denied` on Windows `FlushFileBuffers`.
Embedded SQL migration checksums hash LF-normalized bytes so a CRLF checkout
does not fail the schema-11 integrity gate. Directory `Sync` after rename also
returns `Access is denied` on Windows; publish treats that as success.
