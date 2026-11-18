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
			// what makes a comfortable pane look wrapped. The canvas is a
			// blue-leaning black and the ink a soft white, as leaf's
			// pages are: neither is pure, so the screen reads as paper
			// rather than as pixels.
			Background:      ui.RGB(0x18, 0x18, 0x1b),
			Foreground:      ui.RGB(0xed, 0xed, 0xf0),
			Muted:           ui.RGB(0x11, 0x11, 0x13),
			MutedForeground: ui.RGB(0x9d, 0x9d, 0xa6),
			Border:          ui.RGB(0x26, 0x26, 0x2a),
			Sidebar:         ui.RGB(0x11, 0x11, 0x13),
			Accent:          ui.RGB(0x1c, 0x1c, 0x20),
			Active:          ui.RGB(0x22, 0x22, 0x26),
			Control:         ui.RGB(0x35, 0x35, 0x3b),
			Raised:          ui.RGB(0x2d, 0x2d, 0x32),
			Working:         ui.RGB(0x22, 0xc5, 0x5e),
			Waiting:         ui.RGB(0xf5, 0x9e, 0x0b),
			Danger:          ui.RGB(0xf8, 0x71, 0x71),
			SystemAccent:    accent,
		}
	}
	return tokens{
		// Warm neutrals, as leaf's page: the canvas is paper, the ink holds
		// a little blue, and the hairline between them is the ink at 6%
		// rather than a grey of its own.
		Background:      ui.RGB(0xff, 0xff, 0xff),
		Foreground:      ui.RGB(0x17, 0x17, 0x1a),
		Muted:           ui.RGB(0xf6, 0xf6, 0xf3),
		MutedForeground: ui.RGB(0x5f, 0x5f, 0x66),
		Border:          ui.RGB(0xe8, 0xe8, 0xe5),
		Sidebar:         ui.RGB(0xf6, 0xf6, 0xf3),
		Accent:          ui.RGB(0xf2, 0xf2, 0xef),
		Active:          ui.RGB(0xec, 0xec, 0xe9),
		Control:         ui.RGB(0xdc, 0xdc, 0xd8),
		Raised:          ui.RGB(0xff, 0xff, 0xff),
		Working:         ui.RGB(0x15, 0x80, 0x3d),
		Waiting:         ui.RGB(0xd9, 0x77, 0x06),
		Danger:          ui.RGB(0xdc, 0x26, 0x26),
		SystemAccent:    accent,
	}
}

const chromeRadius float32 = 6

// panePadX and panePadY are the terminal's inset inside a pane card. They are
// larger than the old 12/10: a terminal reads as a page when its text has air
// around it, and the header uses the same left inset so its title lines up
// with the first column below.
const (
	panePadX float32 = 16
	panePadY float32 = 12
)

// groupLabelColor is the quiet tone of a project's name: a section label, not
// the content beneath it. It sits below the 4:1 the content's muted text keeps,
// so the sessions stay the first thing the eye finds, as Rex's groups do.
func groupLabelColor(tk tokens) ui.Color {
	mix := float32(0.30)
	if tk.Dark {
		mix = 0.20
	}
	return tk.MutedForeground.MixOklab(tk.Background, mix)
}

// paneSurface is the pane card's own sheet. On glass it is a sheet of the
// theme's page laid over the window's tint, with a few percent of a warm white
// at its foot: the terminal then sits on a lit sheet, not a flat fill. The
// sheet is 72% over the root's tint, so the effective colour is about 96% of
// the theme and the terminal keeps the contrast its theme was written for;
// the sidebar and the canvas around the card carry more of the glass. With
// glass off the sheet is the opaque theme background.
func paneSurface(tk tokens) (top, bottom ui.Color) {
	top, bottom = tk.Background, tk.Background
	if !glassChrome() {
		return top, bottom
	}
	const alpha = float32(0.72)
	top = top.Alpha(alpha)
	bottom = bottom.Alpha(alpha)
	if !tk.Dark {
		warm := ui.RGB(0xff, 0xf6, 0xe4).Alpha(alpha)
		bottom = bottom.MixOklab(warm, 0.06)
	}
	return top, bottom
}

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
		// A chosen row is a white card, as the neutral pair says: the
		// theme's background alone is too close to the sidebar to lift.
		tk.Raised = bg.MixOklab(ui.RGB(0xff, 0xff, 0xff), 0.55)
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

