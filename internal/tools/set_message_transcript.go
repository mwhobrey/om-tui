package tools

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

func setMessageTranscriptTool() mcp.Tool {
	return mcp.NewTool("set_message_transcript",
		mcp.WithDescription(
			"Save a transcript for an existing message. The original body and media metadata are preserved, and calling again overwrites the prior transcript.",
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The message_id of the message to annotate with transcript text."),
		),
		mcp.WithString("transcript",
			mcp.Required(),
			mcp.Description("The transcribed text. Empty string clears any existing transcript."),
		),
		mcp.WithString("model",
			mcp.Description("Free-form model identifier (for example faster-whisper:base.en)."),
		),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
	)
}

func setMessageTranscriptHandler(a *app.App, configured ...Options) server.ToolHandlerFunc {
	options := resolvedOptions(a, configured)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		messageID := strArg(args, "message_id")
		rawTranscript, ok := args["transcript"]
		if !ok {
			return errorResult("set_message_transcript: transcript is required"), nil
		}
		transcript, ok := rawTranscript.(string)
		if !ok {
			return errorResult("set_message_transcript: transcript must be a string"), nil
		}
		var model *string
		if _, ok := args["model"]; ok {
			modelValue, ok := args["model"].(string)
			if !ok {
				return errorResult("set_message_transcript: model must be a string"), nil
			}
			model = &modelValue
		}
		if messageID == "" {
			return errorResult("set_message_transcript: message_id is required"), nil
		}
		if err := db.ValidateMessageTranscript(transcript, model); err != nil {
			return errorResult(fmt.Sprintf("set_message_transcript: %v", err)), nil
		}
		var msg *db.Message
		if options.V2Primary {
			store := v2WriteStore(options)
			if store == nil {
				return errorResult("set_message_transcript: v2 store is unavailable"), nil
			}
			if err := store.SetMessageTranscript(ctx, messageID, transcript, model); err != nil {
				if errors.Is(err, sqlite.ErrNotFound) {
					return errorResult("set_message_transcript: message not found"), nil
				}
				return errorResult(fmt.Sprintf("set_message_transcript: %v", err)), nil
			}
			reloaded, err := options.Reads.GetMessageByID(messageID)
			if err != nil {
				return errorResult(fmt.Sprintf("set_message_transcript: reload message: %v", err)), nil
			}
			msg = reloaded
		} else {
			if err := a.Store.SetMessageTranscript(messageID, transcript, model); err != nil {
				return errorResult(fmt.Sprintf("set_message_transcript: %v", err)), nil
			}
			reloaded, err := a.Store.GetMessageByID(messageID)
			if err != nil {
				return errorResult(fmt.Sprintf("set_message_transcript: reload message: %v", err)), nil
			}
			msg = reloaded
		}
		if msg != nil && a.OnMessagesChange != nil {
			a.OnMessagesChange(msg.ConversationID)
		}
		storedModel := ""
		if msg != nil {
			storedModel = msg.TranscriptModel
		}
		return textResult(fmt.Sprintf(
			"Transcript saved for message %s (%d chars, model=%q).",
			messageID, utf8.RuneCountInString(transcript), storedModel,
		)), nil
	}
}
