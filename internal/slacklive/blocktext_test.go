package slacklive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestMessageFallbackTextPrefersBlocksOverPlainText(t *testing.T) {
	t.Parallel()
	got := messageFallbackText(slack.Msg{
		Text: "plain",
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			&slack.SectionBlock{Text: slack.NewTextBlockObject("mrkdwn", "from blocks", false, false)},
		}},
	})
	if got != "from blocks" {
		t.Fatalf("got %q", got)
	}
}

func TestMessageFallbackTextFlattensSectionAndHeader(t *testing.T) {
	t.Parallel()
	got := messageFallbackText(slack.Msg{
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			&slack.HeaderBlock{Text: slack.NewTextBlockObject("plain_text", "PR opened", true, false)},
			&slack.SectionBlock{
				Text: slack.NewTextBlockObject("mrkdwn", "*feat:* do the thing", false, false),
				Fields: []*slack.TextBlockObject{
					slack.NewTextBlockObject("mrkdwn", "*Author:*\n<@U1>", false, false),
				},
			},
			&slack.ActionBlock{Elements: &slack.BlockElements{ElementSet: []slack.BlockElement{
				&slack.ButtonBlockElement{Text: slack.NewTextBlockObject("plain_text", "View", true, false)},
			}}},
		}},
	})
	if !strings.Contains(got, "PR opened") || !strings.Contains(got, "*feat:* do the thing") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "<@U1>") || !strings.Contains(got, "[View]") {
		t.Fatalf("got %q", got)
	}
}

func TestMessageFallbackTextFlattensRichTextAndAttachments(t *testing.T) {
	t.Parallel()
	rich := messageFallbackText(slack.Msg{
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			&slack.RichTextBlock{Elements: []slack.RichTextElement{
				&slack.RichTextSection{Elements: []slack.RichTextSectionElement{
					slack.NewRichTextSectionTextElement("hello ", nil),
					slack.NewRichTextSectionUserElement("U9", nil),
					slack.NewRichTextSectionLinkElement("https://example.com", "site", nil),
				}},
			}},
		}},
	})
	if rich != "hello <@U9>site" {
		t.Fatalf("rich = %q", rich)
	}

	att := messageFallbackText(slack.Msg{
		Attachments: []slack.Attachment{{
			Title: "Build failed",
			Text:  "pkg/foo",
			Fields: []slack.AttachmentField{{
				Title: "Job",
				Value: "ci",
			}},
		}},
	})
	if !strings.Contains(att, "Build failed") || !strings.Contains(att, "Job: ci") {
		t.Fatalf("attachment = %q", att)
	}
}

