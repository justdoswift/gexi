package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// appSettings holds the interface settings the UI honours today.
type appSettings struct {
	RestoreScreen    bool   `json:"restoreScreen"`
	SidebarCollapsed bool   `json:"sidebarCollapsed"`
	PaneStyle        string `json:"paneStyle"`
	Notifications    bool   `json:"notifications"`
	// Language is the language the app shows: "" follows the system's
	// languages, "en" and "zh-Hans" pin one.
	Language string `json:"language"`
	// Appearance is the light/dark the app draws: "" follows the system,
	// "light" and "dark" pin one.
	Appearance string `json:"appearance"`
	// SidebarMaterial draws the sidebar in the system's translucent material.
	// Off, the sidebar takes the canvas's colour, wrapping the panes in one
	// surface.
	SidebarMaterial bool `json:"sidebarMaterial"`
	// ArchiveAfterMerge clears a merged worktree and its pane away as the
	// pull request merges. Off, the header offers Archive and the user
	// decides when the checkout goes.
	ArchiveAfterMerge bool `json:"archiveAfterMerge"`
	// ThemeLight and ThemeDark are the Ghostty themes picked for each
	// appearance. Empty keeps the neutral chrome and the toolkit's own
	// terminal colours.
	ThemeLight string `json:"themeLight"`
	ThemeDark  string `json:"themeDark"`

	// Remote access on the local network: whether this Mac can be reached,
	// the six-digit code a client pairs with, the devices paired so far, and
	// the Macs this one reaches.
	RemoteEnabled bool   `json:"remoteEnabled"`
	RemoteCode    string `json:"remoteCode"`
	// RemoteCodeAt is when the pairing code was made. A code is good for
	// pairingCodeTTL, so one left on screen stops working on its own.
	RemoteCodeAt  string         `json:"remoteCodeAt"`
	RemoteDevices []remoteDevice `json:"remoteDevices"`
	RemoteHosts   []remoteHost   `json:"remoteHosts"`

	// The self-hosted relay: a server the user runs, which this Mac keeps a
	// link to so it can be reached without a public address or a router
	// rule. RelayURL is what clients type, shown only so it can be copied.
	RelayEnabled     bool   `json:"relayEnabled"`
	RelayAddress     string `json:"relayAddress"`
	RelayTunnel      string `json:"relayTunnel"`
	RelayToken       string `json:"relayToken"`
	RelayFingerprint string `json:"relayFingerprint"`
	RelayURL         string `json:"relayURL"`
}

const (
	paneComfortable = "comfortable"
	paneCompact     = "compact"

	appearanceLight = "light"
	appearanceDark  = "dark"
)

func defaultSettings() appSettings {
	return appSettings{
		RestoreScreen: true,
		PaneStyle:     paneComfortable,
		Notifications: true,
	}
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
	if s.Appearance != appearanceLight && s.Appearance != appearanceDark {
		s.Appearance = ""
	}
	// A theme that has gone — a deleted user file — falls back to the
	// neutral pair rather than leaving the window without one.
	for _, name := range []*string{&s.ThemeLight, &s.ThemeDark} {
		if *name != "" && !themeExists(*name) {
			*name = ""
		}
	}
	// Remote access needs the code clients pair with; a settings file from
	// before the feature, or a hand-edited one, gets a fresh code.
	if (s.RemoteEnabled || s.RelayEnabled) && len(s.RemoteCode) != 6 {
		s.RemoteCode = newPairingCode()
		s.RemoteCodeAt = time.Now().Format(time.RFC3339)
	}
	sidebarMaterial = s.SidebarMaterial
	return s
}

