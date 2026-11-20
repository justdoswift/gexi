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