func TestMessageFallbackTextUnmarshalsSlackJSON(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"blocks": [
			{"type":"header","text":{"type":"plain_text","text":"Deployed"}},
			{"type":"section","text":{"type":"mrkdwn","text":"shipped ` + "`v2`" + ` to prod"}}
		]
	}`)
	var msg slack.Msg
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	got := messageFallbackText(msg)
	if !strings.Contains(got, "Deployed") || !strings.Contains(got, "shipped") {
		t.Fatalf("got %q", got)
	}
}

func TestMessageLayoutTextRendersDividerQuoteAndPre(t *testing.T) {
	t.Parallel()
	got := messageFallbackText(slack.Msg{
		Text: "ignored fallback",
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			&slack.DividerBlock{},
			&slack.RichTextBlock{Elements: []slack.RichTextElement{
				&slack.RichTextQuote{Elements: []slack.RichTextSectionElement{
					slack.NewRichTextSectionTextElement("quoted", nil),
				}},
				&slack.RichTextPreformatted{Elements: []slack.RichTextSectionElement{
					slack.NewRichTextSectionTextElement("code", nil),
				}},
			}},
		}},
	})
	if !strings.Contains(got, "────────") || !strings.Contains(got, "│ quoted") || !strings.Contains(got, "  code") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "ignored fallback") {
		t.Fatalf("preferred short text over blocks: %q", got)
	}
}

func TestFlattenSectionAccessoryAndOverflow(t *testing.T) {
	t.Parallel()
	got := messageFallbackText(slack.Msg{
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			&slack.SectionBlock{
				Text:      slack.NewTextBlockObject("mrkdwn", "Ship it?", false, false),
				Accessory: slack.NewAccessory(slack.NewButtonBlockElement("go", "v", slack.NewTextBlockObject("plain_text", "Deploy", true, false)).WithURL("https://example.com/deploy")),
			},
			&slack.ActionBlock{Elements: &slack.BlockElements{ElementSet: []slack.BlockElement{
				slack.NewOverflowBlockElement("more",
					slack.NewOptionBlockObject("docs", slack.NewTextBlockObject("plain_text", "Docs", true, false), nil),
				),
			}}},
		}},
	})
	if !strings.Contains(got, "Ship it?") || !strings.Contains(got, "[Deploy]") || !strings.Contains(got, "[Docs]") {
		t.Fatalf("got %q", got)
	}
}

func TestInteractiveActionsURLAndApp(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(slack.Blocks{BlockSet: []slack.Block{
		slack.NewActionBlock("row",
			slack.NewButtonBlockElement("view", "v", slack.NewTextBlockObject("plain_text", "View", true, false)).WithURL("https://example.com/pr"),
			slack.NewButtonBlockElement("approve", "ok", slack.NewTextBlockObject("plain_text", "Approve", true, false)).WithStyle(slack.StylePrimary),
		),
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := InteractiveActions(raw)
	if len(got) != 2 {
		t.Fatalf("actions = %#v", got)
	}
	if got[0].Kind != "url" || got[0].URL != "https://example.com/pr" || got[0].Label != "View" {
		t.Fatalf("url action = %#v", got[0])
	}
	if got[1].Kind != "app" || got[1].URL != "" || got[1].Label != "Approve" || got[1].Style != string(slack.StylePrimary) {
		t.Fatalf("app action = %#v", got[1])
	}
}

func TestMessageDeepLink(t *testing.T) {
	t.Parallel()
	got := MessageDeepLink("T1", "C2", "123.456")
	if got != "slack://channel?team=T1&id=C2&message=123.456" {
		t.Fatalf("got %q", got)
	}
	if MessageDeepLink("", "C2", "1") != "" || MessageDeepLink("T1", "", "1") != "" {
		t.Fatal("empty parts must not produce a link")
	}
}

func TestInteractiveActionsInputBlock(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(slack.Blocks{BlockSet: []slack.Block{
		slack.NewInputBlock("in", slack.NewTextBlockObject("plain_text", "Comment", true, false), nil,
			slack.NewPlainTextInputBlockElement(slack.NewTextBlockObject("plain_text", "Write a note", true, false), "comment"),
		),
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := InteractiveActions(raw)
	if len(got) != 1 || got[0].Kind != "app" || got[0].Label != "Write a note" {
		t.Fatalf("actions = %#v", got)
	}
	flat := flattenBlocks([]slack.Block{slack.NewInputBlock("in", slack.NewTextBlockObject("plain_text", "Comment", true, false), nil,
		slack.NewPlainTextInputBlockElement(slack.NewTextBlockObject("plain_text", "Write a note", true, false), "comment"),
	)})
	if !strings.Contains(flat, "Comment") || !strings.Contains(flat, "[Write a note]") {
		t.Fatalf("flatten = %q", flat)
	}
}

func TestRenderBlocksJSONRoundTrip(t *testing.T) {
	t.Parallel()
	object := json.RawMessage(`{"blocks":[{"type":"header","text":{"type":"plain_text","text":"Title"}}]}`)
	if got := RenderBlocksJSON(object); got != "Title" {
		t.Fatalf("object = %q", got)
	}
	array := json.RawMessage(`[{"type":"header","text":{"type":"plain_text","text":"Title"}}]`)
	if got := RenderBlocksJSON(array); got != "Title" {
		t.Fatalf("array = %q", got)
	}
	var blocks slack.Blocks
	if err := json.Unmarshal(array, &blocks); err != nil {
		t.Fatal(err)
	}
	marshaled := MarshalBlocksJSON(blocks)
	if RenderBlocksJSON(marshaled) != "Title" {
		t.Fatalf("marshal round-trip = %q from %s", RenderBlocksJSON(marshaled), marshaled)
	}
}
