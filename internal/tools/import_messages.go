package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/importer"
	"github.com/maxghenis/openmessage/internal/migration"
)

var (
	importGChatDirectory = importer.ImportGChatDirectory
	importSignalDesktop  = func(store *db.Store, supportDir, name, address string) (*importer.ImportResult, error) {
		imp := &importer.SignalDesktop{
			SupportDir: supportDir,
			MyName:     name,
			MyAddress:  address,
		}
		return imp.ImportFromDB(store)
	}
)

func importMessagesTool() mcp.Tool {
	return mcp.NewTool("import_messages",
		mcp.WithDescription("Import messages from external platforms such as Google Chat Takeout, iMessage, WhatsApp exports, and Signal Desktop"),
		mcp.WithString("source", mcp.Required(), mcp.Description("Source platform: gchat, gchat_conversation, imessage, whatsapp, signal")),
		mcp.WithString("path", mcp.Description("Path to import data (directory for gchat, Signal support dir for signal, file for gchat_conversation/whatsapp, optional for imessage)")),
		mcp.WithString("email", mcp.Description("Your email for gchat imports (marks your messages as is_from_me)")),
		mcp.WithString("name", mcp.Description("Your display name for whatsapp, imessage, or signal imports")),
		mcp.WithString("address", mcp.Description("Your Signal account identifier (usually phone number with country code) for signal imports")),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

func importMessagesHandler(a *app.App, configured ...Options) server.ToolHandlerFunc {
	options := resolvedOptions(a, configured)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		source := strArg(args, "source")
		path := strArg(args, "path")
		email := strArg(args, "email")
		name := strArg(args, "name")
		address := strArg(args, "address")

		if options.V2Primary && v2WriteStore(options) == nil {
			return errorResult("import_messages: v2 store is unavailable"), nil
		}

		var result *importer.ImportResult
		var err error
		platform := ""

		switch source {
		case "gchat":
			if path == "" {
				return errorResult("path is required for gchat import (Google Chat Takeout Groups directory)"), nil
			}
			platform = "gchat"
			result, err = importGChatDirectory(a.Store, path, email)

		case "gchat_conversation":
			if path == "" {
				return errorResult("path is required (path to messages.json)"), nil
			}
			f, ferr := os.Open(path)
			if ferr != nil {
				return errorResult(fmt.Sprintf("open file: %v", ferr)), nil
			}
			defer f.Close()
			platform = "gchat"
			imp := &importer.GChat{MyEmail: email}
			result, err = imp.Import(a.Store, f)

		case "imessage":
			platform = "imessage"
			imp := &importer.IMessage{DBPath: path, MyName: name}
			result, err = imp.ImportFromDB(a.Store)

		case "whatsapp":
			if path == "" {
				return errorResult("path is required (path to WhatsApp chat.txt export)"), nil
			}
			f, ferr := os.Open(path)
			if ferr != nil {
				return errorResult(fmt.Sprintf("open file: %v", ferr)), nil
			}
			defer f.Close()
			platform = "whatsapp"
			imp := &importer.WhatsApp{MyName: name}
			result, err = imp.Import(a.Store, f)

		case "signal":
			platform = "signal"
			result, err = importSignalDesktop(a.Store, path, name, address)

		default:
			return errorResult(fmt.Sprintf("unknown source: %s (supported: gchat, gchat_conversation, imessage, whatsapp, signal)", source)), nil
		}

		if err != nil {
			return errorResult(fmt.Sprintf("import failed: %v", err)), nil
		}
		if store := v2WriteStore(options); store != nil && platform != "" {
			sourcePath := filepath.Join(a.DataDir, "messages.db")
			if err := migration.SyncInto(ctx, sourcePath, store, platform); err != nil {
				return errorResult(fmt.Sprintf("import wrote v1 but v2 sync failed: %v", err)), nil
			}
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Import complete:\n")
		fmt.Fprintf(&sb, "  Conversations created: %d\n", result.ConversationsCreated)
		fmt.Fprintf(&sb, "  Messages imported: %d\n", result.MessagesImported)
		fmt.Fprintf(&sb, "  Messages duplicate: %d\n", result.MessagesDuplicate)
		if len(result.Errors) > 0 {
			fmt.Fprintf(&sb, "  Errors: %d\n", len(result.Errors))
			for _, e := range result.Errors {
				fmt.Fprintf(&sb, "    - %s\n", e)
			}
		}
		return textResult(sb.String()), nil
	}
}
