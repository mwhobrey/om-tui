package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/export"
	"github.com/maxghenis/openmessage/internal/readsource"
)

const exportPageSize = 1000

const exportUsage = "usage: om-tui export <name|number|conversation_id> [--format json|yaml|csv] [--since DATE] [--until DATE] [--out PATH|-]"

// RunExport handles "export <conversation> [--format json|yaml|csv]
// [--since ...] [--until ...] [--out PATH|-]".
//
// It writes one conversation (an SMS/RCS thread, a Slack channel, ...) for an
// optional time window. Read-only, like "read" and "thread": it opens the store
// without repair sweeps and starts no transports. --out - writes to stdout;
// with no --out the file goes to the export dir (see export.DefaultDir).
func RunExport(logger zerolog.Logger, args ...string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return fmt.Errorf(exportUsage)
	}
	query, rest := args[0], args[1:]

	format := export.JSON
	if v := flagValue(rest, "--format"); v != "" {
		f, err := export.ParseFormat(v)
		if err != nil {
			return err
		}
		format = f
	}
	var rng export.Range
	var err error
	if rng.SinceMS, err = export.ParseBound(flagValue(rest, "--since"), false); err != nil {
		return fmt.Errorf("--since: %w", err)
	}
	if rng.UntilMS, err = export.ParseBound(flagValue(rest, "--until"), true); err != nil {
		return fmt.Errorf("--until: %w", err)
	}
	if rng.SinceMS > 0 && rng.UntilMS > 0 && rng.UntilMS < rng.SinceMS {
		return fmt.Errorf("--until is before --since")
	}
	out := flagValue(rest, "--out")

	// stdout carries the export when --out -, so keep the store banner off it.
	session, err := openCommandReadSource(logger, os.Stderr)
	if err != nil {
		return err
	}
	defer session.Close()

	conv, err := resolveExportConversation(session.Reads, query)
	if err != nil {
		return err
	}
	rows, err := loadExportMessages(session.Reads, conv.ConversationID, rng)
	if err != nil {
		return err
	}
	header := export.ConversationFromDB(conv)

	if out == "-" {
		return export.Write(os.Stdout, format, header, rng, rows)
	}
	if out == "" {
		dir, err := export.DefaultDir()
		if err != nil {
			return err
		}
		out = filepath.Join(dir, export.DefaultFilename(header, rng, format))
	}
	path, err := export.WriteFile(out, format, header, rng, rows)
	if err != nil {
		return err
	}
	fmt.Printf("Exported %d message(s) from %s to %s\n", len(rows), conversationLabel(conv), path)
	return nil
}

// resolveExportConversation finds exactly one conversation by id, then by
// case-insensitive substring of name / participants / id. Ambiguity is an
// error that lists the candidates, so a bulk export never silently picks wrong.
func resolveExportConversation(reads readsource.ReadSource, query string) (*db.Conversation, error) {
	if conv, err := reads.GetConversation(query); err == nil && conv != nil {
		return conv, nil
	}
	convs, err := reads.ListConversations(100000)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	var matches []*db.Conversation
	for _, c := range convs {
		if strings.Contains(strings.ToLower(c.Name+"\n"+c.Participants+"\n"+c.ConversationID), needle) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no conversation matches %q (list them with: om-tui threads)", query)
	case 1:
		return matches[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d conversations; re-run with a conversation_id:\n", query, len(matches))
	for i, c := range matches {
		if i == 25 {
			fmt.Fprintf(&b, "  ... and %d more\n", len(matches)-i)
			break
		}
		fmt.Fprintf(&b, "  %-14s  %s [%s]  (last: %s)\n", c.ConversationID,
			firstNonEmpty(c.Name, c.Participants, "(no name)"), c.SourcePlatform, fmtTS(c.LastMessageTS))
	}
	return nil, fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

// loadExportMessages pages the whole window oldest→newest. Pages use the
// (timestamp, id) cursor so messages sharing a millisecond are not dropped.
func loadExportMessages(reads readsource.ReadSource, conversationID string, rng export.Range) ([]export.Message, error) {
	var (
		rows    []export.Message
		afterMS int64
		afterID string
	)
	if rng.SinceMS > 0 {
		afterMS = rng.SinceMS - 1 // After() is strictly greater; Since is inclusive.
	}
	for {
		page, err := reads.GetMessagesByConversationAfter(conversationID, afterMS, afterID, exportPageSize)
		if err != nil {
			return nil, fmt.Errorf("load messages for %s: %w", conversationID, err)
		}
		for _, m := range page {
			if rng.UntilMS > 0 && m.TimestampMS > rng.UntilMS {
				return export.SortChronological(rows), nil
			}
			rows = append(rows, export.FromDB(m))
		}
		if len(page) < exportPageSize {
			break
		}
		last := page[len(page)-1]
		afterMS, afterID = last.TimestampMS, last.MessageID
	}
	return export.SortChronological(rows), nil
}