// compactHeaderTop is the room a compact pane's header leaves above its title.
// A compact pane starts at the window's top edge, so the room a comfortable
// card takes from the canvas and its own inset moves into the header band,
// where it puts the title on the traffic lights' line.
const compactHeaderTop float32 = 12

// glassChrome reports whether the Liquid Glass setting draws the raised
// paper chrome. Off, the chrome keeps the flat, bordered look it had before
// 0.0.103, and the window is opaque too, through mainBackground and liquid.
func glassChrome() bool { return sidebarMaterial }

// raised lifts a card off the surface the way leaf's pages are lifted: a
// hairline in place of a border, then shadows that spread wider and softer
// the further they fall. The card keeps its own fill, so a pane still reads
// as the terminal's colour; only the shadows say it sits above the surface.
// A card whose top edge is its own fill wears lit as well. With glass off it
// is a plain bordered card, as it was before leaf's chrome.
func raised(e ui.Element, tk tokens) ui.Element {
	if !glassChrome() {
		return e.Border(1, tk.Border)
	}
	if tk.Dark {
		return e.
			Shadow(0, 0, 0, 1, ui.RGB(0xff, 0xff, 0xff).Alpha(0.07)).
			Shadow(0, 1, 1, 0, ui.RGB(0, 0, 0).Alpha(0.30)).
			Shadow(0, 3, 6, -2, ui.RGB(0, 0, 0).Alpha(0.35)).
			Shadow(0, 12, 24, -8, ui.RGB(0, 0, 0).Alpha(0.45))
	}
	ink := ui.RGB(0x11, 0x11, 0x1a)
	return e.
		Shadow(0, 0, 0, 1, ink.Alpha(0.06)).
		Shadow(0, 1, 1, 0, ink.Alpha(0.04)).
		Shadow(0, 3, 6, -2, ink.Alpha(0.05)).
		Shadow(0, 12, 24, -8, ink.Alpha(0.08))
}

// lifted is the smaller raise a row or a field takes, as leaf's chosen list
// row does: the same hairline, with the shadows kept close enough for a 32pt
// control to wear without turning into a card.
func lifted(e ui.Element, tk tokens) ui.Element {
	if !glassChrome() {
		return e.Border(1, tk.Border)
	}
	if tk.Dark {
		return e.
			Shadow(0, 0, 0, 1, ui.RGB(0xff, 0xff, 0xff).Alpha(0.07)).
			Shadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.30)).
			Shadow(0, 6, 14, -6, ui.RGB(0, 0, 0).Alpha(0.40))
	}
	ink := ui.RGB(0x11, 0x11, 0x1a)
	return e.
		Shadow(0, 0, 0, 1, ink.Alpha(0.07)).
		Shadow(0, 1, 2, 0, ink.Alpha(0.06)).
		Shadow(0, 6, 14, -6, ink.Alpha(0.11))
}

// lit is the light a raised surface catches along its top edge: one hairline
// of white, so the card reads as facing up. It goes on the element that
// paints the top edge itself, since an opaque child would cover it.
func lit(e ui.Element, tk tokens) ui.Element {
	if !glassChrome() {
		return e
	}
	if tk.Dark {
		return e.InsetShadow(0, 1, 0, 0, ui.RGB(0xff, 0xff, 0xff).Alpha(0.06))
	}
	return e.InsetShadow(0, 1, 0, 0, ui.RGB(0xff, 0xff, 0xff).Alpha(0.90))
}

