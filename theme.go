package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// shadcn's neutral palette, translated to mygo colours. The chrome uses these
// instead of the toolkit's defaults so every surface, border, and muted text
// reads as one system.
type tokens struct {
	Background      ui.Color
	Foreground      ui.Color
	Muted           ui.Color
	MutedForeground ui.Color
	Border          ui.Color
	Sidebar         ui.Color
	Accent          ui.Color
	Active          ui.Color
	Control         ui.Color
	Working         ui.Color
	Waiting         ui.Color
}

func themeTokens(t *ui.Theme) tokens {
	if t.Dark {
		return tokens{
			// The card background is the terminal's own background
			// (ui.DarkTheme), so a pane header and its body read as one
			// surface; the canvas around it sits a step darker, which is
			// what makes a comfortable pane look wrapped.
			Background:      ui.RGB(0x18, 0x18, 0x1b),
			Foreground:      ui.RGB(0xfa, 0xfa, 0xfa),
			Muted:           ui.RGB(0x0e, 0x0e, 0x10),
			MutedForeground: ui.RGB(0xa1, 0xa1, 0xa1),
			Border:          ui.RGB(0x2a, 0x2a, 0x2e),
			Sidebar:         ui.RGB(0x0e, 0x0e, 0x10),
			Accent:          ui.RGB(0x1f, 0x1f, 0x1f),
			Active:          ui.RGB(0x26, 0x26, 0x26),
			Control:         ui.RGB(0x3a, 0x3a, 0x3a),
			Working:         ui.RGB(0x22, 0xc5, 0x5e),
			Waiting:         ui.RGB(0xf5, 0x9e, 0x0b),
		}
	}
	return tokens{
		Background:      ui.RGB(0xff, 0xff, 0xff),
		Foreground:      ui.RGB(0x0a, 0x0a, 0x0a),
		Muted:           ui.RGB(0xf5, 0xf5, 0xf5),
		MutedForeground: ui.RGB(0x73, 0x73, 0x73),
		Border:          ui.RGB(0xe5, 0xe5, 0xe5),
		Sidebar:         ui.RGB(0xfa, 0xfa, 0xfa),
		Accent:          ui.RGB(0xf5, 0xf5, 0xf5),
		Active:          ui.RGB(0xed, 0xed, 0xed),
		Control:         ui.RGB(0xe0, 0xe0, 0xe0),
		Working:         ui.RGB(0x16, 0xa3, 0x4a),
		Waiting:         ui.RGB(0xd9, 0x77, 0x06),
	}
}

const chromeRadius float32 = 6

// windowRadius is the radius AppKit rounds the window to: a comfortable pane
// card takes the same one, so the card's corners follow the window's.
const windowRadius float32 = 16

const sidebarWidth float32 = 240

const (
	headerHeight float32 = 36
	searchHeight float32 = 28
	rowHeightPx  float32 = 30
	iconSize     float32 = 26
)

// canvasInset is the canvas's extra margin to the top, right, and bottom
// window edges in comfortable, on top of the pane cards' own padding, so a
// card sits twice as far from the edge as it does from its neighbour.
const canvasInset float32 = 3

// Motion, as shadcn: short, ease-out, and only on what changes.
const (
	sidebarMotion = 200 * time.Millisecond
	paneMotion    = 140 * time.Millisecond
	focusMotion   = 150 * time.Millisecond
)
