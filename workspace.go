package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// The workspace is two levels: a project is a folder on disk, and the sessions
// opened in it are the shells and agents working in that folder. Each session
// holds its own split tree of panes.

// project is a folder the user works in.
type project struct {
	ID       string
	Dir      string
	Sessions []*session
}

// session is one split tree of panes inside a project.
type session struct {
	ID   string
	Root *splitNode
}

func (s *session) title() string {
	if s.Root == nil {
		return "Terminal"
	}
	ps := s.Root.panes()
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

// agent returns the detection of the session's first pane, which is the pane
// the session is named after.
func (s *session) agent() *agentDetection {
	if s.Root == nil {
		return nil
	}
	if ps := s.Root.panes(); len(ps) > 0 {
		return ps[0].agent
	}
	return nil
}

// name is the folder's own name, which is what the sidebar calls a project.
func (p *project) name() string {
	if base := filepath.Base(p.Dir); base != "" && base != "." && base != "/" {
		return base
	}
	return p.Dir
}

// workspace mirrors Gexi's model: projects, the session each holds, focus,
// and the control server that drives it all from outside the process.
type workspace struct {
	InstanceID      string
	Projects        []*project
	SelectedSession string
	FocusedPane     string
	focusReq        string

	win         *mygo.Window
	settingsWin *mygo.Window
	settings    appSettings
	quitting    bool
	saveTimer   *time.Timer
	lastNotify  map[string]time.Time

	collapsed   map[string]bool
	searchOpen  bool
	searchQuery string
}

func newWorkspace() *workspace {
	return &workspace{
		InstanceID: newID(),
		settings:   loadSettings(),
		collapsed:  map[string]bool{},
	}
}

func (w *workspace) selected() *session {
	if s := w.sessionByID(w.SelectedSession); s != nil {
		return s
	}
	for _, pr := range w.Projects {
		if len(pr.Sessions) > 0 {
			return pr.Sessions[0]
		}
	}
	return nil
}

func (w *workspace) sessionByID(id string) *session {
	for _, pr := range w.Projects {
		for _, s := range pr.Sessions {
			if s.ID == id {
				return s
			}
		}
	}
	return nil
}

func (w *workspace) projectByID(id string) *project {
	for _, pr := range w.Projects {
		if pr.ID == id {
			return pr
		}
	}
	return nil
}

func (w *workspace) projectOf(s *session) *project {
	for _, pr := range w.Projects {
		for _, have := range pr.Sessions {
			if have == s {
				return pr
			}
		}
	}
	return nil
}

func (w *workspace) sessionOf(p *pane) *session {
	for _, pr := range w.Projects {
		for _, s := range pr.Sessions {
			if s.Root != nil && s.Root.paneByID(p.ID) != nil {
				return s
			}
		}
	}
	return nil
}

func (w *workspace) visiblePanes() []*pane {
	if s := w.selected(); s != nil && s.Root != nil {
		return s.Root.panes()
	}
	return nil
}

func (w *workspace) allPanes() []*pane {
	var out []*pane
	for _, pr := range w.Projects {
		for _, s := range pr.Sessions {
			if s.Root != nil {
				out = append(out, s.Root.panes()...)
			}
		}
	}
	return out
}

// openProject picks dir up as a project, reusing the one already there, and
// starts a session in it. Main thread.
func (w *workspace) openProject(dir string) (*project, *session, error) {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = home
	}
	dir = filepath.Clean(dir)
	pr := w.projectFor(dir)
	s, err := w.newSession(pr, nil, dir)
	if err != nil {
		return pr, nil, err
	}
	return pr, s, nil
}

// pickProject asks for a folder and opens it as a project. The native panel
// blocks, so it runs off the UI thread and comes back to it.
func (w *workspace) pickProject() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            w.win,
			Title:             "New Project",
			Message:           "Choose the folder this project works in.",
			ButtonLabel:       "Open",
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		w.onMain(func() { _, _, _ = w.openProject(paths[0]) })
	}()
}

// projectFor returns the project for dir, adding one when there is none.
// Main thread.
func (w *workspace) projectFor(dir string) *project {
	for _, pr := range w.Projects {
		if pr.Dir == dir {
			return pr
		}
	}
	pr := &project{ID: newID(), Dir: dir}
	w.Projects = append(w.Projects, pr)
	delete(w.collapsed, pr.ID)
	return pr
}

