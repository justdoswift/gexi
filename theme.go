package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/plugins/glass"
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

// sidebarScrollbar is the bar every scrolling list in the chrome wears: a
// thin thumb in the middle of a narrow track, so it sits on the last few
// DIPs of the list rather than floating in a gutter of its own. The
// toolkit's default track is 12 DIP with a 6 DIP thumb, which on a narrow
// sidebar reads as a thick bar, and even the 6 DIP thumb of the first
// pass read chunky; 4 DIP still meets a 6 DIP hit area, and grows by 2
// while dragged.
var sidebarScrollbar = ui.ScrollbarStyle{TrackWidth: 6, ThumbWidth: 4}

// The metrics every row of the sidebar shares, header or session. They were
// written out at each site and drifted: a header was 28 tall with a gap of 5
// and no hairline, a session row 32 with a gap of 8 and one, so a header and
// the rows under it did not line up.
const (
	// sidebarRowHeight is every row's height, a header's and a session's
	// alike: one column, one rhythm.
	sidebarRowHeight float32 = rowHeightPx
	// sidebarRowGap is the room between a row's mark and its name.
	sidebarRowGap float32 = 8
	// sidebarRowInset is the room at a row's leading edge, before the step
	// its depth adds. A header and a session row share it, so their marks
	// start in one column.
	sidebarRowInset float32 = 8
	// sidebarRowEnd is the room at a row's trailing edge.
	sidebarRowEnd float32 = 8
	// sidebarRowMark is the width of a row's mark: a header's chevron and a
	// session's icon, so the two line up down the column.
	sidebarRowMark float32 = 12
	// sidebarRowFont is the size of every name in the sidebar, a header's
	// and a session's alike.
	sidebarRowFont float32 = 11.5
	// sidebarRowStep is how far a session steps in from the header that
	// names it. It is the mark and the gap, so a session's name starts
	// where the header's name does rather than a few DIPs off.
	sidebarRowStep float32 = sidebarRowMark + sidebarRowGap
)

// sidebarRowRadius is the corner of every row in the sidebar, header or
// session: one rule for the whole column, so a header and a row under it read
// as the same kind of thing.
//
// On glass a row is a capsule, as Rex's chosen row is: the sheet needs the
// softer end to read as a lifted piece of paper rather than a small
// rectangular card. With glass off it keeps the square-ish chrome radius.
func sidebarRowRadius() float32 {
	if glassChrome() {
		return rowHeightPx / 2
	}
	return chromeRadius
}

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

// glassTint is the theme's paper as a tint on the window's Liquid Glass, in
// the syntax mygo.WindowOptions.VibrancyTint takes. The two appearances do not
// share an alpha, because they do not fail the same way. Light at 55% over a
// desktop still reads as paper: white over anything is white. Dark at 55% is
// the desktop's colour, not the theme's: #18181b over a warm wallpaper comes
// out a grey-brown, and the whole window takes the wallpaper's cast. Dark
// therefore sits near opaque, as Rex's does, and keeps only enough translucency
// for the material to light its rim.
func (w *workspace) glassTint() string {
	light, dark := w.appearanceTokens(false), w.appearanceTokens(true)
	return fmt.Sprintf("light-dark(#%02x%02x%02x%02x, #%02x%02x%02x%02x)",
		light.Background.R, light.Background.G, light.Background.B, glassTintAlphaLight,
		dark.Background.R, dark.Background.G, dark.Background.B, glassTintAlphaDark)
}

const (
	// glassTintAlphaLight is the light window's tint: a little over half, the
	// point where the desktop still reads through the frost while the pane
	// cards carry the contrast.
	glassTintAlphaLight uint8 = 0x6d
	// glassTintAlphaDark is the dark window's: near opaque, so the window is
	// the theme's black rather than the wallpaper's colour. At the light
	// value a warm desktop turned the whole window grey-brown.
	glassTintAlphaDark uint8 = 0xf2
)

// paneGlass is the sheet a pane card is made of on the glass chrome.
//
// Native asks for the system's NSGlassEffectView, the same glass the window
// itself carries: it samples what is under the window's surface, so the card
// bends the desktop and the canvas behind it, and its rim is lit by the
// system. The shader glass (Native false) can only read pixels the page has
// already painted, which is why it works in a dialog over the app's own
// chrome and not over an empty window.
func paneGlass(tk tokens) glass.Glass {
	return glass.Glass{
		Native:  true,
		Tint:    tk.Background.Alpha(0.55),
		Clarity: 0.45,
	}
}

