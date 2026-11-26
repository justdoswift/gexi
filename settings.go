package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// AutoMergePR runs the pipeline in one pass: the pull request goes out
	// with the message and description the pane's agent wrote, without the
	// dialog, and merges as soon as GitHub allows it, with a conflict
	// settled by the agent first. Off, the dialog and the Merge button
	// wait for the user.
	AutoMergePR bool `json:"autoMergePR"`
	// ArchiveAfterMerge clears a merged worktree and its pane away as the
	// pull request merges. Off, the header offers Archive and the user
	// decides when the checkout goes.
	ArchiveAfterMerge bool `json:"archiveAfterMerge"`
	// ThemeLight and ThemeDark are the Ghostty themes picked for each
	// appearance. Empty keeps the neutral chrome and the toolkit's own
	// terminal colours.
	ThemeLight string `json:"themeLight"`
	ThemeDark  string `json:"themeDark"`
	// InterfaceFont is the family the chrome draws in; "" is the system's
	// own, which carries every weight the interface asks for.
	InterfaceFont string `json:"interfaceFont"`
	// TerminalFont is the family panes draw text in; "" is the embedded
	// JetBrains Mono Nerd Font Mono. TerminalFontSize is their text size;
	// 0 is the default 12. TerminalScrollback is how many megabytes of
	// output a pane keeps above its screen; 0 is the default, and panes
	// already open keep the history they were made with.
	TerminalFont       string  `json:"terminalFont"`
	TerminalFontSize   float32 `json:"terminalFontSize"`
	TerminalScrollback int     `json:"terminalScrollback"`

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

// terminalScrollback bounds the megabytes of history a pane keeps, and
// terminalScrollbackDefault is what it keeps when the setting is untouched:
// enough to scroll back through a session, far less than the 10 MB each
// pane held before it was a choice.
const (
	terminalScrollbackMin     = 2
	terminalScrollbackMax     = 16
	terminalScrollbackDefault = 4
)

// terminalScrollbackChoices are the values the picker shows, in order.
var terminalScrollbackChoices = []string{"2", "4", "8", "16"}

// terminalScrollback is the history a pane keeps, in megabytes.
func (s appSettings) terminalScrollback() int {
	if s.TerminalScrollback < terminalScrollbackMin || s.TerminalScrollback > terminalScrollbackMax {
		return terminalScrollbackDefault
	}
	return s.TerminalScrollback
}

// terminalScrollbackBytes is terminalScrollback as the terminal counts it.
func (w *workspace) terminalScrollbackBytes() int {
	return w.settings.terminalScrollback() << 20
}

func defaultSettings() appSettings {
	return appSettings{
		RestoreScreen: true,
		PaneStyle:     paneComfortable,
		Notifications: true,
		AutoMergePR:   true,
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
	// A size from outside the range the picker offers falls back to the
	// default, as a hand-edited file may carry one.
	if s.TerminalFontSize < terminalFontSizeMin || s.TerminalFontSize > terminalFontSizeMax {
		s.TerminalFontSize = 0
	}
	// The same for the history: a size outside the picker's range falls
	// back to the default rather than leaving a pane with a hand-edited one.
	if s.TerminalScrollback < terminalScrollbackMin || s.TerminalScrollback > terminalScrollbackMax {
		s.TerminalScrollback = 0
	}
	// Remote access needs the code clients pair with; a settings file from
	// before the feature, or a hand-edited one, gets a fresh code.
	// A build with a relay compiled in is already on: no settings page
	// turns it on, and no file on the Mac is needed for it.
	applyBuiltinRelay(&s)
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
	settingsArchive
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
	{"folder", locales.SettingsArchive},
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
		// The window carries the same system glass as the main one, so the
		// rail is the desktop seen through the material, as System Settings'
		// sidebar is, instead of a flat grey panel painted inside an opaque
		// window. The form's paper is opaque and covers the material on its
		// side, so only the rail is translucent.
		TitleBarStyle:   mygo.TitleBarHidden,
		Transparent:     true,
		Vibrancy:        mygo.VibrancyGlass,
		VibrancyTint:    w.glassTint(),
		BackgroundColor: w.mainBackground(),
		Content:         ui.View(w.settingsView),
	})
	w.settingsWin.OnShow(func() { w.settingsWin.Invalidate() })
	w.settingsWin.OnClosed(func() {
		w.settingsWin = nil
		w.settingsShown = false
		// A confirmation left pending when the window closes must not
		// greet the next visit.
		w.clearArchivedEntries = nil
		w.archivedDeleteID = ""
	})
}