// badge is a tag in a pane header, as leaf's words in its headline are: the
// card's own surface laid on the header, lit from above and outlined by a
// hairline. The shadow is a contact shadow, a pixel or two deep, rather than
// a card's: a 22pt tag lies on the header, it does not float over it.
// Under the pointer it grows one step, so the tag lifts without moving. The
// step itself eases, so the lift follows the pointer instead of blinking on.
func badge(e ui.Element, tk tokens, hovered bool) ui.Element {
	el := badgeLook(e, tk, hovered)
	return el.Transition(ui.ElementTransition{
		Duration: hoverMotion,
		Ease:     ui.EaseOut,
		Colors:   true,
		Shadows:  true,
	})
}

func badgeLook(e ui.Element, tk tokens, hovered bool) ui.Element {
	if !glassChrome() {
		// The old header chip: one flat fill, a step darker under the
		// pointer, with no gradient or contact shadow.
		fill := tk.Active
		if hovered {
			fill = tk.Control
		}
		return e.Background(fill)
	}
	fill, low := tk.Background, tk.Background
	if tk.Dark {
		// One small step above the card, as leaf's chips sit above its page.
		fill = tk.Background.MixOklab(ui.RGB(0xff, 0xff, 0xff), 0.06)
		low = fill
	}
	low = low.MixOklab(tk.Muted, 0.35)
	e = lit(e.Gradient(fill, low, 180), tk)
	if tk.Dark {
		e = e.Border(1, ui.RGB(0xff, 0xff, 0xff).Alpha(0.08)).
			Shadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.22)).
			Shadow(0, 3, 8, -4, ui.RGB(0, 0, 0).Alpha(0.30))
		if hovered {
			e = e.Shadow(0, 4, 10, -4, ui.RGB(0, 0, 0).Alpha(0.38))
		}
		return e
	}
	// A clean hairline and a short shadow: on paper a white tag has no other
	// edge, and a card's long shadow would read as a smudge under it.
	e = e.Border(1, tk.Border).
		Shadow(0, 1, 2, 0, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.05)).
		Shadow(0, 3, 8, -4, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.07))
	if hovered {
		e = e.Shadow(0, 4, 10, -4, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.10))
	}
	return e
}

// sheet is what a panel sliding over the canvas casts: a shadow on the edge
// it opens from, so it reads as lying above the work rather than beside it.
func sheet(e ui.Element, tk tokens) ui.Element {
	if !glassChrome() {
		return e
	}
	c := ui.RGB(0x11, 0x11, 0x1a).Alpha(0.12)
	if tk.Dark {
		c = ui.RGB(0, 0, 0).Alpha(0.45)
	}
	return e.Shadow(-10, 0, 28, -12, c)
}

// sunken is a well in the surface, as leaf draws its screens and its segment
// track: a hairline inside the edge, and a shadow falling from the top, where
// the surface would step down.
func sunken(e ui.Element, tk tokens) ui.Element {
	if !glassChrome() {
		return e
	}
	if tk.Dark {
		return e.
			InsetShadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.45)).
			InsetShadow(0, 0, 0, 1, ui.RGB(0xff, 0xff, 0xff).Alpha(0.05))
	}
	ink := ui.RGB(0x11, 0x11, 0x1a)
	return e.
		InsetShadow(0, 1, 2, 0, ink.Alpha(0.07)).
		InsetShadow(0, 0, 0, 1, ink.Alpha(0.04))
}

// Motion, as shadcn: short, ease-out, and only on what changes.
const (
	sidebarMotion = 200 * time.Millisecond
	paneMotion    = 240 * time.Millisecond
	focusMotion   = 150 * time.Millisecond
	// hoverMotion is how a tag or a button takes the pointer: a tenth of a
	// second, so the lift follows the hand without lagging behind it.
	hoverMotion = 100 * time.Millisecond
	// chipMotion is how a header control changes form, and rowMotion how a
	// list row arrives or leaves. A 22pt chip and a 34pt settings row do
	// not want the sidebar's 200ms.
	chipMotion = 160 * time.Millisecond
	rowMotion  = 180 * time.Millisecond
	// dialogMotion is a panel's entrance; it leaves a little faster, so a
	// dismissed dialog does not linger over the work it covered.
	dialogMotion     = 180 * time.Millisecond
	dialogExitMotion = 140 * time.Millisecond
)
