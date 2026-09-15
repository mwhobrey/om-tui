package tui

import (
	"os"
	"strings"
)

type pairGraphicsKind int

const (
	pairGfxCells pairGraphicsKind = iota
	pairGfxSixel
	pairGfxKitty
)

const tuiGraphicsEnv = "OPENMESSAGES_TUI_GRAPHICS"

func detectPairGraphics() pairGraphicsKind {
	return detectPairGraphicsFromEnv(os.Getenv)
}

func detectPairGraphicsFromEnv(getenv func(string) string) pairGraphicsKind {
	switch strings.ToLower(strings.TrimSpace(getenv(tuiGraphicsEnv))) {
	case "cells", "ascii", "off", "0":
		return pairGfxCells
	case "sixel":
		return pairGfxSixel
	case "kitty":
		return pairGfxKitty
	}

	prog := strings.ToLower(getenv("TERM_PROGRAM"))
	term := strings.ToLower(getenv("TERM"))

	// Cursor / VS Code xterm.js: don't trust inherited WT_SESSION.
	if prog == "vscode" || prog == "cursor" {
		return pairGfxCells
	}
	if getenv("TMUX") != "" {
		return pairGfxCells
	}

	if getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" || prog == "ghostty" || prog == "wezterm" {
		return pairGfxKitty
	}
	if getenv("WT_SESSION") != "" {
		return pairGfxSixel
	}
	// Do not match generic TERM=xterm-256color — almost every emulator sets it.
	if strings.Contains(term, "mlterm") || term == "foot" {
		return pairGfxSixel
	}
	return pairGfxCells
}

func (k pairGraphicsKind) String() string {
	switch k {
	case pairGfxSixel:
		return "sixel"
	case pairGfxKitty:
		return "kitty"
	default:
		return "cells"
	}
}
