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
	script := clipboardMediaScriptMacOS(`/tmp/test\path"quotes/clip.png`)
	if !strings.Contains(script, `test\\path\"quotes`) {
		t.Fatalf("expected escaped backslash and double quote in AppleScript: %s", script)
	}
	furlIdx := strings.Index(script, "class furl")
	pngIdx := strings.Index(script, "class PNG")
	if furlIdx < 0 || pngIdx < 0 {
		t.Fatal("missing clipboard PNG and furl checks in AppleScript")
	}
	if furlIdx > pngIdx {
		t.Fatal("expected class furl check to precede class PNG check")
	}
}
