package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// The saved preference has to reach mygo as the source it understands, and
// anything unrecognised falls back to following the system.
func TestAppearanceSource(t *testing.T) {
	cases := []struct {
		value string
		want  mygo.ThemeSource
	}{
		{"", mygo.ThemeSystem},
		{appearanceLight, mygo.ThemeLight},
		{appearanceDark, mygo.ThemeDark},
		{"sepia", mygo.ThemeSystem},
	}
	for _, c := range cases {
		if got := appearanceSource(c.value); got != c.want {
			t.Errorf("appearanceSource(%q) = %q, want %q", c.value, got, c.want)
		}
	}
}

// A settings file written by hand, or by a newer build, must not leave the
// app in an appearance it cannot honour.
func TestLoadSettingsNormalizesAppearance(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	s := defaultSettings()
	s.Appearance = "sepia"
	s.save()

	if got := loadSettings().Appearance; got != "" {
		t.Errorf("Appearance = %q after reload, want follow-the-system", got)
	}
}
