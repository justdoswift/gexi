package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appSettings mirrors the Swift build's interface settings, pared down to
// what the Go UI actually honours today.
type appSettings struct {
	RestoreScreen    bool `json:"restoreScreen"`
	ShowHeaders      bool `json:"showHeaders"`
	SidebarCollapsed bool `json:"sidebarCollapsed"`
}

func defaultSettings() appSettings {
	return appSettings{RestoreScreen: true, ShowHeaders: true}
}

func settingsPath() string { return filepath.Join(supportDir(), "settings.json") }

func loadSettings() appSettings {
	s := defaultSettings()
	if data, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func (s appSettings) save() {
	_ = os.MkdirAll(supportDir(), 0o755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(settingsPath(), data, 0o644)
}

// openSettings shows the settings window, or focuses it when it is open.
func (w *workspace) openSettings() {
	if w.settingsWin != nil {
		w.settingsWin.Show()
		w.settingsWin.Focus()
		return
	}
	w.settingsWin = mygo.NewWindow(mygo.WindowOptions{
		Title:     "Settings",
		Width:     440,
		Height:    230,
		MinWidth:  360,
		MinHeight: 200,
		Content:   ui.View(w.settingsView),
	})
	w.settingsWin.OnClosed(func() { w.settingsWin = nil })
}

func (w *workspace) settingsView(c *ui.Context) {
	tk := themeTokens(c.Theme())
	ui.Column(c).Fill().Padding(22).Gap(14).Background(tk.Background).Children(func() {
		ui.Text(c, "Interface").FontSize(11).FontWeight(500).TextColor(tk.MutedForeground)
		if ui.Checkbox(c, &w.settings.ShowHeaders, "Show pane headers").Changed() {
			w.settings.save()
			w.invalidate()
		}
		ui.Text(c, "Sessions").FontSize(11).FontWeight(500).TextColor(tk.MutedForeground)
		if ui.Checkbox(c, &w.settings.RestoreScreen, "Restore last screen").Changed() {
			w.settings.save()
			w.scheduleSave()
		}
		ui.Spacer(c)
		ui.Textf(c, "Socket  %s", socketPath()).FontSize(10).TextColor(tk.MutedForeground)
	})
}