// noticeGlass is the material a small floating surface takes: a selected
// session, a header chip, a hovered row. It is the system's own glass, the
// same NSGlassEffectView a notification banner is made of, so it frosts what
// is behind it and lights its own rim instead of being a flat fill with a
// hairline drawn round it.
//
// It is Native on purpose. The shader glass reads pixels the page has already
// painted, and a row in the sidebar sits over a flat surface: there is
// nothing there to blur, so the shader came out identical to the flat fill.
// The system's material samples what is under the window, which is what gives
// a notification its body.
//
// Clarity stays at 0. A pane up to 100 DIPs keeps the measured material
// whatever the clarity, and these surfaces are 22 to 32 DIPs: the frost is
// the whole point at this size, and a clearer setting would only thin the
// rim.
func noticeGlass(tk tokens) glass.Glass {
	return glass.Glass{
		Native:  true,
		Tint:    glassTint(tk).Alpha(0.30),
		Clarity: 0,
	}
}

// glassTint is the colour a glass surface is tinted with: the theme's own
// paper, warmed a step in light mode.
//
// It is derived, not fixed. A notification banner measures a warm white
// (255, 248, 224 at its top, 255, 254, 240 at its middle) rather than a
// neutral one, and frosted neutral white over a light page has no colour of
// its own, which is what read as grey. But the warmth has to come from the
// theme: a fixed #fffbef ignores a theme whose paper is already warm, and
// then the glass is the one surface in the window that does not belong to the
// palette around it. Mixing toward the paper's own hue keeps every theme's
// glass in its own family.
func glassTint(tk tokens) ui.Color {
	if tk.Dark {
		return tk.Background
	}
	// Toward a warm white, by the paper's own lean: a neutral theme lands on
	// the banner's tone, a warm one keeps its warmth.
	warm := ui.RGB(0xff, 0xf8, 0xe4)
	return tk.Background.MixOklab(warm, 0.55)
}

// noticeGlassLit is noticeGlass for a surface that also needs a lit edge.
//
// A native glass is its own layer under the page: the inset shadows that make
// the lit edge paint on the page, so they land on a hole and disappear, which
// is why a native panel's top edge was a hard jump from white to the surface
// with nothing between. The shader glass paints in the page, so the shadows
// land on it and the band shows.
func noticeGlassLit(tk tokens) glass.Glass {
	return glass.Glass{
		Tint:    tk.Background.Alpha(0.62),
		Clarity: 0,
	}
}

// menuPanel is the look of every menu the app draws in the window: a sheet
// of the system's own material, as a notification banner is, rather than the
// theme's flat background with a hairline round it. The default panel is an
// opaque fill, which is what made a menu read as a white board laid over the
// work; the material frosts what is behind it and lights its own rim.
//
// The shadow stays, and is the panel's own: a menu floats well above the
// work, and the system's rim alone does not say how far.
func menuPanel(e ui.Element, c *ui.Context, tk tokens) ui.Element {
	// The theme's own panel is a fill with a hairline round it; both have to
	// go, or the material wears a drawn edge the system's rim already gives.
	e = e.Radius(12).NoShadow().Border(1, ui.Transparent).
		Shadow(0, 8, 24, -4, ui.RGB(0, 0, 0).Alpha(0.12))
	if tk.Dark {
		e = e.Shadow(0, 16, 40, -8, ui.RGB(0, 0, 0).Alpha(0.38))
	}
	if glassChrome() {
		// noticeSkin carries the light along the top edge and the shade
		// along the bottom, which is what gives a notification its body.
		return noticeSkin(e, c, tk)
	}
	return e.Background(tk.Background).Border(1, tk.Border)
}

// menuItemLook is one row of those menus. The row the pointer or the keys are
// on keeps a blue highlight: a menu is the one surface where that is right,
// because it is the system's own menu behaviour and a Mac user reads it
// without thinking. The glass is the panel around it, not the row.
//
// The highlight is the desktop's own accent, not the terminal theme's and not
// the toolkit's fixed #3b82f6: a menu that stayed orange while the Mac was set
// to graphite would be the one control in the window that ignored the setting.
// tk.SystemAccent is the terminal theme's cursor colour, which is where the
// orange came from; c.Preferences().Accent is the one macOS reports.
//
// The row's own background is set before this runs, so it has to be set here
// to take its place.
func menuItemLook(c *ui.Context, e ui.Element, tk tokens) ui.Element {
	e = e.Radius(7).Padding(5, 9)
	if !e.Highlighted() {
		return e
	}
	accent := c.Preferences().Accent
	if accent.A == 0 {
		accent = tk.SystemAccent
	}
	return e.Background(accent).TextColor(ui.RGB(255, 255, 255))
}

