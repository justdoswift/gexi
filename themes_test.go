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