// appearanceSource turns the saved preference into what mygo wants: ""
// follows the system.
func appearanceSource(value string) mygo.ThemeSource {
	switch value {
	case appearanceLight:
		return mygo.ThemeLight
	case appearanceDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

func (s appSettings) save() {
	_ = os.MkdirAll(supportDir(), 0o755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(settingsPath(), data, 0o600)
	_ = os.Chmod(settingsPath(), 0o600)
}

// The settings window is one page per tab: a symbol and a name in the sidebar,
// a grouped form on the right, as macOS's own settings read.
const (
	settingsGeneral = iota
	settingsThemes
	settingsNotifications
	settingsRemote
	settingsPermissions
	settingsKeyboard
	settingsWorktrees
	settingsAdvanced
)

var settingsTabs = []struct {
	icon  string
	title func() string
}{
	{"setting", locales.SettingsGeneral},
	{"themes", locales.SettingsThemes},
	{"bell", locales.SettingsNotifications},
	{"monitor", locales.SettingsRemote},
	{"file-lock", locales.SettingsPermissions},
	{"keyboard", locales.SettingsKeyboard},
	{"branch", locales.SettingsWorktrees},
	{"sliders", locales.SettingsAdvanced},
}

// openSettings shows the settings window, or focuses it when it is open.
func (w *workspace) openSettings() {
	// The checkouts on disk change outside this window, so each open reads
	// them again rather than trusting the last visit.
	w.worktreesScanned = false
	if w.settingsWin != nil {
		w.settingsWin.Show()
		w.settingsWin.Focus()
		return
	}
	w.settingsWin = mygo.NewWindow(mygo.WindowOptions{
		Title:     "Settings",
		Width:     880,
		Height:    620,
		MinWidth:  720,
		MinHeight: 460,
		// The page fills the window with the theme's own colours, so the
		// window carries no material of its own: a transparent window would
		// show the desktop for the frame before the page's first draw, and
		// the system's material would frost as the window takes the keyboard.
		TitleBarStyle:   mygo.TitleBarHidden,
		BackgroundColor: w.chromeBackground(),
		Content:         ui.View(w.settingsView),
	})
	w.settingsWin.OnShow(func() { w.settingsWin.Invalidate() })
	w.settingsWin.OnClosed(func() {
		w.settingsWin = nil
		// A confirmation left open when the window closes must not greet
		// the next visit.
		w.clearArchivedOpen = false
		w.clearArchivedEntries = nil
	})
}

func (w *workspace) settingsView(c *ui.Context) {
	tk := w.tokens(c)
	// The page takes the theme's own colour: the settings window carries no
	// material, so its cards and rows sit on the theme's surface.
	root := ui.Row(c).Fill().Background(tk.Background)
	root.Children(func() {
		w.settingsSidebar(c, tk)
		ui.Column(c).Fill().Children(func() {
			// The band above the form drags the window, so the whole top of
			// the window moves it, not only the sidebar.
			ui.Row(c).Height(headerHeight).DragWindow()
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(20, 28, 32, 28).Gap(16).Children(func() {
					ui.Text(c, settingsTabs[w.settingsTab].title()).FontSize(17).
						FontWeight(600).TextColor(tk.Foreground)
					w.settingsPage(c, tk)
				})
			})
		})
	})
	// The page's own alerts sit over it, in the same glass frame as the
	// dialogs on the main window.
	if w.clearArchivedOpen {
		w.clearArchivedDialog(c, tk)
	}
}

// settingsSidebar is the window's left rail: the pages above, the config
// file's door below.
func (w *workspace) settingsSidebar(c *ui.Context, tk tokens) {
	side := ui.Column(c).Width(196).FillHeight().Background(tk.Muted).
		Padding(0, 10, 10, 10)
	side.Children(func() {
		ui.Row(c).Height(headerHeight).DragWindow()
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			for i := range settingsTabs {
				w.settingsTabRow(c, tk, i)
			}
		})
		ui.Spacer(c)
		w.sidebarAction(c, tk, locales.SettingsOpenConfig(), func() { openPath(settingsPath()) })
	})
}

func (w *workspace) settingsTabRow(c *ui.Context, tk tokens, i int) {
	selected := w.settingsTab == i
	color := tk.MutedForeground
	row := ui.Row(c).Height(30).FillWidth().Padding(0, 8, 0, 8).Gap(8).
		Radius(chromeRadius).Cursor(ui.CursorPointer)
	switch {
	case selected:
		color = tk.Foreground
		row = row.Background(tk.Background)
	case row.Hovered():
		color = tk.Foreground
		row = row.Background(tk.Active)
	}
	pick := func() {
		if w.settingsTab == i {
			return
		}
		w.settingsTab = i
		w.invalidateSettings()
	}
	row.OnClick(pick).Children(func() {
		if src := iconSource(settingsTabs[i].icon, colorHex(color)); src != nil {
			ui.Image(c, src).Width(15).Height(15).OnClick(pick)
		}
		ui.Text(c, settingsTabs[i].title()).FontSize(13).FontWeight(500).
			TextColor(color).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
	})
}

