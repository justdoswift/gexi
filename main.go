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

		w.restoreOrCreate()
		w.startAgentMonitor()
		stop := startControl(w)
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
			w.saveNow()
			stop()
		})
	})

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