// noticeSkin draws a small floating surface over the app's own work: a menu,
// a dialog, a tag on a header. With the glass chrome off it is the flat raised
// card the app had before, so the switch still turns the whole look over at
// once.
//
// It uses the shader, not the system's glass, because it wants a lit edge. A
// native glass paints a hole and the material shows through it, so the inset
// shadows that make the edge land on that hole and vanish; the shader paints
// in the page, so they land on it. That is the whole difference between a
// panel whose top edge is a band of light and one whose top edge is a jump.
func noticeSkin(e ui.Element, c *ui.Context, tk tokens) ui.Element {
	if !liquid(c) {
		return e
	}
	// The shader frosts at the density a control's material has, which over a
	// light page comes out a flat grey: the panel reads as a grey board, not a
	// sheet of glass. The system's own material is what a notification is
	// made of, and it samples what is under the window instead of blurring the
	// page, so it keeps its body. It paints a hole, so the lit edge cannot go
	// on it — the material lights its own rim.
	return e.Material(noticeGlass(tk))
}

// rowSkin draws a row in the sidebar, the chosen one or the one under the
// pointer. It is the system's glass, not the shader: a row sits on the
// sidebar's flat surface, and the shader can only blur pixels the page has
// already painted, so over a flat fill it has nothing to work with and comes
// out as a plain grey block. The system's material samples what is under the
// window, which is what gives a row its body.
//
// It takes no lit edge for the same reason it cannot: the shadows would land
// on the hole the native glass paints. The material lights its own rim.
func rowSkin(e ui.Element, c *ui.Context, tk tokens) ui.Element {
	if !liquid(c) {
		return e
	}
	return e.Material(noticeGlass(tk))
}

// paneSkin draws a pane card. On glass the card is the system's material and
// nothing else: NSGlassEffectView lights its own rim and casts no shadow, as
// AppKit's does not, so a cast shadow here would only smear a grey band under
// a surface that is lit from within. That band is what made a card's bottom
// edge read wrong against its top: the window's glass closes with a lit
// gradient on both sides, and a shadow on one side broke the symmetry.
// Off glass it is raised(), unchanged.
func paneSkin(e ui.Element, c *ui.Context, tk tokens) ui.Element {
	if !liquid(c) {
		return raised(e, tk)
	}
	return e.Material(paneGlass(tk))
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

// The pane header's chips are one control, whatever they name: the title's
// own strip, the checkout, the branch, the subagent count, and the worktree
// button. They had drifted to three heights (18, 22, 24), two corners (4 and
// 6) and two hover treatments, so a row of them read as three different
// kinds of thing. These are the one set they all take; a chip that needs
// more room grows its padding, never its height.
const (
	chipHeight  float32 = 22
	chipRadius  float32 = chromeRadius
	chipPadX    float32 = 7
	chipGap     float32 = 5
	chipFont    float32 = 11
	chipFontNum float32 = 10
)

// paneHeaderRoomy is the width at which a pane header shows its checkout and
// its branch beside the title. Below it the header keeps only the title and
// the buttons the pointer brings: a row of tags in a sliver of a pane reads
// as noise, and the title is what tells one pane from another. The number is
// where a title, a checkout and a branch stop fitting on one line without
// the title being cut.
const paneHeaderRoomy float32 = 260

// tagRoom is the width the header's tags — the reply and the checkout —
// need beside the pane's four buttons before they are worth drawing. They
// give way first: a header that pushes the close button off its own edge has
// kept the wrong thing.
const tagRoom float32 = 96

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
	// rowMarginY is the air above and below a sidebar row. Without it a
	// capsule sits flush against its neighbours and against the list's own
	// edges, so the first and last rows look cut off by the window's border
	// rather than set inside it: Rex's rows each stand clear of the next.
	rowMarginY float32 = 3
	iconSize   float32 = 26
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

// floatingFill is the paper a surface takes when it floats over the work
// rather than sitting in it: a dialog, the checkpoints card, a tag on a
// header. The pane's own paper is tk.Background, so a floating surface of
// that same colour has nothing but its hairline and shadow to exist by, and
// on warm white that reads as a grey outline. The fill steps off the paper
// instead, toward the ink in light and toward white in dark, which is the
// direction each appearance has room to move.
func floatingFill(tk tokens) ui.Color {
	if tk.Dark {
		return tk.Background.MixOklab(ui.RGB(0xff, 0xff, 0xff), 0.07)
	}
	return tk.Background.MixOklab(tk.Foreground, 0.035)
}

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

// lit is the light a raised surface catches along its top edge, so the card
// reads as facing up. It goes on the element that paints the top edge itself,
// since an opaque child would cover it.
//
// It is not a white line. Measured off a notification banner, the top edge
// goes dark first and then climbs to the panel's own tone: the surface turns
// away from the light at its very edge, catches it just inside, and the band
// between is a gradient rather than a rule. A hairline of white at the top is
// the opposite of that, which is why it read as a drawn border.
//
// So: a soft shade on the outside, then the light inside it, both blurred
// enough to be a band and not a line.
func lit(e ui.Element, tk tokens) ui.Element {
	if !glassChrome() {
		return e
	}
	if tk.Dark {
		return e.
			InsetShadow(0, 1, 2, 0, ui.RGB(0, 0, 0, ).Alpha(0.18)).
			InsetShadow(0, 2, 7, 0, ui.RGB(0xff, 0xff, 0xff).Alpha(0.07))
	}
	return e.
		InsetShadow(0, 1, 2, 0, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.10)).
		InsetShadow(0, 2, 7, 0, ui.RGB(0xff, 0xff, 0xff).Alpha(0.55))
}