// sidebarAction is the quiet text button under the pages.
func (w *workspace) sidebarAction(c *ui.Context, tk tokens, label string, action func()) {
	color := tk.MutedForeground
	el := ui.Row(c).Height(28).FillWidth().Padding(0, 8, 0, 8).
		Radius(chromeRadius).Cursor(ui.CursorPointer)
	if el.Hovered() {
		color = tk.Foreground
		el = el.Background(tk.Active)
	}
	el.OnClick(action).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(color).OnClick(action)
	})
}

func (w *workspace) settingsPage(c *ui.Context, tk tokens) {
	switch w.settingsTab {
	case settingsThemes:
		w.themesPage(c, tk)
	case settingsNotifications:
		w.notificationsPage(c, tk)
	case settingsRemote:
		w.remotePage(c, tk)
	case settingsPermissions:
		w.permissionsPage(c, tk)
	case settingsKeyboard:
		w.keyboardPage(c, tk)
	case settingsWorktrees:
		w.worktreesPage(c, tk)
	case settingsAdvanced:
		w.advancedPage(c, tk)
	default:
		w.generalPage(c, tk)
	}
}

func (w *workspace) generalPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsGeneralSessions())
	settingsCard(c, tk, func() {
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsRestoreScreen(),
			detail: locales.SettingsRestoreScreenDetail(),
			on:     w.settings.RestoreScreen,
			toggle: func() {
				w.settings.RestoreScreen = !w.settings.RestoreScreen
				w.settings.save()
				w.scheduleSave()
			},
		})
	})

	sectionLabel(c, tk, locales.SettingsGeneralInterface())
	settingsCard(c, tk, func() {
		w.paneStyleRow(c, tk)
		cardDivider(c, tk)
		w.appearanceRow(c, tk)
		cardDivider(c, tk)
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsLiquidGlass(),
			detail: locales.SettingsLiquidGlassDetail(),
			on:     w.settings.SidebarMaterial,
			toggle: func() {
				w.settings.SidebarMaterial = !w.settings.SidebarMaterial
				sidebarMaterial = w.settings.SidebarMaterial
				w.settings.save()
				w.invalidate()
				w.invalidateSettings()
			},
		})
		cardDivider(c, tk)
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsShowSidebar(),
			detail: locales.SettingsShowSidebarDetail(),
			on:     !w.settings.SidebarCollapsed,
			toggle: func() {
				w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
				w.settings.save()
				w.invalidate()
			},
		})
		cardDivider(c, tk)
		w.languageRow(c, tk)
	})
}

func (w *workspace) notificationsPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsNotifications())
	settingsCard(c, tk, func() {
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsDesktopNotifications(),
			detail: locales.SettingsDesktopNotificationsDetail(),
			on:     w.settings.Notifications,
			toggle: func() {
				w.settings.Notifications = !w.settings.Notifications
				w.settings.save()
			},
		})
		cardDivider(c, tk)
		w.settingActionView(c, tk,
			locales.SettingsNotifyTest(),
			locales.SettingsNotifyTestDetail(),
			locales.SettingsSendTest(),
			func() { notifyUser(locales.TestNotificationTitle(), "gexi", locales.TestNotificationBody()) },
		)
	})
}

