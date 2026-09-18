package slacklive

import "testing"

func TestReactionNameAndEmojiRoundTripPalette(t *testing.T) {
	t.Parallel()
	for _, emoji := range []string{"👍", "👎", "❤️", "😂", "😮", "😥", "😠", "🤔", "😢"} {
		name := ReactionName(emoji)
		if name == "" || name == emoji {
			t.Fatalf("ReactionName(%q) = %q, want a Slack shortcode", emoji, name)
		}
		if got := ReactionEmoji(name); got != emoji {
			t.Fatalf("ReactionEmoji(%q) = %q, want %q", name, got, emoji)
		}
	}
	if got := ReactionName(":+1:"); got != "+1" {
		t.Fatalf("colon-wrapped name = %q", got)
	}
	if got := ReactionEmoji("thumbsup"); got != "👍" {
		t.Fatalf("thumbsup alias = %q", got)
	}
	if got := ReactionEmoji("partyparrot"); got != ":partyparrot:" {
		t.Fatalf("custom emoji = %q", got)
	}
}
