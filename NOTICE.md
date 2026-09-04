# Notice and attribution

om-tui is released into the public domain under [The Unlicense](LICENSE),
same as every project it's built on below. This file exists so credit is
explicit and durable, not just implied by an import path.

## Fork origin

**[MaxGhenis/openmessage](https://github.com/MaxGhenis/openmessage)** —
om-tui started as a fork of OpenMessage, a local-first messaging workspace
with a native macOS app, a localhost web UI, and Google Messages/WhatsApp/
Signal support. om-tui carries forward the Go core (protocol clients,
storage, MCP server, importers) and narrows the product surface to a
cross-platform terminal UI, dropping the macOS app and web UI that upstream
maintains itself. The Go module path (`github.com/maxghenis/openmessage`)
is left unchanged on purpose, so upstream diffs stay easy to compare and
port.

## Libraries and services this is built on

- **[mautrix/gmessages](https://github.com/mautrix/gmessages)** (libgm) —
  the Google Messages pairing, encryption, and long-polling protocol client.
- **[tulir/whatsmeow](https://github.com/tulir/whatsmeow)** — live WhatsApp
  pairing, sync, send, and receipt handling as a companion-device bridge.
- **[signal-cli](https://github.com/AsamK/signal-cli)** — the local Signal
  linked-device bridge this shells out to for message sync, media, and
  reactions.
- **[slack-go/slack](https://github.com/slack-go/slack)** — the Slack Web
  API client powering Slack rivers (recent-history sync, text send, and
  optional Socket Mode realtime).
- **[mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)** — the MCP
  server implementation exposing om-tui's tools over stdio, Streamable
  HTTP, and SSE.
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** and the rest
  of the Charm ecosystem — the terminal UI framework the TUI is built on.
- **SQLite**, via a pure-Go driver — local storage for messages,
  conversations, contacts, and rivers.

If you maintain one of these projects and want the wording here changed,
open an issue or PR.
