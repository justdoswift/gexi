package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// Version is what ping and info report.
const Version = "0.0.38"

func main() {
	// Hooks call back into this same binary (`gexi event turn.start`), so it
	// has to answer that before any window is built.
	if runEventCommand(os.Args[1:]) {
		return
	}

	// Panes start their shells with the interactive shell's PATH; look it up
	// off the UI thread so the first restored pane does not wait on it.
	go interactivePath()

	// Daily background checks in the manner of Sparkle; the menu item below
	// opens the same window on demand.
	mygo.Use(updater.Plugin)

	w := newWorkspace()

	// Start in the appearance the user pinned, so the first frame is right
	// instead of flashing the system's. "" follows the system.
	mygo.Theme.SetSource(appearanceSource(w.settings.Appearance))

	// Panes are built after the window is ready; the font has to be in
	// Core Text before the first terminal measures a cell.
	registerTerminalFont()

	// Restored panes carry the --settings path saved last run; make sure the
	// file is there before any of them try to launch. The user's own settings
	// take the same hooks, so a claude started by hand in a pane reports too.
	_, _ = installClaudeHooks()
	if err := installGlobalClaudeHooks(); err != nil {
		log.Printf("claude hooks: %v", err)
	}

	var win *mygo.Window

	mygo.App.WhenReady(func() {
		// The language the app shows: the one the user picked, or the
		// system's when they follow it.
		mygo.App.SetLocale(w.settings.Language)

		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "gexi",
			Width:     1180,
			Height:    760,
			MinWidth:  760,
			MinHeight: 440,
			StateKey:  "gexi",
			Content:   ui.View(w.view),
			// Rex chrome: no title bar; the view draws under the traffic
			// lights, which the layout leaves room for.
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 20, Y: 17},
			// The window is glass: the sidebar and the canvas leave the
			// surface bare, and macOS draws the system's Liquid Glass under
			// both, over the desktop. The tint is the canvas's own grey, so
			// the glass reads as the app's surface rather than the
			// wallpaper's. Before macOS 26 the same place shows the popover
			// material instead (see mygo.VibrancyGlass).
			Transparent:  true,
			Vibrancy:     mygo.VibrancyGlass,
			VibrancyTint: "light-dark(rgba(245, 245, 245, 0.8), rgba(14, 14, 16, 0.8))",
			// Still the window color on the platforms whose material shows
			// none, where the view paints the same backdrop itself.
			BackgroundColor: "light-dark(#ffffff, #0e0e10)",
		})
		w.win = win
		win.OnShow(func() { win.Invalidate() })

		// gexi: closing the window hides it; shells keep running.
		win.OnClose(func(e *mygo.CloseEvent) {
			if w.quitting {
				return
			}
			e.PreventDefault()
			win.Hide()
		})

		mygo.App.SetMenu(mainMenu(w))
		w.restoreOrCreate()
		w.refreshGit()
		w.startAgentMonitor()
		w.startWorktreeMonitor()
		w.startGitMonitor()
		stop := startControl(w)
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
			w.saveNow()
			stop()
		})
	})

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { w.quitting = true })
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && win != nil {
			win.Show()
			win.Focus()
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// mainMenu adds the app shortcuts: new project, new session, split, close
// pane, and moving between panes.
func mainMenu(w *workspace) *mygo.Menu {
	split := func(ax axis) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			p, err := w.resolvePane("")
			if err != nil {
				return
			}
			_, _ = w.splitPane(p, ax, nil, "")
		}
	}
	// A session needs a project: the one being worked in, or a folder the
	// user picks when there is none.
	newSession := func(*mygo.MenuItem, *mygo.Window) {
		if s := w.selected(); s != nil {
			if pr := w.projectOf(s); pr != nil {
				_, _ = w.newSession(pr, nil, pr.Dir)
				return
			}
		}
		w.pickProject()
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "gexi", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: locales.MenuFile(), Submenu: []*mygo.MenuItem{
			{Label: locales.MenuNewSession(), Accelerator: "CmdOrCtrl+T", Click: newSession},
			{Label: locales.MenuNewProject(), Accelerator: "CmdOrCtrl+Shift+N", Click: func(*mygo.MenuItem, *mygo.Window) { w.pickProject() }},
			mygo.Separator(),
			{Label: locales.MenuClosePane(), Accelerator: "CmdOrCtrl+W", Click: func(*mygo.MenuItem, *mygo.Window) {
				if p, err := w.resolvePane(""); err == nil {
					w.requestCloseByUser(p)
				}
			}},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: locales.MenuView(), Submenu: []*mygo.MenuItem{
			{Label: locales.MenuSplitRight(), Accelerator: "CmdOrCtrl+D", Click: split(axisHorizontal)},
			{Label: locales.MenuSplitDown(), Accelerator: "CmdOrCtrl+Shift+D", Click: split(axisVertical)},
			{Label: locales.MenuZoomPane(), Accelerator: "CmdOrCtrl+Shift+Return", Click: func(*mygo.MenuItem, *mygo.Window) {
				if p, err := w.resolvePane(""); err == nil {
					w.toggleZoom(p)
				}
			}},
			mygo.Separator(),
			{Label: locales.MenuPreviousPane(), Accelerator: "CmdOrCtrl+[", Click: func(*mygo.MenuItem, *mygo.Window) { w.cyclePane(-1) }},
			{Label: locales.MenuNextPane(), Accelerator: "CmdOrCtrl+]", Click: func(*mygo.MenuItem, *mygo.Window) { w.cyclePane(1) }},
			mygo.Separator(),
			{Label: locales.MenuToggleSidebar(), Accelerator: "CmdOrCtrl+B", Click: func(*mygo.MenuItem, *mygo.Window) {
				w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
				w.settings.save()
				w.invalidate()
			}},
			{Label: locales.MenuToggleFiles(), Accelerator: "CmdOrCtrl+Shift+E", Click: func(*mygo.MenuItem, *mygo.Window) {
				w.toggleDrawer()
			}},
			{Label: locales.MenuLanguage(), Submenu: languageMenu(w)},
			{Label: locales.MenuSettings(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { w.openSettings() }},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

// languageMenu lists the languages the app ships, and the system's choice
// above them. Picking one shows it at once and keeps it for the next
// launches; the menu is rebuilt so its labels are in the new language.
func languageMenu(w *workspace) []*mygo.MenuItem {
	item := func(label, tag string) *mygo.MenuItem {
		return &mygo.MenuItem{
			Label:   label,
			Type:    mygo.MenuItemRadio,
			Checked: w.settings.Language == tag,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				w.setLanguage(tag)
			},
		}
	}
	return []*mygo.MenuItem{
		item(locales.MenuLanguageSystem(), ""),
		item("English", "en"),
		item("简体中文", "zh-Hans"),
	}
}
