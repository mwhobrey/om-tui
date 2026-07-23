package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/maxghenis/openmessage/internal/localapi"
)

// Google Messages reaction palette (matches BuildReactionPayload emoji mapping).
var reactPaletteEmojis = []string{
	"👍", "❤️", "😂", "😮", "😥", "😠", "👎", "🤔", "😢",
}

type reactionEntry struct {
	Emoji  string   `json:"emoji"`
	Count  int      `json:"count"`
	Actors []string `json:"actors,omitempty"`
}

type reactionParticipant struct {
	Name      string `json:"name"`
	Number    string `json:"number"`
	Phone     string `json:"phone"`
	ID        string `json:"id"`
	IsMe      bool   `json:"is_me"`
	IsMeCamel bool   `json:"isMe"`
}

func parseReactions(raw string) []reactionEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var entries []reactionEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	out := entries[:0]
	for _, e := range entries {
		if strings.TrimSpace(e.Emoji) == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

func parseReactionParticipants(raw string) []reactionParticipant {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var participants []reactionParticipant
	if err := json.Unmarshal([]byte(raw), &participants); err != nil {
		return nil
	}
	return participants
}

// formatReactions renders emoji reactions with reactor names when actors resolve.
// peerFallback is used for a single unresolved actor in 1:1 threads (conversation name).
// Examples: "👍 Alice", "👍 Alice,Bob", "❤️ you", "😂3" (unresolved actors).
func formatReactions(raw string, resolve func(actor string) string, peerFallback string) string {
	entries := parseReactions(raw)
	if len(entries) == 0 {
		return ""
	}
	peerFallback = shortDisplayName(peerFallback)
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		names := make([]string, 0, len(e.Actors))
		seen := map[string]bool{}
		for _, actor := range e.Actors {
			label := ""
			if resolve != nil {
				label = strings.TrimSpace(resolve(actor))
			}
			if label == "" {
				label = fallbackActorLabel(actor)
			}
			if label == "" || seen[label] {
				continue
			}
			seen[label] = true
			names = append(names, label)
		}
		if len(names) == 0 && peerFallback != "" && (len(e.Actors) == 1 || (len(e.Actors) == 0 && e.Count == 1)) {
			names = append(names, peerFallback)
		}
		switch {
		case len(names) > 0:
			parts = append(parts, fmt.Sprintf("%s %s", e.Emoji, strings.Join(names, ",")))
		case e.Count > 1:
			parts = append(parts, fmt.Sprintf("%s%d", e.Emoji, e.Count))
		default:
			parts = append(parts, e.Emoji)
		}
	}
	return strings.Join(parts, " ")
}

func fallbackActorLabel(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ""
	}
	lower := strings.ToLower(actor)
	if lower == "me" || lower == "self" {
		return "you"
	}
	// Tapbacks sometimes store SenderName as the actor. Reject opaque IDs
	// (participant tokens, JIDs, digit-heavy keys).
	if strings.Contains(actor, "@") || strings.ContainsFunc(actor, unicode.IsDigit) {
		return ""
	}
	if strings.ContainsFunc(actor, unicode.IsSpace) {
		return shortDisplayName(actor)
	}
	for _, r := range actor {
		if unicode.IsLetter(r) || r == '\'' {
			continue
		}
		return ""
	}
	if !strings.ContainsFunc(actor, unicode.IsLetter) {
		return ""
	}
	return shortDisplayName(actor)
}

func shortDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if i := strings.IndexFunc(name, unicode.IsSpace); i > 0 {
		return name[:i]
	}
	return name
}

