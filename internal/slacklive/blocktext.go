package slacklive

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

func messageLayoutText(msg slack.Msg) string {
	if text := flattenBlocks(msg.Blocks.BlockSet); text != "" {
		return text
	}
	if text := strings.TrimSpace(msg.Text); text != "" {
		return text
	}
	return flattenAttachments(msg.Attachments)
}

func messageFallbackText(msg slack.Msg) string {
	return messageLayoutText(msg)
}

// RenderBlocksJSON turns stored Slack Block Kit JSON into TUI layout text.
func RenderBlocksJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var blocks slack.Blocks
	if err := json.Unmarshal(raw, &blocks); err != nil {
		var wrapped struct {
			Blocks slack.Blocks `json:"blocks"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return ""
		}
		blocks = wrapped.Blocks
	}
	return flattenBlocks(blocks.BlockSet)
}

// MarshalBlocksJSON persists Block Kit for V2 extras. Empty input yields nil.
func MarshalBlocksJSON(blocks slack.Blocks) json.RawMessage {
	if len(blocks.BlockSet) == 0 {
		return nil
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		return nil
	}
	return raw
}

func flattenBlocks(blocks []slack.Block) string {
	if len(blocks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text := flattenBlock(block); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func flattenBlock(block slack.Block) string {
	switch b := block.(type) {
	case *slack.SectionBlock:
		return flattenSection(b)
	case slack.SectionBlock:
		return flattenSection(&b)
	case *slack.HeaderBlock:
		return textObject(b.Text)
	case slack.HeaderBlock:
		return textObject(b.Text)
	case *slack.MarkdownBlock:
		return strings.TrimSpace(b.Text)
	case slack.MarkdownBlock:
		return strings.TrimSpace(b.Text)
	case *slack.ImageBlock:
		return flattenImage(b)
	case slack.ImageBlock:
		return flattenImage(&b)
	case *slack.ContextBlock:
		return flattenContext(b)
	case slack.ContextBlock:
		return flattenContext(&b)
	case *slack.RichTextBlock:
		return flattenRichText(b)
	case slack.RichTextBlock:
		return flattenRichText(&b)
	case *slack.VideoBlock:
		return flattenVideo(b)
	case slack.VideoBlock:
		return flattenVideo(&b)
	case *slack.ActionBlock:
		return flattenActions(b)
	case slack.ActionBlock:
		return flattenActions(&b)
	case *slack.InputBlock:
		return flattenInput(b)
	case slack.InputBlock:
		return flattenInput(&b)
	case *slack.DividerBlock:
		return "────────"
	case slack.DividerBlock:
		return "────────"
	default:
		return ""
	}
}

func flattenSection(block *slack.SectionBlock) string {
	if block == nil {
		return ""
	}
	parts := make([]string, 0, 1+len(block.Fields))
	if text := textObject(block.Text); text != "" {
		parts = append(parts, text)
	}
	for _, field := range block.Fields {
		if text := textObject(field); text != "" {
			parts = append(parts, text)
		}
	}
	if extra := flattenAccessory(block.Accessory); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, "\n")
}

func flattenImage(block *slack.ImageBlock) string {
	if block == nil {
		return ""
	}
	if text := textObject(block.Title); text != "" {
		return text
	}
	if alt := strings.TrimSpace(block.AltText); alt != "" {
		return "[image: " + alt + "]"
	}
	return ""
}

func flattenContext(block *slack.ContextBlock) string {
	if block == nil {
		return ""
	}
	parts := make([]string, 0, len(block.ContextElements.Elements))
	for _, el := range block.ContextElements.Elements {
		switch e := el.(type) {
		case *slack.TextBlockObject:
			if text := textObject(e); text != "" {
				parts = append(parts, text)
			}
		case slack.TextBlockObject:
			if text := textObject(&e); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " ")
}

func flattenVideo(block *slack.VideoBlock) string {
	if block == nil {
		return ""
	}
	if text := textObject(block.Title); text != "" {
		return text
	}
	if alt := strings.TrimSpace(block.AltText); alt != "" {
		return alt
	}
	return strings.TrimSpace(block.VideoURL)
}

func flattenActions(block *slack.ActionBlock) string {
	if block == nil || block.Elements == nil {
		return ""
	}
	parts := make([]string, 0, len(block.Elements.ElementSet))
	for _, el := range block.Elements.ElementSet {
		if text := flattenElement(el); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func flattenAccessory(acc *slack.Accessory) string {
	if acc == nil {
		return ""
	}
	switch {
	case acc.ButtonElement != nil:
		return flattenElement(acc.ButtonElement)
	case acc.OverflowElement != nil:
		return flattenElement(acc.OverflowElement)
	case acc.DatePickerElement != nil:
		return flattenElement(acc.DatePickerElement)
	case acc.TimePickerElement != nil:
		return flattenElement(acc.TimePickerElement)
	case acc.RadioButtonsElement != nil:
		return flattenElement(acc.RadioButtonsElement)
	case acc.SelectElement != nil:
		return flattenElement(acc.SelectElement)
	case acc.MultiSelectElement != nil:
		return flattenElement(acc.MultiSelectElement)
	case acc.CheckboxGroupsBlockElement != nil:
		return flattenElement(acc.CheckboxGroupsBlockElement)
	case acc.WorkflowButtonElement != nil:
		return flattenElement(acc.WorkflowButtonElement)
	default:
		return ""
	}
}

func flattenElement(el slack.BlockElement) string {
	switch e := el.(type) {
	case *slack.ButtonBlockElement:
		return flattenButton(e)
	case slack.ButtonBlockElement:
		return flattenButton(&e)
	case *slack.OverflowBlockElement:
		return flattenOverflow(e)
	case slack.OverflowBlockElement:
		return flattenOverflow(&e)
	case *slack.SelectBlockElement:
		return flattenSelect(e)
	case slack.SelectBlockElement:
		return flattenSelect(&e)
	case *slack.MultiSelectBlockElement:
		return flattenSelect(&slack.SelectBlockElement{Placeholder: e.Placeholder, Options: e.Options})
	case slack.MultiSelectBlockElement:
		return flattenSelect(&slack.SelectBlockElement{Placeholder: e.Placeholder, Options: e.Options})
	case *slack.DatePickerBlockElement:
		return flattenPicker("date", e.Placeholder, e.InitialDate)
	case slack.DatePickerBlockElement:
		return flattenPicker("date", e.Placeholder, e.InitialDate)
	case *slack.TimePickerBlockElement:
		return flattenPicker("time", e.Placeholder, e.InitialTime)
	case slack.TimePickerBlockElement:
		return flattenPicker("time", e.Placeholder, e.InitialTime)
	case *slack.RadioButtonsBlockElement:
		return flattenChoice("radio", e.Options)
	case slack.RadioButtonsBlockElement:
		return flattenChoice("radio", e.Options)
	case *slack.CheckboxGroupsBlockElement:
		return flattenChoice("check", e.Options)
	case slack.CheckboxGroupsBlockElement:
		return flattenChoice("check", e.Options)
	case *slack.WorkflowButtonBlockElement:
		return flattenButton(&slack.ButtonBlockElement{Text: e.Text, Style: e.Style})
	case slack.WorkflowButtonBlockElement:
		return flattenButton(&slack.ButtonBlockElement{Text: e.Text, Style: e.Style})
	case *slack.DateTimePickerBlockElement:
		return flattenPicker("datetime", nil, "")
	case slack.DateTimePickerBlockElement:
		return flattenPicker("datetime", nil, "")
	default:
		return flattenInputish(e)
	}
}

func flattenInput(block *slack.InputBlock) string {
	if block == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if label := textObject(block.Label); label != "" {
		parts = append(parts, label)
	}
	if extra := flattenElement(block.Element); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}

func flattenInputish(el slack.BlockElement) string {
	switch e := el.(type) {
	case *slack.PlainTextInputBlockElement:
		return flattenPicker("text", e.Placeholder, e.InitialValue)
	case slack.PlainTextInputBlockElement:
		return flattenPicker("text", e.Placeholder, e.InitialValue)
	case *slack.EmailTextInputBlockElement:
		return flattenPicker("email", e.Placeholder, e.InitialValue)
	case slack.EmailTextInputBlockElement:
		return flattenPicker("email", e.Placeholder, e.InitialValue)
	default:
		return ""
	}
}

func flattenButton(button *slack.ButtonBlockElement) string {
	if button == nil {
		return ""
	}
	label := textObject(button.Text)
	if label == "" {
		return ""
	}
	return "[" + label + "]"
}

func flattenOverflow(block *slack.OverflowBlockElement) string {
	if block == nil {
		return ""
	}
	parts := make([]string, 0, len(block.Options))
	for _, opt := range block.Options {
		if opt == nil {
			continue
		}
		if label := textObject(opt.Text); label != "" {
			parts = append(parts, "["+label+"]")
		}
	}
	return strings.Join(parts, " ")
}

func flattenSelect(block *slack.SelectBlockElement) string {
	if block == nil {
		return ""
	}
	if label := textObject(block.Placeholder); label != "" {
		return "[" + label + " ▾]"
	}
	if block.InitialOption != nil {
		if label := textObject(block.InitialOption.Text); label != "" {
			return "[" + label + " ▾]"
		}
	}
	return "[choose ▾]"
}

func flattenPicker(kind string, placeholder *slack.TextBlockObject, initial string) string {
	if label := textObject(placeholder); label != "" {
		return "[" + label + "]"
	}
	if strings.TrimSpace(initial) != "" {
		return "[" + strings.TrimSpace(initial) + "]"
	}
	return "[" + kind + "]"
}

func flattenChoice(kind string, options []*slack.OptionBlockObject) string {
	parts := make([]string, 0, len(options))
	mark := "○"
	if kind == "check" {
		mark = "☐"
	}
	for _, opt := range options {
		if opt == nil {
			continue
		}
		if label := textObject(opt.Text); label != "" {
			parts = append(parts, mark+" "+label)
		}
	}
	return strings.Join(parts, " ")
}

const (
	blockActionURL = "url"
	blockActionApp = "app"
)

// BlockAction is one TUI-activatable Slack Block Kit control.
type BlockAction struct {
	Label string
	Kind  string
	URL   string
	Style string
}

// InteractiveActions extracts buttons, overflow items, and other controls
// from stored Block Kit JSON.
func InteractiveActions(raw json.RawMessage) []BlockAction {
	if len(raw) == 0 {
		return nil
	}
	var blocks slack.Blocks
	if err := json.Unmarshal(raw, &blocks); err != nil {
		var wrapped struct {
			Blocks slack.Blocks `json:"blocks"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil
		}
		blocks = wrapped.Blocks
	}
	var out []BlockAction
	for _, block := range blocks.BlockSet {
		collectBlockActions(block, &out)
	}
	return out
}