// remotePage is Settings › Remote: this Mac's own server, and the Macs it
// reaches.
func (w *workspace) remotePage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsRemoteThisMac())
	settingsCard(c, tk, func() {
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsRemoteAllow(),
			detail: locales.SettingsRemoteAllowDetail(),
			on:     w.settings.RemoteEnabled,
			toggle: func() { w.setRemoteEnabled(!w.settings.RemoteEnabled) },
		})
		if !w.remoteServerNeeded() {
			return
		}
		cardDivider(c, tk)
		if w.settings.RemoteEnabled {
			w.remoteAddressRow(c, tk)
			cardDivider(c, tk)
		}
		cardDivider(c, tk)
		w.remoteCodeRow(c, tk)
		cardDivider(c, tk)
		if len(w.settings.RemoteDevices) == 0 {
			ui.Row(c).Padding(10, 16, 10, 16).Children(func() {
				ui.Text(c, locales.SettingsRemoteNoDevices()).FontSize(12).
					TextColor(tk.MutedForeground)
			})
			return
		}
		for _, device := range w.settings.RemoteDevices {
			id := device.ID
			ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, device.Name).FontSize(13).TextColor(tk.Foreground)
					ui.Text(c, remotePairedLabel(device.PairedAt)).FontSize(11).
						TextColor(tk.MutedForeground)
				})
				ui.Spacer(c)
				outlineButton(c, tk, locales.SettingsRemoteRemove(), func() {
					w.removeRemoteDevice(id)
					w.invalidateSettings()
				})
			})
		}
	})

	sectionLabel(c, tk, locales.SettingsRelaySection())
	settingsCard(c, tk, func() {
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsRelayEnable(),
			detail: locales.SettingsRelayEnableDetail(),
			on:     w.settings.RelayEnabled,
			toggle: func() { w.setRelayEnabled(!w.settings.RelayEnabled) },
		})
		if !w.settings.RelayEnabled {
			return
		}
		cardDivider(c, tk)
		status, color := w.relayStatus(tk)
		ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, status).FontSize(13).TextColor(color)
				detail := w.settings.RelayAddress
				if w.settings.RelayTunnel != "" {
					detail += "  ·  " + w.settings.RelayTunnel
				}
				if w.relayDetail != "" {
					detail += "  ·  " + w.relayDetail
				}
				ui.Text(c, detail).FontSize(11).TextColor(tk.MutedForeground).
					SingleLine()
			})
			ui.Spacer(c)
			outlineButton(c, tk, locales.SettingsRelayEdit(), func() {
				w.openRelayDialog()
			})
		})
		if w.settings.RelayURL != "" {
			cardDivider(c, tk)
			ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, locales.SettingsRelayURLLabel()).FontSize(13).
						TextColor(tk.Foreground)
					ui.Text(c, w.settings.RelayURL).FontSize(11).
						TextColor(tk.MutedForeground).SingleLine()
				})
				ui.Spacer(c)
				outlineButton(c, tk, locales.SettingsRemoteCopy(), func() {
					writeClipboard(w.settings.RelayURL)
				})
			})
		}
	})

	sectionLabel(c, tk, locales.SettingsRemoteHosts())
	settingsCard(c, tk, func() {
		if len(w.settings.RemoteHosts) == 0 {
			ui.Row(c).Padding(10, 16, 10, 16).Children(func() {
				ui.Text(c, locales.SettingsRemoteNoHosts()).FontSize(12).
					TextColor(tk.MutedForeground)
			})
		}
		for _, entry := range w.settings.RemoteHosts {
			host := entry
			ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, host.Name).FontSize(13).TextColor(tk.Foreground)
					ui.Text(c, remoteAddressLabel(host)).FontSize(11).TextColor(tk.MutedForeground)
				})
				ui.Spacer(c)
				outlineButton(c, tk, locales.SettingsRemoteOpen(), func() {
					w.openRemoteHost(host)
				})
				outlineButton(c, tk, locales.SettingsRemoteRemove(), func() {
					w.removeRemoteHost(host.Address)
					w.invalidateSettings()
				})
			})
		}
		cardDivider(c, tk)
		ui.Row(c).Padding(10, 16, 10, 16).Children(func() {
			ui.Text(c, locales.SettingsRemoteAddHost()).FontSize(13).
				TextColor(tk.Foreground)
			ui.Spacer(c)
			outlineButton(c, tk, locales.SettingsRemoteAdd(), func() { w.openHostDialog() })
		})
	})
}