// reactionPeerFallback returns a 1:1 conversation's peer short name for unresolved
// single-actor reactions. Groups return empty so we don't label reactors with the room name.
func reactionPeerFallback(participantsJSON, convName string) string {
	participants := parseReactionParticipants(participantsJSON)
	if len(participants) == 0 {
		return shortDisplayName(convName)
	}
	others := 0
	for _, p := range participants {
		if p.IsMe || p.IsMeCamel {
			continue
		}
		others++
	}
	if others != 1 {
		return ""
	}
	return shortDisplayName(convName)
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripMessagingJID(actor string) string {
	actor = strings.TrimSpace(actor)
	if i := strings.IndexByte(actor, '@'); i > 0 {
		return actor[:i]
	}
	return actor
}

// reactionResolver maps reaction actor IDs/numbers to short display names.
func reactionResolver(participantsJSON, peerName string, msgs []localapi.Message) func(string) string {
	participants := parseReactionParticipants(participantsJSON)
	byID := map[string]string{}
	byDigits := map[string]string{}
	peer := shortDisplayName(peerName)

	register := func(key, name string) {
		key = strings.TrimSpace(key)
		name = shortDisplayName(name)
		if key == "" || name == "" {
			return
		}
		if _, ok := byID[key]; !ok {
			byID[key] = name
		}
		if d := digitsOnly(key); len(d) >= 7 {
			if _, ok := byDigits[d]; !ok {
				byDigits[d] = name
			}
			// Match on last 10 digits for US-style numbers with country code drift.
			if len(d) > 10 {
				tail := d[len(d)-10:]
				if _, ok := byDigits[tail]; !ok {
					byDigits[tail] = name
				}
			}
		}
	}

	for _, p := range participants {
		name := strings.TrimSpace(p.Name)
		if p.IsMe || p.IsMeCamel {
			register(p.ID, "you")
			register(p.Number, "you")
			register(p.Phone, "you")
			register("me", "you")
			continue
		}
		if name == "" {
			name = peer
		}
		if name == "" {
			name = strings.TrimSpace(p.Number)
			if name == "" {
				name = strings.TrimSpace(p.Phone)
			}
		}
		register(p.ID, name)
		register(p.Number, name)
		register(p.Phone, name)
	}

	for _, msg := range msgs {
		if msg.IsFromMe {
			continue
		}
		name := strings.TrimSpace(msg.SenderName)
		if name == "" {
			name = peer
		}
		register(msg.SenderNumber, name)
	}

	return func(actor string) string {
		actor = strings.TrimSpace(actor)
		if actor == "" {
			return ""
		}
		lower := strings.ToLower(actor)
		if lower == "me" || lower == "self" {
			return "you"
		}
		if name, ok := byID[actor]; ok {
			return name
		}
		stripped := stripMessagingJID(actor)
		if stripped != actor {
			if name, ok := byID[stripped]; ok {
				return name
			}
		}
		if d := digitsOnly(stripped); d != "" {
			if name, ok := byDigits[d]; ok {
				return name
			}
			if len(d) > 10 {
				if name, ok := byDigits[d[len(d)-10:]]; ok {
					return name
				}
			}
		}
		return fallbackActorLabel(actor)
	}
}

func reactionHasEmoji(raw, emoji string) bool {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return false
	}
	for _, e := range parseReactions(raw) {
		if e.Emoji == emoji {
			return true
		}
	}
	return false
}

func reactionAction(raw, emoji string) string {
	if reactionHasEmoji(raw, emoji) {
		return "remove"
	}
	return "add"
}

func clampMessageIndex(n, selected int) int {
	if n <= 0 {
		return -1
	}
	if selected < 0 || selected >= n {
		return n - 1
	}
	return selected
}

func selectedMessage(msgs []localapi.Message, selected int) (localapi.Message, bool) {
	idx := clampMessageIndex(len(msgs), selected)
	if idx < 0 {
		return localapi.Message{}, false
	}
	return msgs[idx], true
}

func reactPaletteHelp() string {
	parts := make([]string, 0, len(reactPaletteEmojis))
	for i, e := range reactPaletteEmojis {
		parts = append(parts, fmt.Sprintf("%d%s", i+1, e))
	}
	return strings.Join(parts, " ")
}