// MessageDeepLink is the Slack desktop URI for a message. App-owned buttons
// have no public click API; this is how the TUI hands them back to Slack.
func MessageDeepLink(teamID, channelID, ts string) string {
	teamID = strings.TrimSpace(teamID)
	channelID = strings.TrimSpace(channelID)
	ts = strings.TrimSpace(ts)
	if teamID == "" || channelID == "" || ts == "" {
		return ""
	}
	return "slack://channel?team=" + teamID + "&id=" + channelID + "&message=" + ts
}

func collectBlockActions(block slack.Block, out *[]BlockAction) {
	switch b := block.(type) {
	case *slack.SectionBlock:
		collectAccessoryActions(b.Accessory, out)
	case slack.SectionBlock:
		collectAccessoryActions(b.Accessory, out)
	case *slack.ActionBlock:
		if b.Elements == nil {
			return
		}
		for _, el := range b.Elements.ElementSet {
			collectElementActions(el, out)
		}
	case slack.ActionBlock:
		if b.Elements == nil {
			return
		}
		for _, el := range b.Elements.ElementSet {
			collectElementActions(el, out)
		}
	case *slack.InputBlock:
		collectInputActions(b, out)
	case slack.InputBlock:
		collectInputActions(&b, out)
	}
}

func collectInputActions(block *slack.InputBlock, out *[]BlockAction) {
	if block == nil {
		return
	}
	before := len(*out)
	collectElementActions(block.Element, out)
	if len(*out) == before {
		addAppAction(firstNonEmpty(textObject(block.Label), "Input"), "", out)
	}
}

