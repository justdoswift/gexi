package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestAutoMergePRSetting checks the one-pass pipeline's switch: a fresh
// install starts with it on, turning it off is what the file keeps, and the
// Worktrees page shows the row that turns it.
func TestAutoMergePRSetting(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	if !defaultSettings().AutoMergePR {
		t.Fatal("a fresh install does not open and merge pull requests automatically")
	}
	s := defaultSettings()
	s.save()
	if !loadSettings().AutoMergePR {
		t.Fatal("the automatic merge was lost when the settings file was written")
	}
	s.AutoMergePR = false
	s.save()
	if loadSettings().AutoMergePR {
		t.Fatal("turning the automatic merge off was not kept")
	}

	w := &workspace{settingsTab: settingsWorktrees, settings: defaultSettings()}
	tester := ui.NewTester(w.settingsView, 880, 700)
	tester.Frame()
	if !tester.HasText(locales.SettingsAutoMergePR()) ||
		!tester.HasText(locales.SettingsAutoMergePRDetail()) {
		t.Fatalf("the merging settings have no automatic row; the page shows %q", tester.Texts())
	}
}

// Every settings page has to build without a window: the tabs are only
// reached by hand in the app, so a broken one would close the settings
// window instead of showing itself.
func TestSettingsPagesRender(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	for i, tab := range settingsTabs {
		t.Run(tab.icon, func(t *testing.T) {
			w.settingsTab = i
			tester := ui.NewTester(w.settingsView, 720, 540)
			tester.Frame()
			tester.Frame()
		})
	}
}
