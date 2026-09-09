# Contributing to om-tui

om-tui is a TUI-only fork of [MaxGhenis/openmessage](https://github.com/MaxGhenis/openmessage).
File bugs and feature requests **here**, not on upstream, unless you have
confirmed the bug still exists in OpenMessage's macOS app or web UI.

Security reports: [SECURITY.md](SECURITY.md) (private advisory, not a public issue).

## Pull requests

`main` is branch-protected. Open a PR; `Go Test` and `Go Race` must pass.

```bash
go build ./...
go test ./...
go test -race ./...
```

CI is Ubuntu. Many `go test ./...` failures on Windows are POSIX assumptions
in tests, not product bugs — see
[docs/runbook/03_RULES_AND_STANDARDS.md](docs/runbook/03_RULES_AND_STANDARDS.md).

If you change behavior or setup, update `README.md`, `docs/`, and `CLAUDE.md`
in the same PR. The federated runbook in `docs/runbook/` is ground truth when
docs disagree.

## Scope

This fork does not ship a native GUI, a bundled web UI, or a marketing site.
Patches that reintroduce those belong upstream.
