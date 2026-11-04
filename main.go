package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Version is what ping and info report.
const Version = "0.1.0-poc"

func main() {
	w := newWorkspace()
	var win *mygo.Window

	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "JustCli · Go POC",
			Width:     1180,
			Height:    760,
			MinWidth:  760,
			MinHeight: 440,
			StateKey:  "poc",
			Content:   ui.View(w.view),
		})
		w.win = win

		// JustCli: closing the window hides it; shells keep running.
		win.OnClose(func(e *mygo.CloseEvent) {
			if w.quitting {
				return
			}
			e.PreventDefault()
			win.Hide()
		})

		mygo.App.SetMenu(mainMenu(w))
		w.restoreOrCreate()
		w.startAgentMonitor()
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

// mainMenu adds the shortcuts the Swift build binds: new tab, split, close
// pane, and moving between panes.
func mainMenu(w *workspace) *mygo.Menu {
	split := func(ax axis) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			p, err := w.resolvePane("")
			if err != nil {
				return
			}
			_, _ = w.splitPane(p, ax, nil, p.dir())
		}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Tab", Accelerator: "CmdOrCtrl+T", Click: func(*mygo.MenuItem, *mygo.Window) { _, _ = w.newTab(nil, "") }},
			mygo.Separator(),
			{Label: "Close Pane", Accelerator: "CmdOrCtrl+W", Click: func(*mygo.MenuItem, *mygo.Window) {
				if p, err := w.resolvePane(""); err == nil {
					w.closePane(p)
				}
			}},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Split Right", Accelerator: "CmdOrCtrl+D", Click: split(axisHorizontal)},
			{Label: "Split Down", Accelerator: "CmdOrCtrl+Shift+D", Click: split(axisVertical)},
			mygo.Separator(),
			{Label: "Previous Pane", Accelerator: "CmdOrCtrl+[", Click: func(*mygo.MenuItem, *mygo.Window) { w.cyclePane(-1) }},
			{Label: "Next Pane", Accelerator: "CmdOrCtrl+]", Click: func(*mygo.MenuItem, *mygo.Window) { w.cyclePane(1) }},
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { w.openSettings() }},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}
