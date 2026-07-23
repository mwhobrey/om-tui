//go:build windows

package tui

import (
	"strings"
	"testing"
)

func TestOpenFileDialogScript(t *testing.T) {
	script := openFileDialogScript()
	if !strings.Contains(script, "OpenFileDialog") {
		t.Fatal("missing OpenFileDialog")
	}
	if !strings.Contains(script, "CANCEL") {
		t.Fatal("missing cancel sentinel")
	}
}

func TestClipboardMediaScriptEscapesPath(t *testing.T) {
	script := clipboardMediaScript(`C:\temp\O'Malley\clip.png`)
	if !strings.Contains(script, `O''Malley`) {
		t.Fatalf("expected escaped single quote in script: %s", script)
	}
	if !strings.Contains(script, "ContainsFileDropList") || !strings.Contains(script, "ContainsImage") {
		t.Fatal("missing clipboard checks")
	}
}

func TestClipboardTextScript(t *testing.T) {
	script := clipboardTextScript()
	if !strings.Contains(script, "GetText") {
		t.Fatal("missing GetText")
	}
}
