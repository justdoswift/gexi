package main

import "testing"

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
