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

The MCP server exposes these tools (prefix: `mcp__openmessage__`):

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

The om-tui daemon must be running (paired and reachable). If the MCP
connection fails, the user needs to run `om-tui pair` and then either
`om-tui tui` (spawns the daemon automatically) or `om-tui serve --api`.
