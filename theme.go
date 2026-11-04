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
	Working         ui.Color
	Waiting         ui.Color
}

func themeTokens(t *ui.Theme) tokens {
	if t.Dark {
		return tokens{
			Background:      ui.RGB(0x0a, 0x0a, 0x0a),
			Foreground:      ui.RGB(0xfa, 0xfa, 0xfa),
			Muted:           ui.RGB(0x1f, 0x1f, 0x1f),
			MutedForeground: ui.RGB(0xa1, 0xa1, 0xa1),
			Border:          ui.RGB(0x26, 0x26, 0x26),
			Sidebar:         ui.RGB(0x11, 0x11, 0x11),
			Accent:          ui.RGB(0x1f, 0x1f, 0x1f),
			Active:          ui.RGB(0x26, 0x26, 0x26),
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
		Working:         ui.RGB(0x16, 0xa3, 0x4a),
		Waiting:         ui.RGB(0xd9, 0x77, 0x06),
	}
}

const chromeRadius float32 = 6

const sidebarWidth float32 = 240

const (
	headerHeight float32 = 36
	rowHeightPx  float32 = 30
	iconSize     float32 = 26
)

// Motion, as shadcn: short, ease-out, and only on what changes.
const (
	sidebarMotion = 200 * time.Millisecond
	paneMotion    = 140 * time.Millisecond
)