func collectAccessoryActions(acc *slack.Accessory, out *[]BlockAction) {
	if acc == nil {
		return
	}
	switch {
	case acc.ButtonElement != nil:
		collectElementActions(acc.ButtonElement, out)
	case acc.OverflowElement != nil:
		collectElementActions(acc.OverflowElement, out)
	case acc.DatePickerElement != nil:
		collectElementActions(acc.DatePickerElement, out)
	case acc.TimePickerElement != nil:
		collectElementActions(acc.TimePickerElement, out)
	case acc.RadioButtonsElement != nil:
		collectElementActions(acc.RadioButtonsElement, out)
	case acc.SelectElement != nil:
		collectElementActions(acc.SelectElement, out)
	case acc.MultiSelectElement != nil:
		collectElementActions(acc.MultiSelectElement, out)
	case acc.CheckboxGroupsBlockElement != nil:
		collectElementActions(acc.CheckboxGroupsBlockElement, out)
	case acc.WorkflowButtonElement != nil:
		collectElementActions(acc.WorkflowButtonElement, out)
	}
}

func collectElementActions(el slack.BlockElement, out *[]BlockAction) {
	switch e := el.(type) {
	case *slack.ButtonBlockElement:
		addButtonAction(e, out)
	case slack.ButtonBlockElement:
		addButtonAction(&e, out)
	case *slack.OverflowBlockElement:
		addOverflowActions(e, out)
	case slack.OverflowBlockElement:
		addOverflowActions(&e, out)
	case *slack.SelectBlockElement:
		addSelectActions(e, out)
	case slack.SelectBlockElement:
		addSelectActions(&e, out)
	case *slack.MultiSelectBlockElement:
		addSelectActions(&slack.SelectBlockElement{Placeholder: e.Placeholder, Options: e.Options}, out)
	case slack.MultiSelectBlockElement:
		addSelectActions(&slack.SelectBlockElement{Placeholder: e.Placeholder, Options: e.Options}, out)
	case *slack.DatePickerBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Pick date"), "", out)
	case slack.DatePickerBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Pick date"), "", out)
	case *slack.TimePickerBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Pick time"), "", out)
	case slack.TimePickerBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Pick time"), "", out)
	case *slack.RadioButtonsBlockElement:
		addChoiceActions("Choose", e.Options, out)
	case slack.RadioButtonsBlockElement:
		addChoiceActions("Choose", e.Options, out)
	case *slack.CheckboxGroupsBlockElement:
		addChoiceActions("Choose", e.Options, out)
	case slack.CheckboxGroupsBlockElement:
		addChoiceActions("Choose", e.Options, out)
	case *slack.WorkflowButtonBlockElement:
		addAppAction(textObject(e.Text), string(e.Style), out)
	case slack.WorkflowButtonBlockElement:
		addAppAction(textObject(e.Text), string(e.Style), out)
	case *slack.DateTimePickerBlockElement:
		addAppAction("Pick date & time", "", out)
	case slack.DateTimePickerBlockElement:
		addAppAction("Pick date & time", "", out)
	case *slack.PlainTextInputBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Text"), "", out)
	case slack.PlainTextInputBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Text"), "", out)
	case *slack.EmailTextInputBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Email"), "", out)
	case slack.EmailTextInputBlockElement:
		addAppAction(firstNonEmpty(textObject(e.Placeholder), "Email"), "", out)
	}
}