// remoteAddressRow is where another device reaches this Mac, with the
// addresses in reach and a copy button.
func (w *workspace) remoteAddressRow(c *ui.Context, tk tokens) {
	addresses := lanAddresses()
	detail := strings.Join(addresses, "  ")
	color := tk.MutedForeground
	if w.remoteErr != "" {
		detail = locales.SettingsRemoteFailed(w.remoteErr)
		color = tk.Waiting
	} else if len(addresses) == 0 {
		detail = locales.SettingsRemoteNoNetwork()
	}
	ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, locales.SettingsRemoteAddress()).FontSize(13).
				TextColor(tk.Foreground)
			ui.Text(c, detail).FontSize(11).TextColor(color)
		})
		ui.Spacer(c)
		if w.remoteErr == "" && len(addresses) > 0 {
			outlineButton(c, tk, locales.SettingsRemoteCopy(), func() {
				writeClipboard(addresses[0])
			})
		}
	})
}

// remoteCodeRow shows the six digits a client pairs with.
func (w *workspace) remoteCodeRow(c *ui.Context, tk tokens) {
	fresh := codeFresh(w.settings.RemoteCodeAt)
	code := w.settings.RemoteCode
	detail := locales.SettingsRemoteCodeDetail()
	detailColor := tk.MutedForeground
	if fresh {
		detail += " " + locales.SettingsRemoteCodeValid()
	} else {
		// A code left on screen stops working, so the row stops showing
		// digits that would not pair anyone.
		code = "------"
		detail = locales.SettingsRemoteCodeExpired()
		detailColor = tk.Waiting
	}
	ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, locales.SettingsRemoteCode()).FontSize(13).
				TextColor(tk.Foreground)
			ui.Text(c, detail).FontSize(11).TextColor(detailColor)
		})
		ui.Spacer(c)
		ui.Text(c, code).Font("SF Mono, Menlo, monospace").
			FontSize(18).TextColor(tk.Foreground)
		outlineButton(c, tk, locales.SettingsRemoteRegenerate(), func() {
			w.regenerateRemoteCode()
		})
	})
}

func (w *workspace) keyboardPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsShortcuts())
	settingsCard(c, tk, func() {
		rows := [][2]string{
			{locales.ShortcutNewSession(), "⌘T"},
			{locales.ShortcutNewProject(), "⌘⇧N"},
			{locales.ShortcutSplitRight(), "⌘D"},
			{locales.ShortcutSplitDown(), "⌘⇧D"},
			{locales.ShortcutZoomPane(), "⌘⇧↩"},
			{locales.ShortcutPreviousPane(), "⌘["},
			{locales.ShortcutNextPane(), "⌘]"},
			{locales.ShortcutClosePane(), "⌘W"},
			{locales.ShortcutToggleSidebar(), "⌘B"},
			{locales.ShortcutToggleFiles(), "⌘⇧E"},
			{locales.ShortcutSettings(), "⌘,"},
			{locales.ShortcutQuit(), "⌘Q"},
		}
		for i, row := range rows {
			if i > 0 {
				cardDivider(c, tk)
			}
			ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
				ui.Text(c, row[0]).FontSize(13).TextColor(tk.Foreground)
				ui.Spacer(c)
				ui.Text(c, row[1]).FontSize(12).TextColor(tk.MutedForeground)
			})
		}
	})
}

// dismissSettings closes the settings window after a row's action has put a
// pane on stage behind it.
func (w *workspace) dismissSettings() {
	if w.settingsWin != nil {
		w.settingsWin.Close()
	}
}

// confirmDeleteArchived asks before a branch and its checkout go for good.
// The native alert blocks, so it runs off the UI thread and comes back.
func (w *workspace) confirmDeleteArchived(a archivedWorktree) {
	parent := w.settingsWin
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             parent,
			Type:               mygo.MessageWarning,
			Title:              locales.ArchiveDeleteTitle(),
			Message:            locales.ArchiveDeleteBody(a.Branch),
			Buttons:            []string{locales.ArchiveDelete(), locales.Cancel()},
			DefaultButton:      1,
			CancelButton:       1,
			DestructiveButtons: []int{0},
		})
		if err != nil || res.Button != 0 {
			return
		}
		w.onMain(func() { w.deleteArchivedWorktree(a) })
	}()
}