func (w *workspace) settingsView(c *ui.Context) {
	w.applyInterfaceFont(c)
	tk := w.tokens(c)
	// The rail leaves the window's material bare, as the main window's
	// sidebar does; the form's column is the theme's paper, opaque, so its
	// rows keep their contrast over the glass.
	root := ui.Row(c).Fill()
	if !liquid(c) {
		root = root.Background(tk.Background)
	}
	// The page fades and grows in as the window comes up, as the dialogs on
	// the main window do: the first frame places it a touch small, and the
	// frame after lets it settle into the window.
	pose := ui.Pose{}
	if !w.settingsShown {
		w.settingsShown = true
		pose = ui.Pose{Fade: 1, Scale: 0.985}
		c.After(time.Millisecond)
	}
	root = root.AnimatePose(pose, ui.Animation{Duration: sidebarMotion, Ease: ui.EaseOut})
	root.Children(func() {
		w.settingsSidebar(c, tk)
		// The form's paper meets the rail at a hairline, as System Settings'
		// sidebar does, so the two surfaces read as two panes of one window.
		ui.Column(c).Fill().Background(tk.Background).
			BorderWidth(0, 1, 0, 0).BorderColor(tk.Border).Children(func() {
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
}

// settingsSidebar is the window's left rail: the pages above, the config
// file's door below.
func (w *workspace) settingsSidebar(c *ui.Context, tk tokens) {
	side := ui.Column(c).Width(196).FillHeight().Padding(0, 10, 10, 10)
	if !liquid(c) {
		side = side.Background(tk.Muted)
	}
	side.Children(func() {
		ui.Row(c).Height(headerHeight).DragWindow()
		// One tab stop for the pages: Tab enters the list, the arrows move
		// between the pages, and Enter opens the one it is on.
		ui.Column(c).FillWidth().Gap(1).FocusGroup(ui.Vertical).Children(func() {
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
	// The tab is the main sidebar's row: a 32pt capsule, so the rail's
	// chosen page reads as the same kind of control as a chosen session.
	row := ui.ButtonBase(c).Height(sidebarRowHeight).FillWidth().Padding(0, 8, 0, 8).Gap(8).
		Radius(sidebarRowRadius()).Justify(ui.Start).AlignItems(ui.Center).
		Label(settingsTabs[i].title()).Role(ui.RoleTab).AccessSelected(selected)
	switch {
	case selected:
		color = tk.Foreground
		if liquid(c) {
			row = rowSkin(row, c, tk)
		} else {
			row = row.Background(tk.Background)
		}
	case row.Hovered():
		color = tk.Foreground
		if liquid(c) {
			row = rowSkin(row, c, tk)
		} else {
			row = row.Background(tk.Active)
		}
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
			ui.Image(c, src).Width(15).Height(15)
		}
		ui.Text(c, settingsTabs[i].title()).FontSize(13).FontWeight(300).
			TextColor(color)
		ui.Spacer(c)
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
		ui.Text(c, label).FontSize(12).FontWeight(300).TextColor(color).OnClick(action)
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
	case settingsArchive:
		w.archiveSessionsPage(c, tk)
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
		w.interfaceFontRow(c, tk)
		cardDivider(c, tk)
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsLiquidGlass(),
			detail: locales.SettingsLiquidGlassDetail(),
			on:     w.settings.SidebarMaterial,
			toggle: func() {
				w.settings.SidebarMaterial = !w.settings.SidebarMaterial
				sidebarMaterial = w.settings.SidebarMaterial
				w.settings.save()
				w.updateChrome()
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

	sectionLabel(c, tk, locales.SettingsTerminal())
	settingsCard(c, tk, func() {
		w.terminalFontRow(c, tk)
		cardDivider(c, tk)
		w.terminalFontSizeRow(c, tk)
		cardDivider(c, tk)
		w.terminalScrollbackRow(c, tk)
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
			func() { notifyUser("", locales.TestNotificationTitle(), "gexi", locales.TestNotificationBody()) },
		)
	})
}

// remotePage is Settings › Remote: this Mac's own server, and the Macs it
// reaches.
func (w *workspace) remotePage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsRemoteThisMac())
	settingsCard(c, tk, func() {
		// A build with a relay compiled in is always reachable through it,
		// so the LAN toggle would only be a switch that changes nothing the
		// page above it shows.
		if !w.relayHost {
			w.settingRowView(c, tk, settingRow{
				title:  locales.SettingsRemoteAllow(),
				detail: locales.SettingsRemoteAllowDetail(),
				on:     w.settings.RemoteEnabled,
				toggle: func() { w.setRemoteEnabled(!w.settings.RemoteEnabled) },
			})
			cardDivider(c, tk)
		}
		if !w.remoteServerNeeded() {
			return
		}
		if w.settings.RemoteEnabled || w.relayClientURL() != "" {
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

	// The relay's plumbing stays folded away: a Mac that has one set up
	// only ever needs the code; the rest is setup nobody reads twice.
	relayIncomplete := w.settings.RelayEnabled && !w.settings.relay().ready()
	if !w.relayProvisioned {
		ui.Row(c).Padding(2, 2, 6, 0).Children(func() {
			ui.Text(c, locales.SettingsAdvanced()).FontSize(11).
				TextColor(tk.MutedForeground).Cursor(ui.CursorPointer).
				OnClick(func() {
					w.relayAdvanced = !w.relayAdvanced
					w.invalidateSettings()
				})
		})
	}
	if !w.relayProvisioned && (w.relayAdvanced || relayIncomplete) {
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
	}
}

// remoteAddressRow is where another device reaches this Mac, with the
// addresses in reach and a copy button.
func (w *workspace) remoteAddressRow(c *ui.Context, tk tokens) {
	addresses := lanAddresses()
	// With a relay up, that address is the one another device types: the
	// LAN ones would only work under the same roof, and the local port
	// failing to bind does not change it.
	throughRelay := false
	if relay := w.relayClientURL(); relay != "" {
		addresses = []string{relay}
		throughRelay = true
	}
	detail := strings.Join(addresses, "  ")
	color := tk.MutedForeground
	if w.remoteErr != "" && !throughRelay {
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
		if (w.remoteErr == "" || throughRelay) && len(addresses) > 0 {
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
	// The agents' own themes are a preference, not a path into this build, so
	// a dev build hands the switch on too.
	syncAgentThemes()
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

// settingSelect is the pop-up button a settings row uses, as Rex and System
// Settings draw one on macOS: the chosen value and a small round up/down
// arrow at the row's end, with the app's own glass menu below it rather than
// the toolkit's flat panel.
func settingSelect(c *ui.Context, tk tokens, selected *string, options []string) {
	sel := ui.SelectBase(c, selected)
	accent := c.Preferences().Accent
	if accent.A == 0 {
		accent = tk.SystemAccent
	}
	// A settings row's pop-up is not a bezel: it is the chosen value and a
	// small round arrow at the row's end, as Rex and System Settings draw it.
	trigger := sel.Trigger.Height(26).Padding(0, 2).Gap(8).
		Justify(ui.SpaceBetween).AlignItems(ui.Center)
	trigger.Children(func() {
		ui.Text(c, *selected).FontSize(12.5).TextColor(tk.Foreground).SingleLine()
		circle := ui.Row(c).Width(20).Height(20).Radius(10).Center().
			Background(tk.Foreground.Alpha(0.05))
		if trigger.Hovered() {
			circle = circle.Background(tk.Foreground.Alpha(0.10))
		}
		circle.Children(func() {
			// The two chevrons of a pop-up button, drawn in one box so they
			// share a width, a height and the gap between them; two stacked
			// icons clipped unevenly above and below the mark.
			ui.Box(c).Width(10).Height(10).Draw(func(p *ui.Painter, r ui.Rect) {
				cx, cy := r.X+r.W/2, r.Y+r.H/2
				var up, down ui.Path
				up.MoveTo(cx-3.5, cy-1.4).LineTo(cx, cy-4.4).LineTo(cx+3.5, cy-1.4)
				down.MoveTo(cx-3.5, cy+1.4).LineTo(cx, cy+4.4).LineTo(cx+3.5, cy+1.4)
				p.StrokePath(&up, 1.2, tk.MutedForeground)
				p.StrokePath(&down, 1.2, tk.MutedForeground)
			})
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel = panel.Radius(12).NoShadow().
			Shadow(0, 8, 24, -4, ui.RGB(0, 0, 0).Alpha(0.18))
		if liquid(c) {
			// A menu floats over the page, not over the desktop: the shader
			// glass frosts the settings form behind it, as a native menu
			// frosts the window under it, where the system's own glass would
			// sample what is under the window and come out the colour of the
			// app behind it.
			panel = panel.Border(1, ui.Transparent).Material(noticeGlassLit(tk))
			panel = lit(panel, tk)
		} else {
			panel = panel.Background(tk.Background).Border(1, tk.Border)
		}
		panel = panel.Padding(4)
		panel.Children(func() {
			ui.Scroll(c).MaxHeight(420).FillWidth().Children(func() {
				for _, opt := range options {
					item := sel.Item(opt).Height(24).FillWidth().Padding(0, 8).Radius(7).
						Gap(6).AlignItems(ui.Center)
					color := tk.Foreground
					if item.Highlighted() {
						item = item.Background(accent)
						color = ui.RGB(255, 255, 255)
					}
					mark := ""
					if opt == *selected {
						mark = "✓"
					}
					item.Children(func() {
						ui.Text(c, mark).Width(12).FontSize(11).TextColor(color)
						ui.Text(c, opt).FontSize(12.5).TextColor(color).SingleLine()
					})
				}
			})
		})
	})
}

// interfaceFontRow is the chrome's family: the system's own until the user
// picks another, offered from every family the Mac has, so the sidebar, its
// menus and the settings window can match the rest of the system.
func (w *workspace) interfaceFontRow(c *ui.Context, tk tokens) {
	system := locales.SettingsFontSystem()
	if w.interfaceFontSeen == "" {
		w.interfaceFontChoice = w.settings.InterfaceFont
		if w.interfaceFontChoice == "" {
			w.interfaceFontChoice = system
		}
		w.interfaceFontSeen = w.interfaceFontChoice
	}
	current := w.interfaceFontChoice
	if current == system {
		current = ""
	}
	choices := append([]string{system}, interfaceFontFamilies(current)...)
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Text(c, locales.SettingsInterfaceFont()).FontSize(13).TextColor(tk.Foreground)
		ui.Spacer(c)
		settingSelect(c, tk, &w.interfaceFontChoice, choices)
	})
	if w.interfaceFontChoice == w.interfaceFontSeen {
		return
	}
	w.interfaceFontSeen = w.interfaceFontChoice
	family := w.interfaceFontChoice
	if family == system {
		family = ""
	}
	w.settings.InterfaceFont = family
	w.settings.save()
	w.invalidate()
	w.invalidateSettings()
}

// terminalFontRow is the terminal's family, offered from the monospaced
// fonts the system has plus the embedded one.
func (w *workspace) terminalFontRow(c *ui.Context, tk tokens) {
	if w.terminalFontSeen == "" {
		w.terminalFontChoice = w.settings.TerminalFont
		if w.terminalFontChoice == "" {
			w.terminalFontChoice = terminalFont
		}
		w.terminalFontSeen = w.terminalFontChoice
	}
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Text(c, locales.SettingsTerminalFont()).FontSize(13).TextColor(tk.Foreground)
		ui.Spacer(c)
		settingSelect(c, tk, &w.terminalFontChoice, terminalFontChoices(w.terminalFontChoice))
	})
	if w.terminalFontChoice == w.terminalFontSeen {
		return
	}
	w.terminalFontSeen = w.terminalFontChoice
	family := w.terminalFontChoice
	if family == terminalFont {
		family = ""
	}
	w.settings.TerminalFont = family
	w.settings.save()
	w.applyTerminalFont()
}

// terminalFontSizeRow is the terminal's text size, in the range the picker
// offers.
func (w *workspace) terminalFontSizeRow(c *ui.Context, tk tokens) {
	if w.terminalSizeSeen == "" {
		w.terminalSizeChoice = strconv.Itoa(int(w.terminalFontSize()))
		w.terminalSizeSeen = w.terminalSizeChoice
	}
	ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
		ui.Text(c, locales.SettingsTerminalFontSize()).FontSize(13).TextColor(tk.Foreground)
		ui.Spacer(c)
		settingSelect(c, tk, &w.terminalSizeChoice, terminalFontSizes)
	})
	if w.terminalSizeChoice == w.terminalSizeSeen {
		return
	}
	w.terminalSizeSeen = w.terminalSizeChoice
	size, err := strconv.Atoi(w.terminalSizeChoice)
	if err != nil {
		return
	}
	w.settings.TerminalFontSize = float32(size)
	w.settings.save()
	w.applyTerminalFont()
}

// terminalScrollbackRow picks how much output a pane keeps above its
// screen. The terminals already open hold the history they were made with,
// so the choice lands in the panes opened from then on.
func (w *workspace) terminalScrollbackRow(c *ui.Context, tk tokens) {
	if w.terminalScrollbackSeen == "" {
		w.terminalScrollbackChoice = strconv.Itoa(w.settings.terminalScrollback())
		w.terminalScrollbackSeen = w.terminalScrollbackChoice
	}
	w.settingActionRow(c, tk,
		locales.SettingsTerminalScrollback(),
		locales.SettingsTerminalScrollbackDetail(), func() {
			settingSelect(c, tk, &w.terminalScrollbackChoice, terminalScrollbackChoices)
		})
	if w.terminalScrollbackChoice == w.terminalScrollbackSeen {
		return
	}
	w.terminalScrollbackSeen = w.terminalScrollbackChoice
	mb, err := strconv.Atoi(w.terminalScrollbackChoice)
	if err != nil {
		return
	}
	w.settings.TerminalScrollback = mb
	w.settings.save()
}

// applyTerminalFont hands a new font or size to every terminal on screen,
// so the choice shows at once in the panes that are already open.
func (w *workspace) applyTerminalFont() {
	font := w.terminalTextFont()
	apply := func(p *pane) {
		if p.term != nil {
			p.term.SetFont(font)
		}
		if p.stageMirror != nil && p.stageMirror.sink != nil {
			p.stageMirror.sink.SetFont(font)
		}
	}
	for _, p := range w.allPanes() {
		apply(p)
	}
	for _, p := range w.detached {
		apply(p)
	}
	w.invalidate()
	w.invalidateSettings()
}

type segment struct {
	value string
	label string
}

// segmented is the system's segmented picker, as SwiftUI draws one on macOS:
// a muted track sunk into the panel, with the chosen option lifted on a white
// card.
func (w *workspace) segmented(c *ui.Context, tk tokens, options []segment, selected string, pick func(string)) {
	// The track is one tab stop and its options a radio group: Tab enters
	// at the chosen one, the arrows move between them, and Enter chooses.
	sunken(ui.Row(c).Height(28).Padding(2).Radius(14).Background(settingsTrackFill(tk)).Gap(2).
		FocusGroup(ui.Horizontal), tk).
		Children(func() {
			for _, opt := range options {
				w.segmentButton(c, tk, opt, opt.value == selected, pick)
			}
		})
}

func (w *workspace) segmentButton(c *ui.Context, tk tokens, opt segment, selected bool, pick func(string)) {
	color := tk.MutedForeground
	weight := 400
	el := ui.ButtonBase(c).Height(24).Padding(0, 10, 0, 10).Radius(12).
		Label(opt.label).Role(ui.RoleRadio).AccessSelected(selected)
	if selected {
		color = tk.Foreground
		weight = 500
		// The chosen segment is the one white capsule in the track, as
		// SwiftUI's segmented picker draws it: a short contact shadow and no
		// outline, which would read as a border round a button. In the dark
		// the capsule sits a step above the track rather than below it.
		fill := tk.Raised
		if tk.Dark {
			fill = settingsTrackFill(tk).MixOklab(ui.RGB(0xff, 0xff, 0xff), 0.08)
		}
		el = el.Background(fill).
			Shadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.12))
	}
	choose := func() {
		if selected {
			return
		}
		pick(opt.value)
		w.invalidateSettings()
	}
	el.OnClick(choose).Children(func() {
		ui.Text(c, opt.label).FontSize(12.5).FontWeight(weight).
			TextColor(color)
	})
}

// settingsCardFill is a grouped card's surface: a step off the page, grey on
// paper and lifted in the dark, as System Settings and Rex group their rows.
// The card is defined by that step, not by a drawn edge.
//
// It is a light step, not the control track's grey: Rex's card measures about
// 5% under its page, where tk.Active is closer to 9% on a warm theme and read
// as a deep grey panel. A dark card lifts 8% toward white, so it stays a step
// above the page rather than a block sunk into it.
func settingsCardFill(tk tokens) ui.Color {
	if tk.Dark {
		return tk.Background.MixOklab(ui.RGB(0xff, 0xff, 0xff), 0.08)
	}
	return tk.Background.MixOklab(tk.Foreground, 0.05)
}

// settingsTrackFill is the shallow well a switch or a segmented control sits
// in: a step under the card, not the control track's grey. Rex's track is only
// a few levels below its card; tk.Control on a warm theme is near mid grey and
// read as a grey slab under the white capsule.
func settingsTrackFill(tk tokens) ui.Color {
	card := settingsCardFill(tk)
	if tk.Dark {
		return card.MixOklab(ui.RGB(0xff, 0xff, 0xff), 0.08)
	}
	return card.MixOklab(tk.Foreground, 0.08)
}

// settingsCard is one group of rows: a soft grey panel with a divider between
// each two rows, inset from the panel's sides the way macOS insets them.
//
// It takes neither a border nor the raised shadow stack. System Settings
// groups are surfaces ruled on the page, not cards floating over it; the
// hairline and the shadow under each one read as borders round a border.
func settingsCard(c *ui.Context, tk tokens, rows func()) {
	ui.Column(c).FillWidth().Radius(10).Background(settingsCardFill(tk)).Children(rows)
}

// settingsCardMotion is a settings card whose height follows its rows: a row
// restored, deleted, or cleaned up leaves with a fading copy while the
// remaining rows slide up into the gap, instead of the whole card snapping
// one row shorter.
func settingsCardMotion(c *ui.Context, tk tokens, id string, rows func()) {
	card := ui.Column(c.Key("settings-card:" + id)).FillWidth().Radius(10).
		Clip().Background(settingsCardFill(tk))
	// The card is clipped to its bounds, so the leaving copy does not paint
	// over the section label below it while the height eases down.
	card.Transition(ui.ElementTransition{
		Duration: rowMotion,
		Ease:     ui.EaseOut,
		Enter:    &ui.Motion{},
		Exit:     &ui.Motion{},
		Height:   true,
	})
	card.Children(rows)
}

// settingControlSlotKey is a settings row's control identity: its button and
// its progress line are two forms of one control, so a started action fades
// its press into the spinner instead of swapping them between two frames.
type settingControlSlotKey struct{ id string }

func settingControlSlot(c *ui.Context, id, form string, build func()) {
	el := ui.Row(c.Key("setting-control:" + id + ":" + form)).Shrink(0)
	el.Transition(ui.ElementTransition{
		Duration: chipMotion,
		Ease:     ui.EaseOut,
		Enter:    &ui.Motion{Y: 2},
		Exit:     &ui.Motion{Y: -2},
		Match:    settingControlSlotKey{id},
	})
	el.Children(build)
}

func cardDivider(c *ui.Context, tk tokens) {
	// The rule starts at the rows' text and runs to the card's edge, as the
	// divider of a System Settings group does.
	ui.Row(c).Height(1).Padding(0, 16, 0, 0).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(tk.Foreground.Alpha(0.08))
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
		settingControlSlot(c, title, "action", func() {
			outlineButton(c, tk, label, action)
		})
	})
}

// settingProgressView is settingActionView while its action runs: the button
// gives way to a spinner and the progress it has made, so a batch that takes
// a while says where it is instead of looking stuck.
func (w *workspace) settingProgressView(c *ui.Context, tk tokens, title, detail, progress string) {
	w.settingActionRow(c, tk, title, detail, func() {
		settingControlSlot(c, title, "progress", func() {
			ui.Row(c).Height(28).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
				worktreeSpinner(c, tk, progress)
				ui.Text(c, progress).FontSize(12).TextColor(tk.MutedForeground).
					SingleLine().ContentTransition(ui.ContentTransition{Numeric: true})
			})
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

// outlineButton is the system's push button, sized to sit beside a switch:
// 24pt high, the page's paper with a hairline and a contact shadow, and a
// quiet grey under the pointer. It keeps its size when the row is tight, so
// the label never wraps.
func outlineButton(c *ui.Context, tk tokens, label string, action func()) {
	el := ui.Row(c).Height(24).Shrink(0).Padding(0, 10, 0, 10).Radius(6).
		Border(1, tk.Foreground.Alpha(0.10)).Cursor(ui.CursorPointer).
		Shadow(0, 1, 1, 0, ui.RGB(0, 0, 0).Alpha(0.06))
	fill := tk.Raised
	if tk.Dark {
		fill = tk.Control
	}
	bg := fill
	if el.Hovered() {
		bg = fill.MixOklab(tk.Foreground, 0.06)
	}
	el = el.Background(el.AnimateColor("outlineFill", bg,
		ui.Animation{Duration: hoverMotion, Ease: ui.EaseOut}))
	el.OnClick(action).Children(func() {
		ui.Text(c, label).FontSize(12.5).FontWeight(400).
			TextColor(tk.Foreground).SingleLine().OnClick(action)
	})
}

// switchControl is the system's switch, as SwiftUI draws one on macOS: a
// 38×22 track that is the accent when on and a neutral grey when off, with a
// white thumb that slides over 150ms and carries a contact shadow. The ink
// track it had was a monochrome choice; a macOS switch is the accent, and a
// black one read as a control from another system.
func (w *workspace) switchControl(c *ui.Context, tk tokens, on bool, toggle func()) {
	anim := ui.Animation{Duration: 150 * time.Millisecond}

	track := ui.Row(c).Width(38).Height(22).Radius(11).Padding(2).
		Cursor(ui.CursorPointer)

	trackColor := settingsTrackFill(tk)
	if on {
		accent := c.Preferences().Accent
		if accent.A == 0 {
			accent = tk.SystemAccent
		}
		trackColor = accent
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
		ui.Box(c).Absolute().Left(0).Top(0).Width(38).Height(22).OnClick(toggle)
		if pos > 0 {
			ui.Box(c).Width(pos * 16).Height(1).OnClick(toggle)
		}
		thumb := ui.Box(c).Width(18).Height(18).Radius(9).OnClick(toggle).
			Shadow(0, 1, 2, 0, ui.RGB(0, 0, 0).Alpha(0.18)).
			Shadow(0, 0, 0, 0.5, ui.RGB(0, 0, 0).Alpha(0.06))
		thumb.Background(thumb.AnimateColor("switchThumb", ui.RGB(255, 255, 255), anim))
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
	// The interface font's select names the system's font in the old
	// language; let the row read the choice again.
	w.interfaceFontSeen = ""
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
