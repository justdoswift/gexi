package main

import (
	"bytes"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestGlassSwitchChangesTheChrome: the Liquid Glass setting must change what
// the chrome draws, not only the window's background. Off is the flat,
// bordered card; on is the raised paper chrome.
func TestGlassSwitchChangesTheChrome(t *testing.T) {
	old := sidebarMaterial
	defer func() { sidebarMaterial = old }()

	render := func(glass bool) []byte {
		sidebarMaterial = glass
		tester := ui.NewTester(func(c *ui.Context) {
			tk := themeTokens(c.Theme())
			ui.Column(c).Fill().Background(tk.Muted).Padding(24).Children(func() {
				raised(ui.Column(c).FillWidth().Height(60).Background(tk.Background), tk)
				ui.Row(c).Height(8)
				badge(ui.Row(c).Width(60).Height(22).Background(tk.Background), tk, false)
			})
		}, 200, 140)
		tester.Frame()
		return append([]byte(nil), tester.Image().Pix...)
	}

	off, on := render(false), render(true)
	if bytes.Equal(off, on) {
		t.Fatal("the glass switch did not change the chrome")
	}
}

// A side of the pair the user left unset is that appearance's default, not
// the other side repeated: a program asking for the background color has to
// see the colors the window really draws, and the change between light and
// dark is what tells an agent to recolor.
func TestTerminalThemesDefaultEachAppearance(t *testing.T) {
	w := &workspace{}
	w.settings.ThemeLight = "3024 Day"

	light, dark := w.terminalThemes()
	if light == nil || dark == nil {
		t.Fatalf("themes are %v and %v, want one for each appearance", light, dark)
	}
	if themeIsDark(light) {
		t.Fatal("the light theme is dark, want the picked light theme")
	}
	if !themeIsDark(dark) {
		t.Fatal("the dark theme is light, want the default dark theme")
	}
}

// TestFloatingSurfacesStepOffThePaper checks a surface that floats over the
// work is not painted in the same colour as the paper under it. A dialog, the
// checkpoints card, and a header tag all sit on tk.Background; when their fill
// is that same colour they have only a hairline and a shadow to exist by,
// which on warm white reads as a grey outline rather than a raised surface.
func TestFloatingSurfacesStepOffThePaper(t *testing.T) {
	// How far off the paper a floating fill has to sit, in 0-255 levels.
	// The old badge and card fills were 0 levels off in light and 8 in dark.
	const step = 6
	for _, dark := range []bool{false, true} {
		tk := defaultTokens(dark, ui.LightTheme().Accent)
		fill := floatingFill(tk)
		d := int(fill.R) - int(tk.Background.R)
		if d < 0 {
			d = -d
		}
		if d < step {
			t.Fatalf("dark=%v: the floating fill %s is %d levels off the paper %s; it reads as a grey outline",
				dark, colorHex(fill), d, colorHex(tk.Background))
		}
	}
}

// TestDarkWindowKeepsItsOwnColour checks the dark window is the theme's black
// rather than the wallpaper's colour. The tint is what colours the window, so
// at the light appearance's alpha a warm desktop turned #18181b into a
// grey-brown and the whole window took the wallpaper's cast. Composited over
// any desktop, the dark window has to stay close to the colour the theme asks
// for.
func TestDarkWindowKeepsItsOwnColour(t *testing.T) {
	// How far a channel may drift from the theme's own colour, in 0-255.
	// At the old shared alpha a warm desktop moved it by 210.
	const drift = 24
	desktops := [][3]uint8{
		{0xf6, 0xf0, 0xe6}, // warm paper
		{0x3a, 0x3d, 0x42}, // cool grey
		{255, 255, 255},    // white
		{0x1c, 0x1c, 0x1e}, // dark
	}
	tk := defaultTokens(true, ui.LightTheme().Accent)
	a := float64(glassTintAlphaDark) / 255
	for _, d := range desktops {
		for i, base := range []uint8{tk.Background.R, tk.Background.G, tk.Background.B} {
			got := float64(base)*a + float64(d[i])*(1-a)
			diff := int(got) - int(base)
			if diff < 0 {
				diff = -diff
			}
			if diff > drift {
				t.Fatalf("dark window over desktop %v: channel %d drifted %d from the theme's %d",
					d, i, diff, base)
			}
		}
	}
}

// TestHeaderChipsShareOneShape checks the pane header's chips are one control
// at one size. They had drifted to three heights (18, 22, 24), two corners
// (4 and 6) and two hover treatments, so a row of them read as three
// different kinds of thing rather than as one band of tags.
func TestHeaderChipsShareOneShape(t *testing.T) {
	// Every chip is built from these, so a chip that hardcodes its own
	// height or corner fails here rather than in a screenshot.
	if chipHeight != 22 {
		t.Fatalf("chipHeight = %v, want 22", chipHeight)
	}
	if chipRadius != chromeRadius {
		t.Fatalf("chipRadius = %v, want the chrome's %v", chipRadius, chromeRadius)
	}
	// At rest a chip is only its words: no fill, no hairline, no shadow. A
	// row of tags each wearing a surface reads as a wall of boxes over the
	// terminal, which is what the header is meant not to be. The surface
	// arrives under the pointer, and then it does have to step off the
	// header's paper or it is white on white.
	for _, dark := range []bool{false, true} {
		tk := defaultTokens(dark, ui.LightTheme().Accent)
		fill := floatingFill(tk)
		d := int(fill.R) - int(tk.Background.R)
		if d < 0 {
			d = -d
		}
		if d < 6 {
			t.Fatalf("dark=%v: a raised chip's fill %s is %d levels off the header %s",
				dark, colorHex(fill), d, colorHex(tk.Background))
		}
	}
}

// TestHeaderChipsAreQuietAtRest checks a chip at rest is only its words. A
// row of tags each wearing a fill, a hairline and a shadow reads as a wall of
// boxes over the terminal; the header is meant to stay quiet until the
// pointer asks what something is. The surface arrives on hover, and the
// branch pill also raises itself while its drawer is open, which is a state
// the user turned on rather than a pointer passing through.
func TestHeaderChipsAreQuietAtRest(t *testing.T) {
	old := sidebarMaterial
	sidebarMaterial = true
	defer func() { sidebarMaterial = old }()

	for _, dark := range []bool{false, true} {
		tester := ui.NewTester(func(c *ui.Context) {
			tk := themeTokens(c.Theme())
			ui.Column(c).Fill().Background(tk.Background).Padding(20).Children(func() {
				el := ui.Row(c).Height(chipHeight).Radius(chipRadius).Padding(0, chipPadX)
				el = badge(el, tk, false)
				el.Children(func() {
					ui.Text(c, "novelmaker").FontSize(chipFont).TextColor(tk.MutedForeground)
				})
			})
		}, 300, 100)
		tester.SetDark(dark)
		tester.Frame()
		tester.Advance(hoverMotion * 3)
		img := tester.Image()
		// A pixel inside the chip's box but clear of its words: at rest it
		// has to be the header's own colour.
		pr, pg, pb, _ := img.At(24, 30).RGBA()
		cr, cg, cb, _ := img.At(280, 90).RGBA()
		if pr>>8 != cr>>8 || pg>>8 != cg>>8 || pb>>8 != cb>>8 {
			t.Fatalf("dark=%v: a resting chip painted (%d,%d,%d) over a header of (%d,%d,%d)",
				dark, pr>>8, pg>>8, pb>>8, cr>>8, cg>>8, cb>>8)
		}
	}
}