// newSession runs command (or a login shell) in the project and selects it.
// Main thread.
func (w *workspace) newSession(pr *project, command []string, dir string) (*session, error) {
	if pr == nil {
		return nil, fmt.Errorf("No project is open")
	}
	if dir == "" {
		dir = pr.Dir
	}
	p, err := newPane(w, "", command, dir, "")
	if err != nil {
		return nil, err
	}
	s := &session{ID: newID(), Root: leaf(p)}
	pr.Sessions = append(pr.Sessions, s)
	w.SelectedSession = s.ID
	delete(w.collapsed, pr.ID)
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
	w.scheduleSave()
	return s, nil
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
	host := w.sessionOf(p)
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

// closeSession closes every pane under the session; the last closePane
// removes the session itself. Panes fade out first, as closing one from its
// header does. Main thread.
func (w *workspace) closeSession(s *session) {
	if s == nil || s.Root == nil {
		return
	}
	panes := s.Root.panes()
	if len(panes) == 0 {
		return
	}
	now := time.Now()
	for _, p := range panes {
		if p.closing {
			continue
		}
		p.closing = true
		p.closingAt = now
	}
	w.invalidate()
	time.AfterFunc(140*time.Millisecond, func() {
		w.onMain(func() {
			for _, p := range panes {
				if w.paneByID(p.ID) != nil {
					w.closePane(p)
				}
			}
			// closePane fixes focus, but a session that was selected and is
			// now gone still needs the selection moved.
			if w.sessionByID(s.ID) == nil && len(w.Projects) > 0 {
				if first := w.firstSession(); first != nil {
					w.SelectedSession = first.ID
				}
			}
			w.invalidate()
		})
	})
}

func (w *workspace) firstSession() *session {
	for _, pr := range w.Projects {
		if len(pr.Sessions) > 0 {
			return pr.Sessions[0]
		}
	}
	return nil
}

func (w *workspace) requestClose(p *pane) {
	if p.closing {
		return
	}
	p.closing = true
	p.closingAt = time.Now()
	w.invalidate()
	time.AfterFunc(140*time.Millisecond, func() {
		w.onMain(func() {
			if w.paneByID(p.ID) != nil {
				w.closePane(p)
			}
		})
	})
}

// closePane hangs up the pane's program and collapses its slot. Main thread.
func (w *workspace) closePane(p *pane) {
	p.close()
outer:
	for pi, pr := range w.Projects {
		for si, s := range pr.Sessions {
			if s.Root == nil || s.Root.paneByID(p.ID) == nil {
				continue
			}
			s.Root = s.Root.removing(p.ID)
			if s.Root == nil {
				pr.Sessions = append(pr.Sessions[:si], pr.Sessions[si+1:]...)
			}
			if len(pr.Sessions) == 0 {
				w.Projects = append(w.Projects[:pi], w.Projects[pi+1:]...)
			}
			break outer
		}
	}
	if w.FocusedPane == p.ID {
		w.FocusedPane = ""
		if s := w.selected(); s != nil {
			if ps := s.Root.panes(); len(ps) > 0 {
				w.FocusedPane = ps[0].ID
				w.focusReq = ps[0].ID
			}
		}
	}
	if w.sessionByID(w.SelectedSession) == nil {
		if first := w.firstSession(); first != nil {
			w.SelectedSession = first.ID
		} else {
			w.SelectedSession = ""
		}
	}
	w.invalidate()
	w.scheduleSave()
}

func (w *workspace) closeProject(pr *project) {
	if pr == nil {
		return
	}
	panes := []*pane{}
	for _, s := range pr.Sessions {
		if s.Root != nil {
			panes = append(panes, s.Root.panes()...)
		}
	}
	if len(panes) == 0 {
		w.removeProject(pr)
		return
	}
	now := time.Now()
	for _, p := range panes {
		if p.closing {
			continue
		}
		p.closing = true
		p.closingAt = now
	}
	w.invalidate()
	time.AfterFunc(140*time.Millisecond, func() {
		w.onMain(func() {
			for _, p := range panes {
				if w.paneByID(p.ID) != nil {
					w.closePane(p)
				}
			}
			w.removeProject(pr)
			w.invalidate()
		})
	})
}

func (w *workspace) removeProject(pr *project) {
	for i, have := range w.Projects {
		if have == pr {
			w.Projects = append(w.Projects[:i], w.Projects[i+1:]...)
			break
		}
	}
	if w.sessionByID(w.SelectedSession) == nil {
		if first := w.firstSession(); first != nil {
			w.SelectedSession = first.ID
		} else {
			w.SelectedSession = ""
		}
	}
	w.scheduleSave()
}

func (w *workspace) selectSession(id string) {
	s := w.sessionByID(id)
	if s == nil {
		return
	}
	w.SelectedSession = s.ID
	if pr := w.projectOf(s); pr != nil {
		delete(w.collapsed, pr.ID)
	}
	if s.Root != nil {
		if ps := s.Root.panes(); len(ps) > 0 {
			w.FocusedPane = ps[0].ID
			w.focusReq = ps[0].ID
		}
	}
	w.invalidate()
	w.scheduleSave()
}

func (w *workspace) toggleProject(pr *project) {
	if w.collapsed[pr.ID] {
		delete(w.collapsed, pr.ID)
	} else {
		w.collapsed[pr.ID] = true
	}
	w.invalidate()
	w.scheduleSave()
}

func (w *workspace) focusPane(p *pane) {
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	if s := w.sessionOf(p); s != nil {
		w.SelectedSession = s.ID
	}
	w.invalidate()
	w.scheduleSave()
}

// resolveSession accepts a full session id or any unique prefix.
func (w *workspace) resolveSession(raw string) (*session, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var all []*session
	for _, pr := range w.Projects {
		all = append(all, pr.Sessions...)
	}
	var matches []*session
	for _, s := range all {
		id := strings.ToLower(s.ID)
		if id == raw {
			return s, nil
		}
		if raw != "" && strings.HasPrefix(id, raw) {
			matches = append(matches, s)
		}
	}
	switch {
	case raw == "":
		if s := w.selected(); s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("No session is available")
	case len(matches) == 0:
		return nil, fmt.Errorf("No such session: %s", raw)
	case len(matches) > 1:
		return nil, fmt.Errorf("Ambiguous session id: %s", raw)
	}
	return matches[0], nil
}

// resolvePane accepts a full pane id or any unique prefix, as gexi does.
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

// paneExited closes a pane whose program has ended.
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

// cyclePane moves focus through the selected session's panes.
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
	if v := os.Getenv("GEXI_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Gexi")
}

func socketPath() string {
	return filepath.Join(supportDir(), "control.sock")
}

// startAgentMonitor re-checks every pane's foreground group every couple of
// seconds. Process inspection is slow, so it runs off the UI thread; only the
// result is applied there.
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
