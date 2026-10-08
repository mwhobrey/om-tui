// Package export serializes a conversation's messages to JSON, YAML or CSV.
//
// It is shared by the `export` CLI command (which reads the store directly)
// and the TUI `>export` palette command (which reads through the daemon API),
// so both produce byte-identical files for the same input.
package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/localapi"
)

// Format is an output serialization.
type Format string

const (
	JSON Format = "json"
	YAML Format = "yaml"
	CSV  Format = "csv"
)

// ParseFormat accepts json, yaml/yml and csv (case-insensitive).
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return JSON, nil
	case "yaml", "yml":
		return YAML, nil
	case "csv":
		return CSV, nil
	}
	return "", fmt.Errorf("unknown export format %q (use json, yaml or csv)", s)
}

// Ext is the file extension for the format, without the dot.
func (f Format) Ext() string { return string(f) }

// Message is one exported message. Secrets (media decryption keys) are never
// carried over from the store types.
type Message struct {
	MessageID    string `json:"message_id" yaml:"message_id"`
	Timestamp    string `json:"timestamp" yaml:"timestamp"` // RFC3339, local zone
	TimestampMS  int64  `json:"timestamp_ms" yaml:"timestamp_ms"`
	Sender       string `json:"sender" yaml:"sender"`
	SenderNumber string `json:"sender_number,omitempty" yaml:"sender_number,omitempty"`
	IsFromMe     bool   `json:"is_from_me" yaml:"is_from_me"`
	Body         string `json:"body" yaml:"body"`
	Platform     string `json:"source_platform,omitempty" yaml:"source_platform,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
	MediaID      string `json:"media_id,omitempty" yaml:"media_id,omitempty"`
	MimeType     string `json:"mime_type,omitempty" yaml:"mime_type,omitempty"`
	ReplyToID    string `json:"reply_to_id,omitempty" yaml:"reply_to_id,omitempty"`
	Reactions    string `json:"reactions,omitempty" yaml:"reactions,omitempty"`
	Transcript   string `json:"transcript,omitempty" yaml:"transcript,omitempty"`
}

// Conversation is the header describing what was exported.
type Conversation struct {
	ID           string `json:"id" yaml:"id"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Platform     string `json:"source_platform,omitempty" yaml:"source_platform,omitempty"`
	RiverID      string `json:"river_id,omitempty" yaml:"river_id,omitempty"`
	IsGroup      bool   `json:"is_group,omitempty" yaml:"is_group,omitempty"`
	Participants string `json:"participants,omitempty" yaml:"participants,omitempty"`
}

// Document is the JSON/YAML envelope. CSV carries only the message rows.
type Document struct {
	Conversation Conversation `json:"conversation" yaml:"conversation"`
	Since        string       `json:"since,omitempty" yaml:"since,omitempty"`
	Until        string       `json:"until,omitempty" yaml:"until,omitempty"`
	ExportedAt   string       `json:"exported_at" yaml:"exported_at"`
	Count        int          `json:"count" yaml:"count"`
	Messages     []Message    `json:"messages" yaml:"messages"`
}

// Range is an inclusive [SinceMS, UntilMS] window; a zero side is unbounded.
type Range struct {
	SinceMS int64
	UntilMS int64
}

// Contains reports whether ts falls inside the range.
func (r Range) Contains(ts int64) bool {
	return (r.SinceMS == 0 || ts >= r.SinceMS) && (r.UntilMS == 0 || ts <= r.UntilMS)
}

// ConversationFromDB maps a store conversation to the export header.
func ConversationFromDB(c *db.Conversation) Conversation {
	if c == nil {
		return Conversation{}
	}
	return Conversation{
		ID: c.ConversationID, Name: c.Name, Platform: c.SourcePlatform,
		RiverID: c.RiverID, IsGroup: c.IsGroup, Participants: c.Participants,
	}
}

// ConversationFromLocal maps a daemon API conversation to the export header.
func ConversationFromLocal(c localapi.Conversation) Conversation {
	return Conversation{
		ID: c.ConversationID, Name: c.Name, Platform: c.SourcePlatform,
		RiverID: c.RiverID, IsGroup: c.IsGroup, Participants: c.Participants,
	}
}

