//go:build darwin

package tui

import (
	"strings"
	"testing"
)

func TestOpenFileDialogScriptMacOS(t *testing.T) {
	script := openFileDialogScriptMacOS()
	if !strings.Contains(script, "choose file with prompt") {
		t.Fatal("missing choose file prompt in AppleScript")
	}
	if !strings.Contains(script, "error number -128") || !strings.Contains(script, `"CANCEL"`) {
		t.Fatal("missing cancellation sentinel in AppleScript")
	}
}

func TestClipboardMediaScriptMacOSEscapesPath(t *testing.T) {
	script := clipboardMediaScriptMacOS(`/tmp/test"quotes/clip.png`)
	if !strings.Contains(script, `test\"quotes`) {
		t.Fatalf("expected escaped double quote in AppleScript: %s", script)
	}
	if !strings.Contains(script, "class PNG") || !strings.Contains(script, "class furl") {
		t.Fatal("missing clipboard PNG and furl checks in AppleScript")
	}
}
