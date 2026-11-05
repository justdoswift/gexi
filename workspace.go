package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/justdoswift/gexi/locales"
)

// The workspace is two levels: a project is a folder on disk, and the sessions
// opened in it are the shells and agents working in that folder. Each session
// holds its own split tree of panes.

// project is a folder the user works in, or a remote host. A remote project
// keeps the local home as its working directory, because that is where the
// ssh process starts; Label names it and RemoteHost marks it.
type project struct {
	ID         string
	Dir        string
	Label      string
	RemoteHost string
	Sessions   []*session
}

// session is one split tree of panes inside a project.
type session struct {
	ID   string
	Root *splitNode
}

func (s *session) title() string {
	if s.Root == nil {
		return locales.Terminal()
	}
	ps := s.Root.panes()
	if len(ps) == 0 {
		return locales.Terminal()
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

// name is what the sidebar calls a project: a remote host's alias, or the
// folder's own name.
func (p *project) name() string {
	if p.Label != "" {
		return p.Label
	}
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
	// zoomedPane fills the session on its own while set, so the other panes
	// keep their places without being drawn. zoomSettling asks the next frame
	// to fade and grow the layout in after a zoom changes.
	zoomedPane   string
	zoomSettling bool

	win         *mygo.Window
	settingsWin *mygo.Window
	settings    appSettings
	// settingsTab is the page the settings window shows.
	settingsTab int
	quitting    bool
	saveTimer   *time.Timer
	lastNotify  map[string]time.Time

	collapsed   map[string]bool
	searchQuery string

	// Remote access: the reverse tunnel this Mac keeps when it is reachable,
	// and the small dialog that turns it on.
	tunnel     *exec.Cmd
	tunnelStop chan struct{}
	tunnelUp   bool
	tunnelNote string
	relayOpen  bool
	relayInput string
	relayName  string
	relayErr   string
	relayBusy  bool
	importOpen bool
	importText string
	importErr  string

	// pendingToast is handed to the next frame's view, which shows it.
	pendingToast string
}

func newWorkspace() *workspace {
	w := &workspace{
		InstanceID: newID(),
		settings:   loadSettings(),
		collapsed:  map[string]bool{},
	}
	// A Mac that was left reachable comes back reachable without a click.
	w.startRemoteTunnel()
	return w
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
			Title:             locales.NewProjectTitle(),
			Message:           locales.NewProjectMessage(),
			ButtonLabel:       locales.Open(),
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		w.onMain(func() { _, _, _ = w.openProject(paths[0]) })
	}()
}

// projectFor returns the local project for dir, adding one when there is
// none. Remote projects keep the home directory as their working directory,
// so they are not candidates here. Main thread.
func (w *workspace) projectFor(dir string) *project {
	for _, pr := range w.Projects {
		if pr.RemoteHost == "" && pr.Dir == dir {
			return pr
		}
	}
	pr := &project{ID: newID(), Dir: dir}
	w.Projects = append(w.Projects, pr)
	delete(w.collapsed, pr.ID)
	return pr
}

// remoteProject returns the project for a host alias, or nil.
func (w *workspace) remoteProject(host string) *project {
	for _, pr := range w.Projects {
		if pr.RemoteHost == host {
			return pr
		}
	}
	return nil
}

// connectHost opens a session on host, reusing the project that already
// groups sessions for it. The pane runs ssh with the user's own config, so
// ProxyJump, identities, and anything else in ~/.ssh/config apply unchanged.
// Main thread.
func (w *workspace) connectHost(h sshHost) {
	pr := w.remoteProject(h.Alias)
	if pr == nil {
		home, _ := os.UserHomeDir()
		pr = &project{
			ID:         newID(),
			Dir:        home,
			Label:      h.Alias,
			RemoteHost: h.Alias,
		}
		w.Projects = append(w.Projects, pr)
		delete(w.collapsed, pr.ID)
	}
	_, _ = w.newSession(pr, h.command(w.settings.RemoteTmux), pr.Dir)
}

// projectCommand fills in a remote project's default command: another ssh
// session to its host. A local project keeps the login shell.
func (w *workspace) projectCommand(pr *project, command []string) []string {
	if pr == nil || command != nil || pr.RemoteHost == "" {
		return command
	}
	return sshHost{Alias: pr.RemoteHost}.command(w.settings.RemoteTmux)
}

// newSession runs command (or a login shell) in the project and selects it.
// Main thread.
func (w *workspace) newSession(pr *project, command []string, dir string) (*session, error) {
	if pr == nil {
		return nil, fmt.Errorf("No project is open")
	}
	command = w.projectCommand(pr, command)
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
	w.zoomedPane = ""
	delete(w.collapsed, pr.ID)
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
	w.scheduleSave()
	return s, nil
}

// splitPane puts a new pane beside p. On a remote project the new pane opens
// another session to the same host. Main thread.
func (w *workspace) splitPane(p *pane, ax axis, command []string, dir string) (*pane, error) {
	if dir == "" {
		dir = p.dir()
	}
	command = w.projectCommand(w.projectOf(w.sessionOf(p)), command)
	np, err := newPane(w, "", command, dir, "")
	if err != nil {
		return nil, err
	}
	host := w.sessionOf(p)
	if host == nil {
		return nil, fmt.Errorf("No such pane: %s", p.ID)
	}
	host.Root = host.Root.splitting(p.ID, ax, np)
	// A split made while zoomed has to be visible, so the session comes
	// back to showing all its panes.
	w.zoomedPane = ""
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
	time.AfterFunc(paneMotion, func() {
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
	time.AfterFunc(paneMotion, func() {
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
	if w.zoomedPane == p.ID {
		w.zoomedPane = ""
	}
outer:
	for _, pr := range w.Projects {
		for si, s := range pr.Sessions {
			if s.Root == nil || s.Root.paneByID(p.ID) == nil {
				continue
			}
			s.Root = s.Root.removing(p.ID)
			if s.Root == nil {
				pr.Sessions = append(pr.Sessions[:si], pr.Sessions[si+1:]...)
			}
			// The project stays when its last session closes: a folder is a
			// place the user opened, not a side effect of a session. It goes
			// only when they close it.
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
	time.AfterFunc(paneMotion, func() {
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
	w.zoomedPane = ""
	if s.Root != nil {
		for _, p := range s.Root.panes() {
			if p.Worktree != nil {
				w.refreshWorktree(p)
			}
		}
	}
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

// toggleZoom fills the session with p, or puts the other panes back when p
// already fills it.
func (w *workspace) toggleZoom(p *pane) {
	if p == nil {
		return
	}
	if w.zoomedPane == p.ID {
		w.zoomedPane = ""
	} else {
		w.zoomedPane = p.ID
		w.FocusedPane = p.ID
		w.focusReq = p.ID
	}
	w.zoomSettling = true
	w.invalidate()
}

// zoomedIn returns the pane the session is zoomed to. A pane that is gone
// leaves the session unzoomed rather than blank.
func (w *workspace) zoomedIn(s *session) *pane {
	if w.zoomedPane == "" || s == nil || s.Root == nil {
		return nil
	}
	p := s.Root.paneByID(w.zoomedPane)
	if p == nil {
		w.zoomedPane = ""
	}
	return p
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
		// A pane moved into a worktree is replaced by a new pane with the
		// same id; the old process exiting must not close its successor.
		if w.quitting || w.paneByID(p.ID) != p {
			return
		}
		w.requestClose(p)
	})
}

// cyclePane moves focus through the selected session's panes.
func (w *workspace) cyclePane(delta int) {
	panes := w.visiblePanes()
	if len(panes) < 2 {
		return
	}
	// Moving between panes brings the whole layout back, so the pane the
	// focus lands on is visible.
	w.zoomedPane = ""
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

// flash queues a toast for the next frame. Main thread.
func (w *workspace) flash(message string) {
	if message == "" {
		return
	}
	w.pendingToast = message
	w.invalidate()
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
