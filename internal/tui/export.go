package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/export"
	"github.com/maxghenis/openmessage/internal/localapi"
)

const (
	exportUsage = "usage: >export [json|yaml|csv] [selected | last N | FROM..TO]  (dates YYYY-MM-DD or YYYY-MM-DDTHH:MM; one date = that day)"

	exportPageSize = 500
	exportMaxLast  = 1000 // the messages endpoint caps one page at 1000
)

type exportDoneMsg struct {
	path  string
	count int
}

// exportOpts is a parsed ">export" request. Selected exports the one highlighted
// message; Last keeps the newest N; Range narrows by time. Last and Range combine
// (newest N inside the range).
type exportOpts struct {
	Format   export.Format
	Range    export.Range
	Last     int
	Selected bool
}

// parseExportArgs reads ">export" arguments, in any order: a format, "selected",
// "last N", or a range. The range is a single token so times need no quoting:
// "FROM..TO" with either side optional, or one bare date meaning that whole day.
func parseExportArgs(args string) (exportOpts, error) {
	opts := exportOpts{Format: export.JSON}
	toks := strings.Fields(args)
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if f, err := export.ParseFormat(tok); err == nil {
			opts.Format = f
			continue
		}
		switch strings.ToLower(tok) {
		case "selected", "sel", "this":
			opts.Selected = true
			continue
		case "last":
			if i+1 >= len(toks) {
				return opts, fmt.Errorf("last needs a count")
			}
			i++
			n, err := strconv.Atoi(toks[i])
			if err != nil || n <= 0 || n > exportMaxLast {
				return opts, fmt.Errorf("last N must be 1-%d", exportMaxLast)
			}
			opts.Last = n
			continue
		}
		from, to, isRange := strings.Cut(tok, "..")
		if !isRange {
			to = from
		}
		var err error
		if opts.Range.SinceMS, err = export.ParseBound(from, false); err != nil {
			return opts, err
		}
		if opts.Range.UntilMS, err = export.ParseBound(to, true); err != nil {
			return opts, err
		}
		if opts.Range.SinceMS > 0 && opts.Range.UntilMS > 0 && opts.Range.UntilMS < opts.Range.SinceMS {
			return opts, fmt.Errorf("range ends before it starts")
		}
	}
	if opts.Selected && (opts.Last > 0 || opts.Range != (export.Range{})) {
		return opts, fmt.Errorf("selected can't be combined with last or a range")
	}
	return opts, nil
}

// runExport exports from the open conversation through the daemon API.
func (m Model) runExport(args string) (tea.Model, tea.Cmd) {
	opts, err := parseExportArgs(args)
	if err != nil {
		m.err = err.Error() + " — " + exportUsage
		return m, nil
	}
	if m.activeID == "" {
		m.err = "open a conversation first — " + exportUsage
		return m, nil
	}
	var selected *localapi.Message
	if opts.Selected {
		sel, ok := selectedMessage(m.messages, m.selectedMsg)
		if !ok {
			m.err = "no message selected — focus the thread and pick one"
			return m, nil
		}
		selected = &sel
	}
	m.info = "Exporting…"
	return m, m.exportCmd(opts, selected)
}

func (m Model) exportCmd(opts exportOpts, selected *localapi.Message) tea.Cmd {
	client := m.session.Client
	convID := m.activeID
	conv := export.Conversation{ID: convID, Name: m.activeName, Participants: m.activeParticipants}
	cached := false
	for _, c := range m.paletteConvs {
		if c.ConversationID == convID {
			conv = export.ConversationFromLocal(c)
			cached = true
			break
		}
	}
	return func() tea.Msg {
		// Paging a long history can take a while; bound the whole export.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// The palette cache loads asynchronously and may be empty; without it
		// the platform/river/participants would silently drop out of the file.
		if !cached {
			if convs, err := client.ListConversations(ctx, 1000); err == nil {
				for _, c := range convs {
					if c.ConversationID == convID {
						conv = export.ConversationFromLocal(c)
						break
					}
				}
			}
		}

		var (
			rows   []export.Message
			suffix string
		)
		switch {
		case selected != nil:
			rows = []export.Message{export.FromLocal(*selected)}
			suffix = "_msg"
		case opts.Last > 0 && opts.Range == (export.Range{}):
			// Newest N straight from the API; no need to walk the whole thread.
			msgs, err := client.ConversationMessages(ctx, convID, opts.Last)
			if err != nil {
				return errMsg{err: fmt.Errorf("export: %w", err)}
			}
			for _, lm := range msgs {
				rows = append(rows, export.FromLocal(lm))
			}
		default:
			var err error
			if rows, err = fetchExportRange(ctx, client, convID, opts.Range); err != nil {
				return errMsg{err: fmt.Errorf("export: %w", err)}
			}
		}
		rows = export.SortChronological(rows)
		if opts.Last > 0 {
			suffix = fmt.Sprintf("_last%d", opts.Last)
			if len(rows) > opts.Last {
				rows = rows[len(rows)-opts.Last:]
			}
		}

		dir, err := export.DefaultDir()
		if err != nil {
			return errMsg{err: err}
		}
		named := conv
		if strings.TrimSpace(named.Name) == "" {
			named.Name = named.ID
		}
		named.Name += suffix
		path, err := export.WriteFile(filepath.Join(dir, export.DefaultFilename(named, opts.Range, opts.Format)), opts.Format, conv, opts.Range, rows)
		if err != nil {
			return errMsg{err: err}
		}
		return exportDoneMsg{path: path, count: len(rows)}
	}
}

// fetchExportRange pages a conversation oldest-first through the daemon API.
func fetchExportRange(ctx context.Context, client *localapi.Client, convID string, rng export.Range) ([]export.Message, error) {
	var (
		rows    []export.Message
		afterID string
	)
	afterMS := int64(1) // 0 means "no cursor" (newest-first) to the API
	if rng.SinceMS > 1 {
		afterMS = rng.SinceMS - 1 // the cursor is strictly-after; since is inclusive
	}
	for {
		page, err := client.ConversationMessagesAfter(ctx, convID, afterMS, afterID, exportPageSize)
		if err != nil {
			return nil, err
		}
		for _, lm := range page {
			if rng.UntilMS > 0 && lm.TimestampMS > rng.UntilMS {
				return rows, nil
			}
			rows = append(rows, export.FromLocal(lm))
		}
		if len(page) < exportPageSize {
			return rows, nil
		}
		last := page[len(page)-1]
		afterMS, afterID = last.TimestampMS, last.MessageID
	}
}
