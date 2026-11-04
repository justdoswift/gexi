package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appSettings holds the interface settings the UI honours today.
type appSettings struct {
	RestoreScreen    bool   `json:"restoreScreen"`
	SidebarCollapsed bool   `json:"sidebarCollapsed"`
	PaneStyle        string `json:"paneStyle"`
	RemoteTmux       bool   `json:"remoteTmux"`

	// Remote access: either this Mac is reachable through a relay, or it
	// keeps codes imported from other Macs.
	RemoteListen  bool         `json:"remoteListen"`
	RemoteRelay   string       `json:"remoteRelay"`
	RemoteName    string       `json:"remoteName"`
	RemotePort    int          `json:"remotePort"`
	ImportedHosts []remoteHost `json:"importedHosts"`
}

const (
	paneComfortable = "comfortable"
	paneCompact     = "compact"
)

func defaultSettings() appSettings {
	return appSettings{RestoreScreen: true, PaneStyle: paneComfortable, RemoteTmux: true}
}

func settingsPath() string { return filepath.Join(supportDir(), "settings.json") }

func loadSettings() appSettings {
	s := defaultSettings()
	if data, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.PaneStyle != paneCompact {
		s.PaneStyle = paneComfortable
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
		Title:           "Settings",
		Width:           480,
		Height:          440,
		MinWidth:        400,
		MinHeight:       300,
		TitleBarStyle:   mygo.TitleBarHidden,
		BackgroundColor: "light-dark(#ffffff, #0e0e10)",
		Content:         ui.View(w.settingsView),
	})
	w.settingsWin.OnShow(func() { w.settingsWin.Invalidate() })
	w.settingsWin.OnClosed(func() { w.settingsWin = nil })
}

type settingRow struct {
	title    string
	detail   string
	value    *bool
	onChange func()
}

func (w *workspace) settingsView(c *ui.Context) {
	tk := themeTokens(c.Theme())
	bar := c.TitleBar()
	ui.Column(c).Fill().Background(tk.Background).Children(func() {
		ui.Row(c).Height(headerHeight).Padding(0, 12, 0, bar.Left+10).Gap(8).
			DragWindow().Children(func() {
			ui.Text(c, "Settings").FontSize(12).FontWeight(500).TextColor(tk.Foreground)
		})
		ui.Column(c).Fill().Padding(20).Gap(8).Children(func() {
			sectionLabel(c, tk, "Panes")
			ui.Column(c).FillWidth().Radius(8).Border(1, tk.Border).Children(func() {
				ui.Row(c).Padding(14, 16, 14, 16).Children(func() {
					ui.Text(c, "Pane style").FontSize(13).TextColor(tk.Foreground)
					ui.Spacer(c)
					w.paneStyleControl(c, tk)
				})
			})
			sectionLabel(c, tk, "Workspace")
			ui.Column(c).FillWidth().Radius(8).Border(1, tk.Border).Children(func() {
				w.settingRowView(c, tk, settingRow{
					title:    "Restore last screen",
					detail:   "Paint the previous screen above the new shell.",
					value:    &w.settings.RestoreScreen,
					onChange: func() { w.settings.save(); w.scheduleSave() },
				})
			})
			sectionLabel(c, tk, "Remote")
			ui.Column(c).FillWidth().Radius(8).Border(1, tk.Border).Children(func() {
				w.settingRowView(c, tk, settingRow{
					title:    "Keep remote sessions",
					detail:   "Run tmux on the host so a dropped link can reconnect.",
					value:    &w.settings.RemoteTmux,
					onChange: func() { w.settings.save() },
				})
			})
			sectionLabel(c, tk, "Notifications")
			ui.Column(c).FillWidth().Radius(8).Border(1, tk.Border).Children(func() {
				w.settingActionView(c, tk,
					"Desktop notifications",
					"Sent when an agent finishes a turn or waits for input.",
					"Send test",
					func() {
						notifyUser("Test notification", "If this banner shows up, agent events can reach you.")
					},
				)
			})
			ui.Spacer(c)
			ui.Textf(c, "Socket  %s", socketPath()).FontSize(10).TextColor(tk.MutedForeground)
		})
	})
}

