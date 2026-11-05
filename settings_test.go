package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

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
