package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// shadcn's neutral palette, translated to mygo colours. The chrome uses these
// instead of the toolkit's defaults so every surface, border, and muted text
// reads as one system.
type tokens struct {
	// Dark is whether the palette draws a dark surface. Shadows, tints and
	// the raised card read from it, where the background's red channel once
	// stood in: a theme may be dark with a warm background.
	Dark bool

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

// themeTokens returns the chrome's neutral palette, which is what the app
// draws when no theme is picked.
func themeTokens(t *ui.Theme) tokens { return defaultTokens(t.Dark, t.Accent) }

func defaultTokens(dark bool, accent ui.Color) tokens {
	if dark {
		return tokens{
			Dark: true,
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
			SystemAccent:    accent,
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
		SystemAccent:    accent,
	}
}

const chromeRadius float32 = 6

// themeTokensFor returns the chrome's tokens for one appearance: the neutral
// pair when name is empty, else a ramp derived from that terminal theme.
func themeTokensFor(t *ui.Theme, name string) tokens {
	if name == "" {
		return defaultTokens(t.Dark, t.Accent)
	}
	th, err := terminal.GhosttyTheme(name)
	if err != nil {
		return defaultTokens(t.Dark, t.Accent)
	}
	return tokensForTheme(th)
}

// tokensForTheme derives the chrome from a terminal theme's own two colours:
// the surfaces step off the background, the borders and controls climb toward
// the text, and the status colours come from the theme's ANSI palette, lifted
// until they read on its background. Chrome and terminal then share one
// palette, as Rex's themes do, instead of a theme inside a neutral shell.
func tokensForTheme(th *terminal.Theme) tokens {
	bg, fg := th.Background, th.Foreground
	if bg.A == 0 {
		bg = ui.RGB(0x18, 0x18, 0x1b)
	}
	if fg.A == 0 {
		fg = ui.RGB(0xfa, 0xfa, 0xfa)
	}
	dark := themeIsDark(th)

	accent := th.Cursor
	if accent.A == 0 {
		accent = th.Palette[4]
	}
	if accent.A == 0 {
		accent = fg
	}

	step := func(t float32) ui.Color { return bg.MixOklab(fg, t) }
	recede := func(t float32) ui.Color {
		if dark {
			return bg.MixOklab(ui.RGB(0, 0, 0), t)
		}
		return bg.MixOklab(fg, t)
	}
	mutedText := func() ui.Color {
		c := fg.MixOklab(bg, 0.42)
		for i := 0; i < 8 && contrast(c, bg) < 4; i++ {
			c = c.MixOklab(fg, 0.1)
		}
		return c
	}

	tk := tokens{
		Dark:            dark,
		Background:      bg,
		Foreground:      fg,
		MutedForeground: mutedText(),
		Border:          step(0.10),
		Accent:          step(0.05),
		Active:          step(0.09),
		Control:         step(0.17),
		Raised:          step(0.14),
		Working:         legible(th.Palette[2], bg, fg, 3.2),
		Waiting:         legible(th.Palette[3], bg, fg, 3.2),
		Danger:          legible(th.Palette[1], bg, fg, 3.2),
		SystemAccent:    accent,
	}
	if dark {
		tk.Muted = recede(0.45)
		tk.Sidebar = tk.Muted
	} else {
		tk.Muted = step(0.04)
		tk.Sidebar = step(0.03)
		tk.Raised = bg
	}
	return tk
}

// themeIsDark reports whether a theme paints a dark background.
func themeIsDark(th *terminal.Theme) bool { return luminance(th.Background) < 0.4 }

// luminance is WCAG's relative luminance: how bright a colour reads, 0 for
// black and 1 for white.
func luminance(c ui.Color) float32 {
	lin := func(v uint8) float32 {
		s := float32(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return float32(math.Pow(float64((s+0.055)/1.055), 2.4))
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// contrast is WCAG's contrast ratio: 1 for a colour with itself, 21 at most.
func contrast(a, b ui.Color) float32 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// legible lifts c toward fg until it reads on bg at min contrast: a theme's
// red or green may be too dark for its own background, and a status dot must
// not be.
func legible(c, bg, fg ui.Color, min float32) ui.Color {
	for i := 0; i < 12 && contrast(c, bg) < min; i++ {
		c = c.MixOklab(fg, 0.12)
	}
	return c
}

// windowBackground is the colour behind the page, for the platforms whose
// material shows none.
func windowBackground(light, dark tokens) string {
	return fmt.Sprintf("light-dark(#%02x%02x%02x, #%02x%02x%02x)",
		light.Background.R, light.Background.G, light.Background.B,
		dark.Background.R, dark.Background.G, dark.Background.B)
}

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
	searchHeight float32 = 30
	// groupRowHeight is a project header's line: a label above its
	// sessions, lighter than the sessions themselves.
	groupRowHeight float32 = 28
	rowHeightPx    float32 = 32
	iconSize       float32 = 26
	// iconSmallSize is a list-row control's hit area, beside 12pt text.
	iconSmallSize float32 = 20
)

// canvasInset is the canvas's extra margin to the top, right, and bottom
// window edges in comfortable, on top of the pane cards' own padding, so a
// card sits twice as far from the edge as it does from its neighbour.
const canvasInset float32 = 6

// paneShadow lifts a comfortable pane off the canvas: a soft, low shadow, so
// the card reads as a panel with depth rather than a flat fill. A light window
// takes the lighter one, as stageShadow does.
func paneShadow(tk tokens) ui.Color {
	alpha := float32(0.16)
	if !tk.Dark {
		alpha = 0.07
	}
	return ui.RGB(0, 0, 0).Alpha(alpha)
}

// Motion, as shadcn: short, ease-out, and only on what changes.
const (
	sidebarMotion = 200 * time.Millisecond
	paneMotion    = 240 * time.Millisecond
	focusMotion   = 150 * time.Millisecond
)