func addButtonAction(button *slack.ButtonBlockElement, out *[]BlockAction) {
	if button == nil {
		return
	}
	label := textObject(button.Text)
	if label == "" {
		return
	}
	kind := blockActionApp
	if strings.TrimSpace(button.URL) != "" {
		kind = blockActionURL
	}
	*out = append(*out, BlockAction{
		Label: label,
		Kind:  kind,
		URL:   strings.TrimSpace(button.URL),
		Style: string(button.Style),
	})
}

func addOverflowActions(block *slack.OverflowBlockElement, out *[]BlockAction) {
	if block == nil {
		return
	}
	for _, opt := range block.Options {
		addOptionAction(opt, out)
	}
}

func addSelectActions(block *slack.SelectBlockElement, out *[]BlockAction) {
	if block == nil {
		return
	}
	added := 0
	for _, opt := range block.Options {
		before := len(*out)
		addOptionAction(opt, out)
		if len(*out) > before && (*out)[len(*out)-1].Kind == blockActionURL {
			added++
		}
	}
	if added == 0 {
		addAppAction(firstNonEmpty(textObject(block.Placeholder), "Choose"), "", out)
	}
}

func addChoiceActions(fallback string, options []*slack.OptionBlockObject, out *[]BlockAction) {
	added := 0
	for _, opt := range options {
		before := len(*out)
		addOptionAction(opt, out)
		if len(*out) > before {
			added++
		}
	}
	if added == 0 {
		addAppAction(fallback, "", out)
	}
}

func addOptionAction(opt *slack.OptionBlockObject, out *[]BlockAction) {
	if opt == nil {
		return
	}
	label := textObject(opt.Text)
	if label == "" {
		return
	}
	kind := blockActionApp
	if strings.TrimSpace(opt.URL) != "" {
		kind = blockActionURL
	}
	*out = append(*out, BlockAction{
		Label: label,
		Kind:  kind,
		URL:   strings.TrimSpace(opt.URL),
	})
}

