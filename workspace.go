package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// tab is one workspace tab: a title and a split tree.
type tab struct {
	ID   string
	Root *splitNode
}

func (t *tab) title() string {
	if t.Root == nil {
		return "Terminal"
	}
	ps := t.Root.panes()
	if len(ps) == 0 {
		return "Terminal"
	}
	p := ps[0]
	if title := strings.TrimSpace(p.term.Title()); title != "" {
		return title
	}
	shell := ""
	if len(p.command) > 0 {
		shell = filepath.Base(p.command[0])
	}
	if shell != "" {
		return shell + " " + abbreviate(p.dir())
	}
	return abbreviate(p.dir())
}

// workspace mirrors JustCli's model: tabs, one selected tab, focus, and the
// control server that drives it all from outside the process.
type workspace struct {
	SessionID   string
	Tabs        []*tab
	SelectedTab string
	FocusedPane string
	focusReq    string

	win         *mygo.Window
	settingsWin *mygo.Window
	settings    appSettings
	quitting    bool
	saveTimer   *time.Timer
}

func newWorkspace() *workspace {
	return &workspace{
		SessionID: newID(),
		settings:  loadSettings(),
	}
}

func (w *workspace) selected() *tab {
	for _, t := range w.Tabs {
		if t.ID == w.SelectedTab {
			return t
		}
	}
	if len(w.Tabs) > 0 {
		return w.Tabs[0]
	}
	return nil
}

func (w *workspace) visiblePanes() []*pane {
	if t := w.selected(); t != nil {
		return t.Root.panes()
	}
	return nil
}

func (w *workspace) allPanes() []*pane {
	var out []*pane
	for _, t := range w.Tabs {
		out = append(out, t.Root.panes()...)
	}
	return out
}

// newTab runs command (or a login shell) and selects it. Main thread.
func (w *workspace) newTab(command []string, dir string) (*tab, error) {
	p, err := newPane(w, "", command, dir, "")
	if err != nil {
		return nil, err
	}
	t := &tab{ID: newID(), Root: leaf(p)}
	w.Tabs = append(w.Tabs, t)
	w.SelectedTab = t.ID
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
	w.scheduleSave()
	return t, nil
}

// splitPane puts a new pane beside p. Main thread.
func (w *workspace) splitPane(p *pane, ax axis, command []string, dir string) (*pane, error) {
	if dir == "" {
		dir = p.dir()
	}
	np, err := newPane(w, "", command, dir, "")
	if err != nil {
		return nil, err
	}
	var host *tab
	for _, t := range w.Tabs {
		if t.Root.paneByID(p.ID) != nil {
			host = t
			break
		}
	}
	if host == nil {
		return nil, fmt.Errorf("No such pane: %s", p.ID)
	}
	host.Root = host.Root.splitting(p.ID, ax, np)
	w.FocusedPane = np.ID
	w.focusReq = np.ID
	w.invalidate()
	w.scheduleSave()
	return np, nil
}

// closePane hangs up the pane's program and collapses its slot. Main thread.
func (w *workspace) closePane(p *pane) {
	p.close()
	for i, t := range w.Tabs {
		if t.Root.paneByID(p.ID) == nil {
			continue
		}
		t.Root = t.Root.removing(p.ID)
		if t.Root == nil {
			w.Tabs = append(w.Tabs[:i], w.Tabs[i+1:]...)
		}
		break
	}
	if w.FocusedPane == p.ID {
		w.FocusedPane = ""
		if t := w.selected(); t != nil {
			if ps := t.Root.panes(); len(ps) > 0 {
				w.FocusedPane = ps[0].ID
				w.focusReq = ps[0].ID
			}
		}
	}
	if w.selected() == nil && len(w.Tabs) > 0 {
		w.SelectedTab = w.Tabs[0].ID
	}
	w.invalidate()
	w.scheduleSave()
}

func (w *workspace) selectTab(id string) {
	for _, t := range w.Tabs {
		if t.ID == id {
			w.SelectedTab = t.ID
			if ps := t.Root.panes(); len(ps) > 0 {
				w.FocusedPane = ps[0].ID
				w.focusReq = ps[0].ID
			}
			w.invalidate()
			w.scheduleSave()
			return
		}
	}
}

