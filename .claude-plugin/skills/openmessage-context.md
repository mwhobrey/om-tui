---
description: Context for om-tui MCP integration
---

# om-tui

om-tui is a cross-platform terminal messaging client (fork of MaxGhenis/openmessage)
connecting Google Messages (libgm), WhatsApp, Signal, and Slack to one local
inbox and MCP server. There is no macOS app or web UI on this fork — the TUI
is the only client.

## Architecture

- **Go daemon** (`om-tui serve`): owns the live platform connections, serves
  the local REST/SSE API, and exposes MCP over stdio, Streamable HTTP, or SSE
- **TUI** (`om-tui tui`): the Bubble Tea terminal client; spawns
  `serve --api --no-web` if no daemon is already running
- **MCP server**: stdio by default (`serve --mcp-stdio`); SSE/Streamable HTTP
  at `http://localhost:7007/mcp/sse` and `/mcp` when started with `--mcp-sse`
  — provides tools for listing conversations, reading/sending messages, searching

## MCP tools

The MCP server exposes these tools (prefix depends on the server key in the
user's `~/.mcp.json`; this repo's README examples use `om-tui`, i.e.
`mcp__om-tui__<tool>`):

| Tool | Description | Key params |
|------|-------------|------------|
| `list_conversations` | Recent conversations with unread counts | `limit` |
| `get_conversation` | Single conversation details | `conversation_id` |
| `get_messages` | Messages in a conversation | `conversation_id`, `limit` |
| `search_messages` | Full-text search across all messages | `query`, `limit` |
| `send_message` | Send SMS/RCS to a phone number | `phone_number`, `message` |
| `list_contacts` | Known contacts from message history | — |
| `get_status` | Connection status to Google Messages | — |

## Prerequisites

The account must be **paired** (`om-tui pair`) at least once. Reads/search
work directly against the local store even with no daemon running. Sends
and reactions need a **running** daemon (`om-tui serve --api` or `om-tui
tui`, which spawns one) — if those fail with a "start the daemon" error,
that's what's missing.