func sender(isFromMe bool, name, number, fallback string) string {
	if isFromMe {
		return "me"
	}
	for _, v := range []string{name, number, fallback} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func stamp(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

// FromDB maps a store message.
func FromDB(m *db.Message) Message {
	return Message{
		MessageID: m.MessageID, Timestamp: stamp(m.TimestampMS), TimestampMS: m.TimestampMS,
		Sender: sender(m.IsFromMe, m.SenderName, m.SenderNumber, m.ConversationID), SenderNumber: m.SenderNumber,
		IsFromMe: m.IsFromMe, Body: m.Body, Platform: m.SourcePlatform, Status: m.Status,
		MediaID: m.MediaID, MimeType: m.MimeType, ReplyToID: m.ReplyToID,
		Reactions: m.Reactions, Transcript: m.Transcript,
	}
}

// FromLocal maps a daemon API message.
func FromLocal(m localapi.Message) Message {
	return Message{
		MessageID: m.MessageID, Timestamp: stamp(m.TimestampMS), TimestampMS: m.TimestampMS,
		Sender: sender(m.IsFromMe, m.SenderName, m.SenderNumber, m.ConversationID), SenderNumber: m.SenderNumber,
		IsFromMe: m.IsFromMe, Body: m.Body, Platform: m.SourcePlatform, Status: m.Status,
		MediaID: m.MediaID, MimeType: m.MimeType, ReplyToID: m.ReplyToID, Reactions: m.Reactions,
	}
}

// SortChronological orders messages oldest→newest (ties broken by id) and
// drops duplicate message ids, which paged fetches can produce.
func SortChronological(msgs []Message) []Message {
	seen := make(map[string]struct{}, len(msgs))
	out := msgs[:0:0]
	for _, m := range msgs {
		if m.MessageID != "" {
			if _, dup := seen[m.MessageID]; dup {
				continue
			}
			seen[m.MessageID] = struct{}{}
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TimestampMS != out[j].TimestampMS {
			return out[i].TimestampMS < out[j].TimestampMS
		}
		return out[i].MessageID < out[j].MessageID
	})
	return out
}

var csvHeader = []string{
	"timestamp", "timestamp_ms", "sender", "sender_number", "is_from_me", "body",
	"source_platform", "status", "message_id", "reply_to_id", "media_id", "mime_type", "reactions", "transcript",
}

// Write serializes conv and msgs (assumed chronological) to w.
func Write(w io.Writer, f Format, conv Conversation, r Range, msgs []Message) error {
	if msgs == nil {
		msgs = []Message{}
	}
	switch f {
	case JSON, YAML:
		doc := Document{
			Conversation: conv, Since: stamp(r.SinceMS), Until: stamp(r.UntilMS),
			ExportedAt: time.Now().Format(time.RFC3339), Count: len(msgs), Messages: msgs,
		}
		if f == JSON {
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			return enc.Encode(doc)
		}
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(doc); err != nil {
			return err
		}
		return enc.Close()
	case CSV:
		cw := csv.NewWriter(w)
		if err := cw.Write(csvHeader); err != nil {
			return err
		}
		for _, m := range msgs {
			if err := cw.Write([]string{
				m.Timestamp, strconv.FormatInt(m.TimestampMS, 10), m.Sender, m.SenderNumber,
				strconv.FormatBool(m.IsFromMe), m.Body, m.Platform, m.Status, m.MessageID,
				m.ReplyToID, m.MediaID, m.MimeType, m.Reactions, m.Transcript,
			}); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	}
	return fmt.Errorf("unsupported export format %q", f)
}

// WriteFile writes the export to path (creating parent dirs), owner-only
// because message history is private. It returns the absolute path.
func WriteFile(path string, f Format, conv Conversation, r Range, msgs []Message) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	// OpenFile's mode only applies to files it creates; tighten a pre-existing
	// file before any conversation data lands in it.
	if err := out.Chmod(0o600); err != nil {
		out.Close()
		return "", err
	}
	if err := Write(out, f, conv, r, msgs); err != nil {
		out.Close()
		return "", err
	}
	return abs, out.Close()
}

// DefaultDir is where exports land when no path is given:
// $OPENMESSAGES_EXPORT_DIR/exports, else ~/Documents/OpenMessage/exports.
func DefaultDir() (string, error) {
	if root := strings.TrimSpace(os.Getenv("OPENMESSAGES_EXPORT_DIR")); root != "" {
		return filepath.Join(root, "exports"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents", "OpenMessage", "exports"), nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._+-]+`)

// DefaultFilename builds e.g. "Alice_sms_2026-05-01_2026-05-31.json".
func DefaultFilename(conv Conversation, r Range, f Format) string {
	label := conv.Name
	if strings.TrimSpace(label) == "" {
		label = conv.ID
	}
	parts := []string{label}
	if conv.Platform != "" {
		parts = append(parts, conv.Platform)
	}
	day := func(ms int64) string {
		if ms <= 0 {
			return ""
		}
		return time.UnixMilli(ms).Format("2006-01-02")
	}
	if s, u := day(r.SinceMS), day(r.UntilMS); s != "" || u != "" {
		if s == "" {
			s = "start"
		}
		if u == "" {
			u = "now"
		}
		parts = append(parts, s, u)
	}
	name := strings.Trim(unsafeName.ReplaceAllString(strings.Join(parts, "_"), "_"), "._")
	if len(name) > 120 {
		name = name[:120]
	}
	if name == "" {
		name = "export"
	}
	return name + "." + f.Ext()
}

// ParseBound parses a since/until value into a millisecond timestamp in local
// time. Accepts YYYY-MM-DD, "YYYY-MM-DD HH:MM", "YYYY-MM-DDTHH:MM" or RFC3339.
// Empty returns 0 (unbounded). With endOfDay, a bare date resolves to the last
// millisecond of that day so an --until date includes the whole day.
func ParseBound(s string, endOfDay bool) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		if endOfDay {
			// Calendar day, not 24h: DST days are 23 or 25 hours long.
			t = t.AddDate(0, 0, 1).Add(-time.Millisecond)
		}
		return t.UnixMilli(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("invalid date %q (use YYYY-MM-DD or YYYY-MM-DD HH:MM)", s)
}
