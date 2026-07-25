package tui

import (
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestMediaLabel(t *testing.T) {
	got := mediaLabel(localapi.Message{MimeType: "image/jpeg", Body: "look"})
	if got != "[image] look" {
		t.Fatalf("got %q", got)
	}
	got = mediaLabel(localapi.Message{MimeType: "video/mp4"})
	if got != "[video]" {
		t.Fatalf("got %q", got)
	}
	got = mediaLabel(localapi.Message{MimeType: "application/pdf"})
	if got != "[file] application/pdf" {
		t.Fatalf("got %q", got)
	}
}

func TestLatestMediaMessage(t *testing.T) {
	msgs := []localapi.Message{
		{MessageID: "1", Body: "hi"},
		{MessageID: "2", MediaID: "m1", MimeType: "image/png"},
		{MessageID: "3", Body: "later"},
		{MessageID: "4", MediaID: "m2", MimeType: "video/mp4", Body: "clip"},
	}
	got, ok := latestMediaMessage(msgs)
	if !ok || got.MessageID != "4" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestLatestMediaMessageMimeOnlyWithCaption(t *testing.T) {
	// MimeType + caption body must still count as media (HasMedia).
	msgs := []localapi.Message{
		{MessageID: "1", Body: "text only"},
		{MessageID: "2", MimeType: "image/jpeg", Body: "look at this"},
	}
	got, ok := latestMediaMessage(msgs)
	if !ok || got.MessageID != "2" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
	if _, ok := latestDownloadableMediaMessage(msgs); ok {
		t.Fatal("MimeType-only stub must not be downloadable")
	}
}

func TestResolveMediaMessageThreadSelectionNoFallback(t *testing.T) {
	msgs := []localapi.Message{
		{MessageID: "media", MediaID: "m1", MimeType: "image/png"},
		{MessageID: "empty", Body: ""},
		{MessageID: "mime-only", MimeType: "image/jpeg"},
	}

	_, _, err := resolveMediaMessage(msgs, 1, true)
	if err == nil || !strings.Contains(err.Error(), "no media") {
		t.Fatalf("empty body selected: err=%v", err)
	}

	_, _, err = resolveMediaMessage(msgs, 2, true)
	if err == nil || !strings.Contains(err.Error(), "no downloadable") {
		t.Fatalf("mime-only selected: err=%v", err)
	}

	got, ok, err := resolveMediaMessage(msgs, 0, true)
	if err != nil || !ok || got.MessageID != "media" {
		t.Fatalf("downloadable selected: got=%#v ok=%v err=%v", got, ok, err)
	}

	got, ok, err = resolveMediaMessage(msgs, 1, false)
	if err != nil || !ok || got.MessageID != "media" {
		t.Fatalf("list/compose fallback: got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestExtensionForMIME(t *testing.T) {
	if extensionForMIME("image/jpeg") != ".jpg" {
		t.Fatal(extensionForMIME("image/jpeg"))
	}
	if extensionForMIME("application/octet-stream") != ".bin" {
		t.Fatal(extensionForMIME("application/octet-stream"))
	}
}