func addAppAction(label, style string, out *[]BlockAction) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	*out = append(*out, BlockAction{Label: label, Kind: blockActionApp, Style: style})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func flattenRichText(block *slack.RichTextBlock) string {
	if block == nil {
		return ""
	}
	parts := make([]string, 0, len(block.Elements))
	for _, el := range block.Elements {
		if text := flattenRichElement(el); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func flattenRichElement(el slack.RichTextElement) string {
	switch e := el.(type) {
	case *slack.RichTextSection:
		return flattenRichSectionElements(e.Elements)
	case slack.RichTextSection:
		return flattenRichSectionElements(e.Elements)
	case *slack.RichTextQuote:
		return indentQuote(flattenRichSectionElements(e.Elements))
	case *slack.RichTextPreformatted:
		return indentPre(flattenRichSectionElements(e.Elements))
	case *slack.RichTextList:
		return flattenRichList(e)
	case slack.RichTextList:
		return flattenRichList(&e)
	default:
		return ""
	}
}

func flattenRichList(list *slack.RichTextList) string {
	if list == nil {
		return ""
	}
	parts := make([]string, 0, len(list.Elements))
	for i, el := range list.Elements {
		text := flattenRichElement(el)
		if text == "" {
			continue
		}
		if list.Style == slack.RTEListOrdered {
			parts = append(parts, fmt.Sprintf("%d. %s", i+1, text))
			continue
		}
		parts = append(parts, "- "+text)
	}
	return strings.Join(parts, "\n")
}

func flattenRichSectionElements(elements []slack.RichTextSectionElement) string {
	var b strings.Builder
	for _, el := range elements {
		switch e := el.(type) {
		case *slack.RichTextSectionTextElement:
			b.WriteString(e.Text)
		case slack.RichTextSectionTextElement:
			b.WriteString(e.Text)
		case *slack.RichTextSectionUserElement:
			b.WriteString("<@" + e.UserID + ">")
		case slack.RichTextSectionUserElement:
			b.WriteString("<@" + e.UserID + ">")
		case *slack.RichTextSectionChannelElement:
			b.WriteString("<#" + e.ChannelID + ">")
		case slack.RichTextSectionChannelElement:
			b.WriteString("<#" + e.ChannelID + ">")
		case *slack.RichTextSectionLinkElement:
			if strings.TrimSpace(e.Text) != "" {
				b.WriteString(e.Text)
			} else {
				b.WriteString(e.URL)
			}
		case slack.RichTextSectionLinkElement:
			if strings.TrimSpace(e.Text) != "" {
				b.WriteString(e.Text)
			} else {
				b.WriteString(e.URL)
			}
		case *slack.RichTextSectionEmojiElement:
			if e.Unicode != "" {
				b.WriteString(e.Unicode)
			} else if e.Name != "" {
				b.WriteString(":" + e.Name + ":")
			}
		case slack.RichTextSectionEmojiElement:
			if e.Unicode != "" {
				b.WriteString(e.Unicode)
			} else if e.Name != "" {
				b.WriteString(":" + e.Name + ":")
			}
		case *slack.RichTextSectionBroadcastElement:
			b.WriteString("@")
			b.WriteString(e.Range)
		case slack.RichTextSectionBroadcastElement:
			b.WriteString("@")
			b.WriteString(e.Range)
		}
	}
	return b.String()
}

func indentQuote(text string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "│ " + line
	}
	return strings.Join(lines, "\n")
}

func indentPre(text string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

func flattenAttachments(attachments []slack.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attachments))
	for _, att := range attachments {
		if text := flattenBlocks(att.Blocks.BlockSet); text != "" {
			parts = append(parts, text)
			continue
		}
		chunk := make([]string, 0, 4+len(att.Fields))
		for _, value := range []string{att.Pretext, att.Title, att.Text} {
			if strings.TrimSpace(value) != "" {
				chunk = append(chunk, strings.TrimSpace(value))
			}
		}
		for _, field := range att.Fields {
			line := strings.TrimSpace(field.Title)
			if strings.TrimSpace(field.Value) != "" {
				if line != "" {
					line += ": " + strings.TrimSpace(field.Value)
				} else {
					line = strings.TrimSpace(field.Value)
				}
			}
			if line != "" {
				chunk = append(chunk, line)
			}
		}
		if len(chunk) == 0 {
			if fallback := strings.TrimSpace(att.Fallback); fallback != "" {
				chunk = append(chunk, fallback)
			}
		}
		if text := strings.Join(chunk, "\n"); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func textObject(obj *slack.TextBlockObject) string {
	if obj == nil {
		return ""
	}
	return strings.TrimSpace(obj.Text)
}
