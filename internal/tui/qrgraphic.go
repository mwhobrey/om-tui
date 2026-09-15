package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"rsc.io/qr"
)

const (
	qrQuietModules  = 4
	qrSixelModulePx = 6
	qrSkipLine      = "\x1bQRSKIP"
)

func compilePairQR(payload string, kind pairGraphicsKind, maxPx int) (cells string, graphic string, cellW, cellH int, err error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", "", 0, 0, nil
	}
	code, err := qr.Encode(payload, qr.L)
	if err != nil {
		return "", "", 0, 0, err
	}
	cells, cellW, cellH = renderQRCells(code)
	switch kind {
	case pairGfxSixel:
		graphic = encodeQRSixel(code, qrModulePx(code.Size, maxPx))
	case pairGfxKitty:
		graphic = encodeQRKitty(code, qrModulePx(code.Size, maxPx))
	}
	return cells, graphic, cellW, cellH, nil
}

func qrModulePx(size, maxPx int) int {
	n := size + qrQuietModules*2
	if n < 1 {
		n = 1
	}
	if maxPx < 32 {
		maxPx = 180
	}
	px := qrSixelModulePx
	for px > 3 && n*px > maxPx {
		px--
	}
	return px
}

func qrModule(code *qr.Code, quiet, x, y int) bool {
	return code.Black(x-quiet, y-quiet)
}

func renderQRCells(code *qr.Code) (string, int, int) {
	quiet := qrQuietModules
	n := code.Size + quiet*2
	block := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#FFFFFF"))
	var b strings.Builder
	rows := 0
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			top := qrModule(code, quiet, x, y)
			bot := false
			if y+1 < n {
				bot = qrModule(code, quiet, x, y+1)
			}
			var ch string
			switch {
			case top && bot:
				ch = "█"
			case top:
				ch = "▀"
			case bot:
				ch = "▄"
			default:
				ch = " "
			}
			b.WriteString(block.Render(ch))
		}
		b.WriteByte('\n')
		rows++
	}
	return strings.TrimRight(b.String(), "\n"), n, rows
}

func scaledQRImage(code *qr.Code, modulePx int) *image.Paletted {
	if modulePx < 1 {
		modulePx = 4
	}
	quiet := qrQuietModules
	n := code.Size + quiet*2
	dim := n * modulePx
	pal := color.Palette{color.White, color.Black}
	img := image.NewPaletted(image.Rect(0, 0, dim, dim), pal)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if !qrModule(code, quiet, x, y) {
				continue
			}
			x0 := x * modulePx
			y0 := y * modulePx
			for dy := 0; dy < modulePx; dy++ {
				for dx := 0; dx < modulePx; dx++ {
					img.SetColorIndex(x0+dx, y0+dy, 1)
				}
			}
		}
	}
	return img
}

func encodeQRSixel(code *qr.Code, modulePx int) string {
	img := scaledQRImage(code, modulePx)
	payload := encodeSixelBW(img)
	return "\x1b[?2026h" + ansi.SixelGraphics(1, 1, 0, payload) + "\x1b[?2026l"
}

func encodeQRKitty(code *qr.Code, modulePx int) string {
	img := scaledQRImage(code, modulePx)
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return ""
	}
	b64 := make([]byte, base64.StdEncoding.EncodedLen(pngBuf.Len()))
	base64.StdEncoding.Encode(b64, pngBuf.Bytes())
	return ansi.KittyGraphics(b64, "a=T", "f=100", "q=2")
}

func encodeSixelBW(img *image.Paletted) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var b bytes.Buffer
	fmt.Fprintf(&b, `"1;1;%d;%d`, w, h)
	b.WriteString("#0;2;100;100;100#1;2;0;0;0")
	bands := (h + 5) / 6
	for band := 0; band < bands; band++ {
		y0 := band * 6
		b.WriteString("#1")
		var runCh byte
		runN := 0
		flush := func() {
			if runN == 0 {
				return
			}
			if runN >= 3 {
				fmt.Fprintf(&b, "!%d%c", runN, runCh)
			} else {
				for i := 0; i < runN; i++ {
					b.WriteByte(runCh)
				}
			}
			runN = 0
		}
		for x := 0; x < w; x++ {
			var bits byte
			for bit := 0; bit < 6; bit++ {
				y := y0 + bit
				if y < h && img.ColorIndexAt(img.Bounds().Min.X+x, img.Bounds().Min.Y+y) == 1 {
					bits |= 1 << bit
				}
			}
			ch := '?' + bits
			if runN > 0 && ch == runCh {
				runN++
				continue
			}
			flush()
			runCh = ch
			runN = 1
		}
		flush()
		b.WriteByte('-')
	}
	return b.Bytes()
}

func isTerminalGraphicsLine(s string) bool {
	return strings.Contains(s, "\x1bP") || strings.Contains(s, "\x1b_G") || strings.Contains(s, "\x1b[?2026h")
}
