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
	// Raised is what a chosen segment lifts onto: a white card in light,
	// a step above the track in dark.
	Raised  ui.Color
	Working ui.Color
	Waiting ui.Color
	// Danger marks what is gone or in conflict, as a deleted file.
	Danger ui.Color
	// SystemAccent is the system's accent colour, kept for the one place a
	// hint of colour helps: the chip that names a gexi worktree.
	SystemAccent ui.Color
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
			Raised:          ui.RGB(0x35, 0x35, 0x3a),
			Working:         ui.RGB(0x22, 0xc5, 0x5e),
			Waiting:         ui.RGB(0xf5, 0x9e, 0x0b),
			Danger:          ui.RGB(0xf8, 0x71, 0x71),
			SystemAccent:    t.Accent,
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
		Raised:          ui.RGB(0xff, 0xff, 0xff),
		Working:         ui.RGB(0x16, 0xa3, 0x4a),
		Waiting:         ui.RGB(0xd9, 0x77, 0x06),
		Danger:          ui.RGB(0xdc, 0x26, 0x26),
		SystemAccent:    t.Accent,
	}
}

const chromeRadius float32 = 6

// headerRadius is the pane header's chip corner: a rounded rectangle, the same
// corner as the rest of the chrome, for the W / PR / Merge badges and the
// branch and subagent pills.
const headerRadius float32 = chromeRadius

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
const canvasInset float32 = 6

// paneShadow lifts a comfortable pane off the canvas: a soft, low shadow, so
// the card reads as a panel with depth rather than a flat fill. A light window
// takes the lighter one, as stageShadow does.
func paneShadow(tk tokens) ui.Color {
	alpha := float32(0.4)
	if tk.Background.R > 128 {
		alpha = 0.14
	}
	return ui.RGB(0, 0, 0).Alpha(alpha)
}

// Motion, as shadcn: short, ease-out, and only on what changes.
const (
	sidebarMotion = 200 * time.Millisecond
	paneMotion    = 240 * time.Millisecond
	focusMotion   = 150 * time.Millisecond
)
