package tui

import (
	"strings"
	"testing"

	"rsc.io/qr"
)

func TestDetectPairGraphicsFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want pairGraphicsKind
	}{
		{
			name: "override cells beats windows terminal",
			env:  map[string]string{"OPENMESSAGES_TUI_GRAPHICS": "cells", "WT_SESSION": "1"},
			want: pairGfxCells,
		},
		{
			name: "override sixel beats vscode",
			env:  map[string]string{"OPENMESSAGES_TUI_GRAPHICS": "sixel", "TERM_PROGRAM": "vscode"},
			want: pairGfxSixel,
		},
		{
			name: "cursor is cells even with WT_SESSION",
			env:  map[string]string{"TERM_PROGRAM": "vscode", "WT_SESSION": "1"},
			want: pairGfxCells,
		},
		{
			name: "tmux is cells",
			env:  map[string]string{"TMUX": "1", "WT_SESSION": "1"},
			want: pairGfxCells,
		},
		{
			name: "windows terminal sixel",
			env:  map[string]string{"WT_SESSION": "1", "TERM": "xterm-256color"},
			want: pairGfxSixel,
		},
		{
			name: "generic xterm is cells",
			env:  map[string]string{"TERM": "xterm-256color"},
			want: pairGfxCells,
		},
		{
			name: "kitty",
			env:  map[string]string{"KITTY_WINDOW_ID": "1"},
			want: pairGfxKitty,
		},
		{
			name: "ghostty",
			env:  map[string]string{"TERM_PROGRAM": "ghostty"},
			want: pairGfxKitty,
		},
		{
			name: "foot sixel",
			env:  map[string]string{"TERM": "foot"},
			want: pairGfxSixel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPairGraphicsFromEnv(func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestCompilePairQRSixel(t *testing.T) {
	cells, graphic, cellW, cellH, err := compilePairQR("whatsapp-pair-test", pairGfxSixel, 120)
	if err != nil {
		t.Fatal(err)
	}
	if cells == "" || cellW < 8 || cellH < 4 {
		t.Fatalf("cells empty or tiny: w=%d h=%d", cellW, cellH)
	}
	if !strings.Contains(cells, "█") && !strings.Contains(cells, "▀") && !strings.Contains(cells, "▄") {
		t.Fatalf("cells missing half-block glyphs:\n%s", cells)
	}
	if !strings.HasPrefix(graphic, "\x1b[?2026h") {
		t.Fatalf("sixel missing DEC 2026 wrap: %q", graphic[:min(40, len(graphic))])
	}
	if !strings.Contains(graphic, "\x1bP") {
		t.Fatal("sixel missing DCS")
	}
	if !strings.Contains(graphic, "#1") {
		t.Fatal("sixel missing black color")
	}
}

func TestCompilePairQRKitty(t *testing.T) {
	_, graphic, _, _, err := compilePairQR("signal-pair-test", pairGfxKitty, 80)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(graphic, "\x1b_G") {
		t.Fatalf("kitty missing APC: %q", graphic[:min(40, len(graphic))])
	}
	if !strings.Contains(graphic, "a=T") || !strings.Contains(graphic, "f=100") {
		t.Fatalf("kitty missing transmit params: %q", graphic[:min(80, len(graphic))])
	}
}

func TestEncodeSixelBWStartsWithRaster(t *testing.T) {
	code, err := qr.Encode("sixel-raster", qr.L)
	if err != nil {
		t.Fatal(err)
	}
	payload := encodeSixelBW(scaledQRImage(code, 4))
	if !strings.HasPrefix(string(payload), `"1;1;`) {
		t.Fatalf("payload %q", payload[:min(20, len(payload))])
	}
}

func TestOverlayCenterPreservesSixel(t *testing.T) {
	sixel := "\x1b[?2026h\x1bP1;1;0q\"1;1;12;12#1!!!!!!\x1b\\\x1b[?2026l"
	baseLines := make([]string, 10)
	for i := range baseLines {
		baseLines[i] = strings.Repeat("x", 40)
	}
	overlay := strings.Join([]string{
		"Pair WhatsApp",
		sixel,
		qrSkipLine,
		"esc close",
	}, "\n")
	got := overlayCenter(strings.Join(baseLines, "\n"), overlay, 40, 10)
	if !strings.Contains(got, sixel) {
		t.Fatalf("sixel truncated:\n%q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "\x1bP") && strings.Contains(line, "x") &&
			strings.LastIndex(line, "x") > strings.Index(line, "\x1bP") {
			t.Fatalf("base cells painted after sixel: %q", line)
		}
	}
}

func TestOverlayCenterSkipLineLeavesBase(t *testing.T) {
	base := strings.Join([]string{
		strings.Repeat("a", 20),
		strings.Repeat("b", 20),
		strings.Repeat("c", 20),
	}, "\n")
	overlay := "TITLE\n" + qrSkipLine + "\nFOOT"
	got := overlayCenter(base, overlay, 20, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d", len(lines))
	}
	if !strings.Contains(lines[1], "b") {
		t.Fatalf("skip row overwritten: %q", lines[1])
	}
}

func TestRenderLivePairOverlaySixelNotLipglossWrapped(t *testing.T) {
	_, graphic, cellW, cellH, err := compilePairQR("overlay-sixel", pairGfxSixel, 80)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{
		width:  80,
		height: 40,
		pair: pairOverlay{
			open:      true,
			kind:      pairKindWhatsApp,
			qrGraphic: graphic,
			qrCellW:   cellW,
			qrCellH:   cellH,
			qrPayload: "overlay-sixel",
		},
	}
	got := m.renderPairOverlay()
	if !strings.Contains(got, graphic) {
		t.Fatal("overlay dropped sixel payload")
	}
	if !strings.Contains(got, qrSkipLine) {
		t.Fatal("overlay missing skip rows")
	}
}
