package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

func draftMessageTool() mcp.Tool {
	return mcp.NewTool("draft_message",
		mcp.WithDescription("Create a draft message for a conversation. The user can review and send it from the app."),
		mcp.WithString("conversation_id", mcp.Required(), mcp.Description("The conversation ID to create a draft for")),
		mcp.WithString("message", mcp.Required(), mcp.Description("The draft message text")),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
	)
}

func draftMessageHandler(a *app.App, configured ...Options) server.ToolHandlerFunc {
	options := resolvedOptions(a, configured)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		conversationID := strArg(args, "conversation_id")
		message := strArg(args, "message")

		if conversationID == "" {
			return errorResult("conversation_id is required"), nil
		}
		if message == "" {
			return errorResult("message is required"), nil
		}

		now := time.Now()
		if options.V2Primary {
			if options.V2 == nil || options.V2.V2Store == nil {
				return errorResult("draft_message: v2 store is unavailable"), nil
			}
			draft, err := options.V2.V2Store.UpsertDraft(ctx, sqlite.Draft{
				ConversationID: conversationID,
				Body:           message,
			})
			if err != nil {
				return errorResult(fmt.Sprintf("failed to create draft: %v", err)), nil
			}
			return textResult(fmt.Sprintf("Draft %s created. The user can review and send it from the app.", draft.DraftID)), nil
		}
		draftID := fmt.Sprintf("draft_%d", now.UnixMilli())

		err := a.Store.UpsertDraft(&db.Draft{
			DraftID:        draftID,
			ConversationID: conversationID,
			Body:           message,
			CreatedAt:      now.UnixMilli(),
		})
		if err != nil {
			return errorResult(fmt.Sprintf("failed to create draft: %v", err)), nil
		}

		return textResult("Draft created. The user can review and send it from the app."), nil
	}
}