func (w *workspace) focusPane(p *pane) {
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	for _, t := range w.Tabs {
		if t.Root.paneByID(p.ID) != nil {
			w.SelectedTab = t.ID
			break
		}
	}
	w.invalidate()
	w.scheduleSave()
}

// resolvePane accepts a full pane id or any unique prefix, as justcli does.
func (w *workspace) resolvePane(raw string) (*pane, error) {
	all := w.allPanes()
	if raw == "" {
		if p := w.paneByID(w.FocusedPane); p != nil {
			return p, nil
		}
		if ps := w.visiblePanes(); len(ps) > 0 {
			return ps[0], nil
		}
		if len(all) > 0 {
			return all[0], nil
		}
		return nil, fmt.Errorf("No pane is available")
	}
	raw = strings.ToLower(raw)
	var matches []*pane
	for _, p := range all {
		if strings.ToLower(p.ID) == raw {
			return p, nil
		}
		if strings.HasPrefix(strings.ToLower(p.ID), raw) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("No such pane: %s", raw)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("Ambiguous pane id: %s", raw)
	}
}

func (w *workspace) paneByID(id string) *pane {
	if id == "" {
		return nil
	}
	for _, p := range w.allPanes() {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (w *workspace) tabOf(p *pane) *tab {
	for _, t := range w.Tabs {
		if t.Root.paneByID(p.ID) != nil {
			return t
		}
	}
	return nil
}

// paneExited closes a pane whose program has ended, as the Swift build does.
func (w *workspace) paneExited(p *pane) {
	if w.quitting || w.win == nil {
		return
	}
	w.onMain(func() {
		if w.quitting || w.paneByID(p.ID) == nil {
			return
		}
		w.closePane(p)
	})
}

// cyclePane moves focus through the selected tab's panes.
func (w *workspace) cyclePane(delta int) {
	panes := w.visiblePanes()
	if len(panes) < 2 {
		return
	}
	index := 0
	for i, p := range panes {
		if p.ID == w.FocusedPane {
			index = i
			break
		}
	}
	next := (index + delta + len(panes)) % len(panes)
	w.focusPane(panes[next])
}

func (w *workspace) invalidate() {
	if w.win != nil {
		w.win.Invalidate()
	}
}

// onMain runs fn on the UI thread and waits, so the control server can read
// and change the model without touching it off-thread.
func (w *workspace) onMain(fn func()) {
	done := make(chan struct{})
	w.win.Update(func() {
		fn()
		close(done)
	})
	<-done
}

func loginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

func shellCommand(raw string) []string {
	return []string{loginShell(), "-lc", raw}
}

func supportDir() string {
	if v := os.Getenv("JUSTCLI_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "JustCliGo")
}

func socketPath() string {
	return filepath.Join(supportDir(), "control.sock")
}

// startAgentMonitor re-checks every pane's foreground group, as the Swift
// build does every couple of seconds. Process inspection is slow, so it runs
// off the UI thread; only the result is applied there.
func (w *workspace) startAgentMonitor() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			type job struct {
				p     *pane
				group int
			}
			var jobs []job
			w.onMain(func() {
				now := time.Now()
				for _, p := range w.allPanes() {
					group := p.foregroundGroup()
					if group <= 0 {
						continue
					}
					if group == p.lastGroup && now.Sub(p.lastScan) < 5*time.Second {
						continue
					}
					p.lastGroup, p.lastScan = group, now
					jobs = append(jobs, job{p, group})
				}
			})
			if len(jobs) == 0 {
				continue
			}
			detections := make([]*agentDetection, len(jobs))
			for i, j := range jobs {
				detections[i] = detectAgent(j.group, j.p.initialDir)
			}
			w.onMain(func() {
				changed := false
				for i, j := range jobs {
					if j.p.applyAgent(detections[i]) {
						changed = true
					}
				}
				if changed {
					w.scheduleSave()
				}
			})
		}
	}()
}
