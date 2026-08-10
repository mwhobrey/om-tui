//go:build windows

package tui

import (
	"reflect"
	"runtime"
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

func TestOpenFileScriptEscapesPathAndStopsOnError(t *testing.T) {
	script := openFileScript(`C:\temp\O'Malley\file.png`)
	if !strings.Contains(script, `O''Malley`) {
		t.Fatalf("expected escaped single quote in script: %s", script)
	}
	if !strings.Contains(script, "Start-Process") {
		t.Fatal("missing Start-Process")
	}
	if !strings.Contains(script, "$ErrorActionPreference = 'Stop'") {
		t.Fatal("missing ErrorActionPreference Stop — failures would be silently swallowed")
	}
}

func TestOpenFileImplOverriddenOnWindows(t *testing.T) {
	got := runtime.FuncForPC(reflect.ValueOf(openFileImpl).Pointer()).Name()
	want := runtime.FuncForPC(reflect.ValueOf(openFileWindows).Pointer()).Name()
	if got != want {
		t.Fatalf("expected openFileImpl to be overridden with openFileWindows on Windows, got %s want %s", got, want)
	}
}
