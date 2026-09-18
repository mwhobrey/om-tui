package slacklive

import "strings"

// Palette unicode → Slack reactions.add name. Slack stores :+1: as "+1".
var unicodeToSlackName = map[string]string{
	"👍":  "+1",
	"👎":  "-1",
	"❤️": "heart",
	"😂":  "joy",
	"😮":  "open_mouth",
	"😥":  "disappointed_relieved",
	"😠":  "angry",
	"🤔":  "thinking_face",
	"😢":  "cry",
}

var slackNameToUnicode = map[string]string{
	"+1":                    "👍",
	"thumbsup":              "👍",
	"-1":                    "👎",
	"thumbsdown":            "👎",
	"heart":                 "❤️",
	"joy":                   "😂",
	"open_mouth":            "😮",
	"disappointed_relieved": "😥",
	"angry":                 "😠",
	"thinking_face":         "🤔",
	"cry":                   "😢",
}

// ReactionName maps a TUI/unicode emoji (or already-named shortcode) to the
// Slack reactions.add name.
func ReactionName(emoji string) string {
	emoji = strings.Trim(strings.TrimSpace(emoji), ":")
	if emoji == "" {
		return ""
	}
	if name, ok := unicodeToSlackName[emoji]; ok {
		return name
	}
	return emoji
}

// ReactionEmoji maps a Slack reaction name onto the unicode glyph the TUI
// palette already uses. Unknown custom emoji stay as :name:.
func ReactionEmoji(name string) string {
	name = strings.Trim(strings.TrimSpace(name), ":")
	if name == "" {
		return ""
	}
	if glyph, ok := slackNameToUnicode[name]; ok {
		return glyph
	}
	return ":" + name + ":"
}
