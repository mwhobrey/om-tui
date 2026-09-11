package tui

import (
	"strings"
	"testing"
)

func TestWrapLinesKeepsCookieNames(t *testing.T) {
	s := "Chrome profile Profile 1 is missing SID, HSID, SSID, APISID, SAPISID. Sign into Google in that profile, quit Chrome fully, then retry"
	got := strings.Join(wrapLines(s, 40), " ")
	for _, name := range []string{"SID", "HSID", "SSID", "APISID", "SAPISID"} {
		if !strings.Contains(got, name) {
			t.Fatalf("wrapLines dropped %s:\n%s", name, got)
		}
	}
	if strings.Contains(got, "SI…") {
		t.Fatalf("wrapLines truncated mid-token:\n%s", got)
	}
}
