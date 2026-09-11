package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/readsource"
)

func getPersonMessagesTool() mcp.Tool {
	return mcp.NewTool("get_person_messages",
		mcp.WithDescription("Get all messages with a person across all platforms (SMS, Google Chat, iMessage, WhatsApp). Searches by name or identifier."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Person's name to search for (case-insensitive partial match)")),
		mcp.WithNumber("limit", mcp.Description("Maximum messages to return (default 50)")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

func getPersonMessagesHandler(a *app.App, configured ...Options) server.ToolHandlerFunc {
	options := resolvedOptions(a, configured)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		name := strArg(args, "name")
		if name == "" {
			return errorResult("name is required"), nil
		}
		limit := intArg(args, "limit", 50)

		matches, err := findPersonConversations(options.Reads, name, true)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		if len(matches) == 0 {
			return textResult(fmt.Sprintf("No conversations found with '%s'.", name)), nil
		}

		convMap := make(map[string]*db.Conversation, len(matches))
		ids := make([]string, 0, len(matches))
		for _, c := range matches {
			ids = append(ids, c.ConversationID)
			convMap[c.ConversationID] = c
		}

		msgs, err := options.Reads.GetMessagesByConversations(ids, limit)
		if err != nil {
			return errorResult(fmt.Sprintf("get messages: %v", err)), nil
		}

		var sb strings.Builder
		sb.WriteString(messagePreamble)
		fmt.Fprintf(&sb, "Messages with '%s' across %d conversation(s):\n\n", name, len(ids))

		currentConv := ""
		totalMsgs := 0
		for _, m := range msgs {
			if m.ConversationID != currentConv {
				if currentConv != "" {
					sb.WriteString("\n")
				}
				currentConv = m.ConversationID
				if info, ok := convMap[currentConv]; ok {
					platform := info.SourcePlatform
					if platform == "" {
						platform = "sms"
					}
					fmt.Fprintf(&sb, "--- %s [%s] (ID: %s) ---\n", info.Name, platform, currentConv)
				}
			}

			sb.WriteString(formatMessageLine(m))
			sb.WriteByte('\n')
			totalMsgs++
		}

		fmt.Fprintf(&sb, "\nTotal: %d messages across %d conversation(s)\n", totalMsgs, len(ids))
		return textResult(sb.String()), nil
	}
}

func findPersonConversations(reads readsource.ReadSource, name string, includeGroups bool) ([]*db.Conversation, error) {
	allConvs, err := reads.ListConversations(1000)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %v", err)
	}

	nameLower := strings.ToLower(name)
	var matching []*db.Conversation
	for _, c := range allConvs {
		if !includeGroups && c.IsGroup {
			continue
		}
		if strings.Contains(strings.ToLower(c.Name), nameLower) ||
			strings.Contains(strings.ToLower(c.Participants), nameLower) {
			matching = append(matching, c)
		}
	}
	return matching, nil
}
