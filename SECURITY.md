# Security policy

om-tui is a local-first messaging client. A bug that leaks session cookies,
river tokens, or message bodies is a security bug even when it only affects
the machine that ran the binary.

## Reporting a vulnerability

**Do not file a public GitHub issue** for an unreleased vulnerability.

Use GitHub's private advisory form:

https://github.com/mwhobrey/om-tui/security/advisories/new

Include OS, om-tui version (or commit), and enough to reproduce. Do not
paste live session cookies, vault contents, message bodies, or contact
lists. Redact those and describe them.

## In scope

- Vault sealing (`rivers/*/credentials.enc`, DPAPI / Keychain / Secret Service)
- `session.json` and other pairing/session files in the data dir
- Local HTTP API and MCP endpoints (loopback auth, bind address, token handling)
- Path traversal or write-outside-export-dir in viz/story/media save
- Anything that lets another local user or a remote party read or send as you

## Out of scope / report elsewhere

Known vulnerabilities in libraries we consume belong with those projects, not
here, unless om-tui uses them in a way that makes a "won't fix" upstream CVE
exploitable in this client:

- [mautrix/gmessages](https://github.com/mautrix/gmessages) (libgm)
- [tulir/whatsmeow](https://github.com/tulir/whatsmeow)
- [signal-cli](https://github.com/AsamK/signal-cli)
- [slack-go/slack](https://github.com/slack-go/slack)
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- Go stdlib / toolchain: [Go security policy](https://go.dev/security)

Binding `OPENMESSAGES_HOST` off localhost is an operator choice. Do not
report "the API is reachable on my LAN" unless auth is bypassed on the
default loopback bind.

## What happens next

I will acknowledge privately and patch or document the limitation. Credit
is yours unless you ask otherwise.
