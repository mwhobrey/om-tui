package signallive

import (
	"context"
	"strings"
	"testing"
)

func TestSignalLinkCommandSkipsUnixScriptWithoutPTY(t *testing.T) {
	orig := signalLinkUsePTY
	signalLinkUsePTY = false
	t.Cleanup(func() { signalLinkUsePTY = orig })

	cmd := signalLinkCommand(context.Background(), `C:\om-tui\signal-cli`)
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "script") {
		t.Fatalf("direct link used script: %q", cmd.Args)
	}
	if !strings.Contains(joined, "link") || !strings.Contains(joined, signalLinkDeviceName) {
		t.Fatalf("args = %q", cmd.Args)
	}
}

func TestSignalLinkCommandUsesScriptWhenPTYRequested(t *testing.T) {
	orig := signalLinkUsePTY
	signalLinkUsePTY = true
	t.Cleanup(func() { signalLinkUsePTY = orig })

	cmd := signalLinkCommand(context.Background(), "/tmp/signal-cli")
	if len(cmd.Args) < 4 || cmd.Args[0] != "script" {
		t.Fatalf("expected script wrapper, got %q", cmd.Args)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "link") || !strings.Contains(joined, "-n "+signalLinkDeviceName) {
		t.Fatalf("args = %q", cmd.Args)
	}
}