func (w *workspace) advancedPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsStorage())
	settingsCard(c, tk, func() {
		w.pathRow(c, tk, locales.SettingsSocketPath(), socketPath())
		cardDivider(c, tk)
		ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, locales.SettingsDataFolder()).FontSize(13).TextColor(tk.Foreground)
				ui.Text(c, abbreviate(supportDir())).FontSize(11).
					TextColor(tk.MutedForeground).SingleLine().Selectable()
			})
			ui.Spacer(c)
			outlineButton(c, tk, locales.SettingsOpenFolder(), func() { openPath(supportDir()) })
		})
		cardDivider(c, tk)
		ui.Row(c).Padding(10, 16, 10, 16).Children(func() {
			ui.Text(c, locales.SettingsVersion()).FontSize(13).TextColor(tk.Foreground)
			ui.Spacer(c)
			ui.Text(c, Version).FontSize(12).TextColor(tk.MutedForeground).Selectable()
		})
	})
}

// pathRow shows a path the user can copy, with no control beside it.
func (w *workspace) pathRow(c *ui.Context, tk tokens, title, path string) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).TextColor(tk.Foreground)
			ui.Text(c, abbreviate(path)).FontSize(11).TextColor(tk.MutedForeground).
				SingleLine().Selectable()
		})
	})
}

// paneStyleRow is the segmented switch between the two pane layouts.
func (w *workspace) paneStyleRow(c *ui.Context, tk tokens) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Text(c, locales.SettingsPaneStyle()).FontSize(13).TextColor(tk.Foreground)
		ui.Spacer(c)
		w.segmented(c, tk, []segment{
			{value: paneComfortable, label: locales.PaneComfortable()},
			{value: paneCompact, label: locales.PaneCompact()},
		}, w.settings.PaneStyle, func(value string) {
			w.settings.PaneStyle = value
			w.settings.save()
			w.invalidate()
		})
	})
}

// appearanceRow pins the app's light or dark look, or follows the system's.
func (w *workspace) appearanceRow(c *ui.Context, tk tokens) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Text(c, locales.SettingsAppearance()).FontSize(13).TextColor(tk.Foreground)
		ui.Spacer(c)
		w.segmented(c, tk, []segment{
			{value: "", label: locales.AppearanceAutomatic()},
			{value: appearanceLight, label: locales.AppearanceLight()},
			{value: appearanceDark, label: locales.AppearanceDark()},
		}, w.settings.Appearance, w.setAppearance)
	})
}

// setAppearance saves the choice and hands it to mygo, which restyles every
// window — the main one and this settings window — on the spot.
func (w *workspace) setAppearance(value string) {
	w.settings.Appearance = value
	w.settings.save()
	mygo.Theme.SetSource(appearanceSource(value))
}

// languageRow pins the app's language, or leaves it to the system's.
func (w *workspace) languageRow(c *ui.Context, tk tokens) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Text(c, locales.SettingsLanguage()).FontSize(13).TextColor(tk.Foreground)
		ui.Spacer(c)
		w.segmented(c, tk, []segment{
			{value: "", label: locales.MenuLanguageSystem()},
			{value: "en", label: "English"},
			{value: "zh-Hans", label: "简体中文"},
		}, w.settings.Language, w.setLanguage)
	})
}

type segment struct {
	value string
	label string
}

// segmented is shadcn's segmented switch: a muted track, a raised card on the
// chosen side.
func (w *workspace) segmented(c *ui.Context, tk tokens, options []segment, selected string, pick func(string)) {
	ui.Row(c).Height(30).Padding(2).Radius(8).Background(tk.Active).Gap(2).Children(func() {
		for _, opt := range options {
			w.segmentButton(c, tk, opt, opt.value == selected, pick)
		}
	})
}

func (w *workspace) segmentButton(c *ui.Context, tk tokens, opt segment, selected bool, pick func(string)) {
	color := tk.MutedForeground
	el := ui.Row(c).Height(26).Padding(0, 10, 0, 10).Radius(6).
		Cursor(ui.CursorPointer)
	if selected {
		color = tk.Foreground
		el = el.Background(tk.Raised).Border(1, tk.Border)
	}
	choose := func() {
		if selected {
			return
		}
		pick(opt.value)
		w.invalidateSettings()
	}
	el.OnClick(choose).Children(func() {
		ui.Text(c, opt.label).FontSize(12).FontWeight(500).
			TextColor(color).OnClick(choose)
	})
}

