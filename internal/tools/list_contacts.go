package tools

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/readsource"
)

func listContactsTool() mcp.Tool {
	return mcp.NewTool("list_contacts",
		mcp.WithDescription("List or search contacts by name or phone number"),
		mcp.WithString("query", mcp.Description("Search by name or number")),
		mcp.WithNumber("limit", mcp.Description("Maximum contacts to return (default 50)")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

func listContactsHandler(a *app.App, configured ...Options) server.ToolHandlerFunc {
	options := resolvedOptions(a, configured)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		query := strArg(args, "query")
		limit := intArg(args, "limit", 50)

		var contacts []*db.Contact
		var err error
		if options.V2Primary {
			if v2 := v2WriteStore(options); v2 != nil {
				book, bookErr := v2.ListAddressBookContacts("", 1)
				if bookErr == nil && len(book) == 0 {
					if options.Daemon != nil {
						if _, syncErr := options.Daemon.SyncGoogleContacts(ctx); syncErr != nil {
							a.Logger.Debug().Err(syncErr).Msg("Google contact sync skipped")
						}
					} else if a.GetClient() != nil {
						if _, syncErr := a.SyncGoogleContacts(); syncErr != nil {
							a.Logger.Warn().Err(syncErr).Msg("Failed to fetch contacts from phone")
						}
					}
				}
			}
			contacts, err = primaryContacts(options, query, limit)
			if err != nil {
				return errorResult(fmt.Sprintf("query failed: %v", err)), nil
			}
		} else {
			// If no contacts in DB yet, try fetching from phone
			contacts, err = a.Store.ListContacts("", 1)
			if err == nil && len(contacts) == 0 && a.GetClient() != nil {
				if err := fetchAndCacheContacts(a); err != nil {
					a.Logger.Warn().Err(err).Msg("Failed to fetch contacts from phone")
				}
			}

			contacts, err = a.Store.ListContacts(query, limit)
			if err != nil {
				return errorResult(fmt.Sprintf("query failed: %v", err)), nil
			}

			// Fall back to conversation participants if contacts table is empty
			if len(contacts) == 0 {
				contacts, err = a.Store.ListContactsFromConversations(query, limit)
				if err != nil {
					return errorResult(fmt.Sprintf("query failed: %v", err)), nil
				}
			}
		}

		if len(contacts) == 0 {
			return structuredResult(map[string]any{
				"count":    0,
				"query":    query,
				"contacts": []contactSummary{},
			}, "No contacts found."), nil
		}

		var sb strings.Builder
		summaries := make([]contactSummary, 0, len(contacts))
		fmt.Fprintf(&sb, "%d contacts:\n\n", len(contacts))
		for _, c := range contacts {
			summaries = append(summaries, summarizeContact(c))
			if c.Number != "" {
				fmt.Fprintf(&sb, "- %s: %s\n", c.Name, c.Number)
			} else {
				fmt.Fprintf(&sb, "- %s\n", c.Name)
			}
		}
		return structuredResult(map[string]any{
			"count":    len(summaries),
			"query":    query,
			"contacts": summaries,
		}, sb.String()), nil
	}
}

func fetchAndCacheContacts(a *app.App) error {
	cli := a.GetClient()
	if cli == nil {
		return fmt.Errorf("not connected")
	}
	resp, err := cli.GM.ListContacts()
	if err != nil {
		return err
	}
	for _, c := range resp.GetContacts() {
		number := ""
		if n := c.GetNumber(); n != nil {
			number = n.GetNumber()
		}
		contact := &db.Contact{
			ContactID: c.GetContactID(),
			Name:      c.GetName(),
			Number:    number,
		}
		if err := a.Store.UpsertContact(contact); err != nil {
			a.Logger.Warn().Err(err).Str("id", contact.ContactID).Msg("Failed to cache contact")
		}
	}
	return nil
}

func conversationContacts(reads readsource.ReadSource, query string, limit int) ([]*db.Contact, error) {
	if limit <= 0 {
		limit = 50
	}
	convs, err := reads.ListConversations(math.MaxInt)
	if err != nil {
		return nil, err
	}
	return db.ContactsFromConversations(convs, query, limit), nil
}

func primaryContacts(options Options, query string, limit int) ([]*db.Contact, error) {
	if limit <= 0 {
		limit = 50
	}
	fromConvos, err := conversationContacts(options.Reads, query, limit)
	if err != nil {
		return nil, err
	}
	var fromBook []*db.Contact
	if v2 := v2WriteStore(options); v2 != nil {
		listed, bookErr := v2.ListAddressBookContacts(query, limit)
		if bookErr != nil {
			return nil, bookErr
		}
		fromBook = listed
	}
	return mergePrimaryContacts(fromBook, fromConvos, limit), nil
}

func mergePrimaryContacts(book, convos []*db.Contact, limit int) []*db.Contact {
	const maxContacts = 100
	if limit <= 0 || limit > maxContacts {
		limit = 50
	}
	seen := map[string]bool{}
	out := make([]*db.Contact, 0, limit)
	appendUnique := func(list []*db.Contact) {
		for _, c := range list {
			if c == nil {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(c.Name)) + "|" + digitsOnly(c.Number)
			if key == "|" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
			if len(out) >= limit {
				return
			}
		}
	}
	appendUnique(book)
	appendUnique(convos)
	return out
}
