# om-tui Release Checklist

Use this before tagging a release build.

## 1. Worktree And Privacy Preflight

- Run `git status --short` and confirm every changed file is intentional.
- Confirm private recovery files, chat exports, screenshots, and local databases are not tracked. In particular, `.tmp-openmessage-recover-filtered.sql` must stay untracked and ignored locally.
- Check any new screenshots/demo assets for real names, private chats, phone numbers, or account data.

## 2. Automated Checks

- Run `go build ./...`.
- Run `go test ./...`.
- Run `go test -race ./...`.
- Confirm CI is green on the release commit before tagging.

## 3. Messaging Dogfood Matrix

- Google Messages: pairing, reconnect, SMS send/receive, RCS attribution, image receive, notifications, and read receipts.
- WhatsApp: pairing, reconnect, text send, image plus caption send, reaction send/receive, group leave, group names, and avatar loading.
- Signal: pairing, history/backfill, group names, image receive, reactions, and stale connection recovery.
- Slack rivers: pairing, channel/DM sync, unread counts, reply threads, text send, and (if configured) Socket Mode realtime.
- Long threads: opening, sending, receiving, older-message scrollback, and media hydration in the TUI.

## 4. Diagnostics

- Confirm `GET /api/status` reports each platform/river's connection state correctly after a clean launch.
- Attach relevant `serve` log output to issues when investigating crashes, backend exits, dropped connections, stale media, or missing notifications.
- Do not paste message bodies, contact exports, or database dumps into public issues.

## 5. Release Gate

- No open P1/P2 review findings or known privacy leaks.
- No failing required CI checks.
- Release artifacts (`om-tui-<os>-<arch>` tarballs/zips) are generated from the intended tagged commit.
- `SHA256SUMS` published alongside the release artifacts.