func sectionLabel(c *ui.Context, tk tokens, title string) {
	ui.Text(c, title).FontSize(11).FontWeight(500).TextColor(tk.MutedForeground)
}

func (w *workspace) settingRowView(c *ui.Context, tk tokens, row settingRow) {
	ui.Row(c).Padding(14, 16, 14, 16).Gap(16).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, row.title).FontSize(13).TextColor(tk.Foreground)
			ui.Text(c, row.detail).FontSize(11).TextColor(tk.MutedForeground)
		})
		ui.Spacer(c)
		w.switchControl(c, tk, row.value, row.onChange)
	})
}

// settingActionView is a setting row whose control is a button: same
// title/detail pair, no switch to hold.
func (w *workspace) settingActionView(c *ui.Context, tk tokens, title, detail, label string, action func()) {
	ui.Row(c).Padding(14, 16, 14, 16).Gap(16).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).TextColor(tk.Foreground)
			ui.Text(c, detail).FontSize(11).TextColor(tk.MutedForeground)
		})
		ui.Spacer(c)
		outlineButton(c, tk, label, action)
	})
}

// outlineButton is shadcn's outline button, sized to sit beside a switch.
func outlineButton(c *ui.Context, tk tokens, label string, action func()) {
	el := ui.Row(c).Height(28).Padding(0, 10, 0, 10).Radius(6).
		Border(1, tk.Border).Cursor(ui.CursorPointer)
	bg := tk.Background
	if el.Hovered() {
		bg = tk.Accent
	}
	el.Background(bg).OnClick(action).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).
			TextColor(tk.Foreground).OnClick(action)
	})
}

// switchControl is shadcn's switch: a 36×20 track and a 16pt thumb that
// slides over 150ms, colours easing out the same way.
func (w *workspace) switchControl(c *ui.Context, tk tokens, value *bool, onChange func()) {
	on := *value
	anim := ui.Animation{Duration: 150 * time.Millisecond}

	track := ui.Row(c).Width(36).Height(20).Radius(10).Padding(2).
		Cursor(ui.CursorPointer)

	trackColor := tk.Active
	if on {
		trackColor = tk.Foreground
	}
	track = track.Background(track.AnimateColor("track", trackColor, anim))

	var target float32
	if on {
		target = 1
	}
	pos := track.Animate("thumb", target, 150*time.Millisecond)

	toggle := func() {
		*value = !*value
		onChange()
	}
	track.OnClick(toggle).Children(func() {
		// The whole track is the hit area: the thumb alone left the empty
		// half unclickable.
		ui.Box(c).Absolute().Left(0).Top(0).Width(36).Height(20).OnClick(toggle)
		if pos > 0 {
			ui.Box(c).Width(pos * 16).Height(1).OnClick(toggle)
		}
		ui.Box(c).Width(16).Height(16).Radius(8).Background(tk.Background).OnClick(toggle)
	})
}

// paneStyleControl is shadcn's segmented switch: a muted track, a raised
// card on the chosen side.
func (w *workspace) paneStyleControl(c *ui.Context, tk tokens) {
	ui.Row(c).Height(32).Padding(2).Radius(8).Background(tk.Active).Gap(2).Children(func() {
		w.paneStyleSegment(c, tk, paneComfortable, "layout-grid", "Comfortable")
		w.paneStyleSegment(c, tk, paneCompact, "grid-2x2", "Compact")
	})
}

func (w *workspace) paneStyleSegment(c *ui.Context, tk tokens, value, icon, label string) {
	selected := w.settings.PaneStyle == value
	color := tk.MutedForeground
	el := ui.Row(c).Height(28).Padding(0, 10, 0, 10).Gap(6).Radius(6).
		Cursor(ui.CursorPointer)
	if selected {
		color = tk.Foreground
		el = el.Background(tk.Background).Border(1, tk.Border)
	}
	pick := func() {
		if w.settings.PaneStyle == value {
			return
		}
		w.settings.PaneStyle = value
		w.settings.save()
		w.invalidate()
	}
	el.OnClick(pick).Children(func() {
		if src := iconSource(icon, colorHex(color)); src != nil {
			ui.Image(c, src).Width(13).Height(13).OnClick(pick)
		}
		ui.Text(c, label).FontSize(12).TextColor(color).OnClick(pick)
	})
}
