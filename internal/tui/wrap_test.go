package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestWrapTextBasic(t *testing.T) {
	got := wrapText("hello world from openmessage", 12)
	if len(got) < 2 {
		t.Fatalf("expected multiple wrapped lines, got %#v", got)
	}
	for _, line := range got {
		if runewidth.StringWidth(line) > 12 {
			t.Fatalf("line too wide %q (%d)", line, runewidth.StringWidth(line))
		}
	}
	joined := strings.Join(got, " ")
	if joined != "hello world from openmessage" {
		t.Fatalf("lost words: %#v (joined %q)", got, joined)
	}
}

func TestWrapTextHardBreak(t *testing.T) {
	got := wrapText("abcdefghij", 4)
	if len(got) < 2 {
		t.Fatalf("expected hard wrap, got %#v", got)
	}
	for _, line := range got {
		if runewidth.StringWidth(line) > 4 {
			t.Fatalf("line too wide %q", line)
		}
	}
}

func TestWrapTextNewline(t *testing.T) {
	got := wrapText("one\ntwo", 20)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got %#v", got)
	}
}

func TestWrapTextEmpty(t *testing.T) {
	got := wrapText("", 10)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("got %#v", got)
	}
}