// badge is a tag in a pane header. At rest it is only its words; the surface
// — the card's own paper, lit from above and outlined by a hairline — arrives
// under the pointer, so the header stays quiet until the pointer asks what
// something is. The shadow is a contact shadow, a pixel or two deep, rather
// than a card's: a 22pt tag lies on the header, it does not float over it.
// The step eases, so the surface follows the pointer instead of blinking on.
func badge(c *ui.Context, e ui.Element, tk tokens, raised bool) ui.Element {
	el := badgeLook(c, e, tk, raised)
	return el.Transition(ui.ElementTransition{
		Duration: hoverMotion,
		Ease:     ui.EaseOut,
		Colors:   true,
		Shadows:  true,
	})
}

// badgeLook is the surface a header chip wears. A chip at rest is only its
// words: a row of tags each wearing a fill, a hairline and a shadow reads as
// a wall of boxes over the terminal, and the header is meant to be quiet
// until the pointer asks what something is. The surface arrives on hover,
// the same sheet the sidebar's chosen row is made of, so a tag under the
// pointer and a chosen row read as the same material at two sizes.
//
// raised is not only hover: the branch pill passes its drawer's open state,
// so a chip that stands for something the user turned on keeps its surface
// without the pointer being there.
func badgeLook(c *ui.Context, e ui.Element, tk tokens, raised bool) ui.Element {
	if !glassChrome() {
		// The old header chip: one flat fill, a step darker under the
		// pointer, with no gradient or contact shadow.
		if !raised {
			return e.Background(ui.Transparent)
		}
		return e.Background(tk.Control)
	}
	// The hairline is reserved whether the surface is there or not: a border
	// takes room inside the chip, so a raised state that dropped it would
	// pull the words a pixel to the left as the pointer arrives, and a chip
	// that moves under the hand is the one thing a hover must not do.
	e = e.Border(1, ui.Transparent)
	if !raised {
		// At rest the chip is words on the header: no fill, no hairline, no
		// shadow.
		return e.Background(ui.Transparent)
	}
	// Under the pointer the tag is the system's own material, as the chosen
	// session row is: it frosts the header behind it and lights its own rim.
	// A flat fill with a hairline drawn round it was the thing that read as
	// a grey box; the material is what a notification banner is made of, and
	// a tag under the pointer is the same kind of small floating surface.
	if liquid(c) {
		return e.Material(noticeGlass(tk))
	}
	// The chosen row's paper, stepped off the header it lies on. The row
	// itself can take tk.Raised because the sidebar sits on the muted paper;
	// the pane header is tk.Background, so a raised fill there is white on
	// white and the tag exists only by its hairline. floatingFill is the
	// same step the dialog and the checkpoints card take.
	fill := floatingFill(tk)
	e = lit(e.Background(fill), tk)
	if tk.Dark {
		return e.Border(1, ui.RGB(0xff, 0xff, 0xff).Alpha(0.10)).
			Shadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.22)).
			Shadow(0, 3, 8, -4, ui.RGB(0, 0, 0).Alpha(0.30)).
			Shadow(0, 4, 10, -4, ui.RGB(0, 0, 0).Alpha(0.38))
	}
	// A clean hairline and a short shadow: on paper a white tag has no other
	// edge, and a card's long shadow would read as a smudge under it.
	return e.Border(1, tk.Border).
		Shadow(0, 1, 2, 0, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.05)).
		Shadow(0, 3, 8, -4, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.07)).
		Shadow(0, 4, 10, -4, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.10))
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

// paneSpring is how a pane arrives and leaves. A split opens a slot that did
// not exist a frame ago, and a close collapses one; a cubic ease makes that
// read as a redraw, while a spring makes the card settle into its slot as
// something with weight. Bounce is small on purpose: a terminal is not a toy,
// and the overshoot only has to be felt.
var paneSpring = ui.Animation{Spring: ui.SpringOptions{
	Duration: 320 * time.Millisecond,
	Bounce:   0.18,
}}