// settingsCard is one group of rows: a rounded panel with a divider between
// each two rows, inset from the panel's sides the way macOS insets them.
func settingsCard(c *ui.Context, tk tokens, rows func()) {
	ui.Column(c).FillWidth().Radius(10).Border(1, tk.Border).
		Background(tk.Background).Children(rows)
}

func cardDivider(c *ui.Context, tk tokens) {
	ui.Row(c).Height(1).Padding(0, 16, 0, 16).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(tk.Border)
	})
}

func sectionLabel(c *ui.Context, tk tokens, title string) {
	ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(tk.MutedForeground).
		Padding(2, 2, 0, 2)
}

type settingRow struct {
	title  string
	detail string
	on     bool
	toggle func()
}

func (w *workspace) settingRowView(c *ui.Context, tk tokens, row settingRow) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, row.title).FontSize(13).TextColor(tk.Foreground)
			ui.Text(c, row.detail).FontSize(11).TextColor(tk.MutedForeground)
		})
		ui.Spacer(c)
		w.switchControl(c, tk, row.on, row.toggle)
	})
}

// settingActionView is a setting row whose control is a button: same
// title/detail pair, no switch to hold.
func (w *workspace) settingActionView(c *ui.Context, tk tokens, title, detail, label string, action func()) {
	w.settingActionRow(c, tk, title, detail, func() {
		outlineButton(c, tk, label, action)
	})
}

// settingProgressView is settingActionView while its action runs: the button
// gives way to a spinner and the progress it has made, so a batch that takes
// a while says where it is instead of looking stuck.
func (w *workspace) settingProgressView(c *ui.Context, tk tokens, title, detail, progress string) {
	w.settingActionRow(c, tk, title, detail, func() {
		ui.Row(c).Height(28).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
			worktreeSpinner(c, tk, progress)
			ui.Text(c, progress).FontSize(12).TextColor(tk.MutedForeground).SingleLine()
		})
	})
}

// settingActionRow is the title/detail pair both forms share, with the
// control at the row's right edge.
func (w *workspace) settingActionRow(c *ui.Context, tk tokens, title, detail string, control func()) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).TextColor(tk.Foreground)
			ui.Text(c, detail).FontSize(11).TextColor(tk.MutedForeground)
		})
		ui.Spacer(c)
		control()
	})
}

// outlineButton is shadcn's outline button, sized to sit beside a switch.
// It keeps its size when the row is tight, so the label never wraps.
func outlineButton(c *ui.Context, tk tokens, label string, action func()) {
	el := ui.Row(c).Height(28).Shrink(0).Padding(0, 10, 0, 10).Radius(6).
		Border(1, tk.Border).Cursor(ui.CursorPointer)
	bg := tk.Background
	if el.Hovered() {
		bg = tk.Accent
	}
	el.Background(bg).OnClick(action).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).
			TextColor(tk.Foreground).SingleLine().OnClick(action)
	})
}

// switchControl is shadcn's switch: a 36×20 track and a 16pt thumb that
// slides over 150ms, colours easing out the same way.
func (w *workspace) switchControl(c *ui.Context, tk tokens, on bool, toggle func()) {
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

// setLanguage shows the app in the chosen language at once, and keeps it for
// the next launches. The menu is rebuilt so its own labels follow.
func (w *workspace) setLanguage(tag string) {
	if w.settings.Language == tag {
		return
	}
	w.settings.Language = tag
	w.settings.save()
	mygo.App.SetLocale(tag)
	mygo.App.SetMenu(mainMenu(w))
	w.invalidate()
	w.invalidateSettings()
}

func (w *workspace) invalidateSettings() {
	if w.settingsWin != nil {
		w.settingsWin.Invalidate()
	}
}

// refreshSettings rebuilds the settings page after a background change.
// Unlike Invalidate, it does not coalesce, so a batch that reports every
// entry does not lose its last update while an earlier frame is pending.
func (w *workspace) refreshSettings() {
	if w.settingsWin != nil {
		w.settingsWin.Update(func() {})
	}
}

// openPath hands a file or folder to the Finder, which opens it the way a
// double click would.
func openPath(path string) {
	go func() { _ = exec.Command("open", path).Run() }()
}
