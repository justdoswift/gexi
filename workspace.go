package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

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
	if title := cleanTitle(p.term.Title()); title != "" {
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

// workspace mirrors gexi's model: projects, the session each holds, focus,
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
	// stageRailShown tells that the off-stage panes' rail has drawn once since
	// the zoom began, so its cards slide in only on entry.
	stageRailShown bool
	// stageW is the stage's last layout width; a narrow stage leaves the
	// rail off rather than cover most of the terminal with it.
	stageW float32

	win         *mygo.Window
	settingsWin *mygo.Window
	settings    appSettings
	// settingsTab is the page the settings window shows.
	settingsTab int
	// themeQuery filters the Themes page; themeScroll keeps where each
	// appearance's carousel is, and themeQuerySeen notices a new query so
	// both start from the beginning.
	themeQuery     string
	themeQuerySeen string
	themeScroll    [2]ui.ScrollState
	quitting       bool
	saveTimer      *time.Timer
	lastNotify     map[string]time.Time

	collapsed   map[string]bool
	searchQuery string

	// archived holds the merged worktrees cleared away with Archive. They
	// are listed in Settings › Worktrees, where they can be restored or
	// deleted for good.
	archived []archivedWorktree

	// detached are panes whose panel was closed while their program kept
	// running. Settings › Worktrees lists them, and reattachPane brings one
	// back into the layout with its live process.
	detached []*pane

	// closeOpen is the alert asking whether a pane whose agent is running
	// should end or go on in the background; closeTarget is its pane.
	closeOpen   bool
	closeTarget *pane
	closeMsg    string

	// worktreesScan caches the checkouts under the app's worktree folder for
	// Settings › Worktrees; worktreesScanned says the cache is current.
	worktreesScan    []worktreeScanEntry
	worktreesScanned bool

	// The files drawer: the right-hand column of changes and files.
	drawer drawerState

	// Each project's Git state, for the pane headers' branch pill and the
	// drawer's list, read off the UI thread.
	gitInfo map[string]gitSummary
	gitBusy map[string]bool
	gitGen  map[string]int

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

	// pendingToast is handed to the next frame's view, which shows it. An
	// action, when set, turns it into a toast with a button.
	pendingToast       string
	pendingToastAction string
	pendingAction      func()

	// leaveOpen is the alert asking whether a checkout with unmerged work
	// should be deleted; leaveTarget is the pane it belongs to.
	leaveOpen   bool
	leaveTarget *pane
	leaveMsg    string

	// commitOpen is the manual commit-message prompt, shown when the pane
	// has no agent that can write one or the agent could not answer.
	commitOpen   bool
	commitTarget *pane
	commitText   string
	commitHint   string
	commitErr    string
}

func newWorkspace() *workspace {
	w := &workspace{
		InstanceID: newID(),
		settings:   loadSettings(),
		collapsed:  map[string]bool{},
		gitInfo:    map[string]gitSummary{},
		gitBusy:    map[string]bool{},
		gitGen:     map[string]int{},
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
	w.syncStageMirrors()
	delete(w.collapsed, pr.ID)
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
	w.scheduleSave()
	w.refreshGit()
	return s, nil
}

// splitDir is where a split beside p starts. A worktree gexi made belongs to
// p alone: another pane there would share its branch and index, so the new
// pane starts in the project folder. A checkout the pane's shell moved into
// itself is followed, since that is the folder the user is working in.
func (w *workspace) splitDir(p *pane) string {
	projectDir := ""
	if pr := w.projectOf(w.sessionOf(p)); pr != nil {
		projectDir = pr.Dir
	}
	return splitDirFor(p, projectDir, p.dir())
}

// splitDirFor decides between the folder the pane is in now and the project
// folder. It is apart from the workspace so the rule can be read and tested
// on its own.
func splitDirFor(p *pane, projectDir, current string) string {
	wt := p.Worktree
	if wt == nil || wt.Detected || projectDir == "" || !dirWithin(current, wt.Path) {
		return current
	}
	return projectDir
}

// splitPane puts a new pane beside p. On a remote project the new pane opens
// another session to the same host. Main thread.
func (w *workspace) splitPane(p *pane, ax axis, command []string, dir string) (*pane, error) {
	if dir == "" {
		dir = w.splitDir(p)
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
	w.syncStageMirrors()
	w.FocusedPane = np.ID
	w.focusReq = np.ID
	w.invalidate()
	w.scheduleSave()
	return np, nil
}

// closeSession closes every pane under the session, asking first when one of
// them has an agent in it.
func (w *workspace) closeSession(s *session) {
	if s == nil || s.Root == nil {
		return
	}
	if panesHaveAgent(s.Root.panes()) {
		w.confirmClose(locales.PaneCloseSessionTitle(), locales.PaneCloseSessionBody(), func() { w.closeSessionNow(s) })
		return
	}
	w.closeSessionNow(s)
}

// panesHaveAgent reports whether any of the panes has an agent running or
// was opened with one, which is what makes closing worth a question.
func panesHaveAgent(panes []*pane) bool {
	for _, p := range panes {
		if p.agent != nil || p.sawAgent || len(p.lastAgentRestore) > 0 {
			return true
		}
	}
	return false
}

// confirmClose asks before a session or project with agents goes. The native
// alert blocks, so it runs off the UI thread and comes back.
func (w *workspace) confirmClose(title, message string, action func()) {
	parent := w.win
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             parent,
			Type:               mygo.MessageWarning,
			Title:              title,
			Message:            message,
			Buttons:            []string{locales.MenuClosePane(), locales.Cancel()},
			DefaultButton:      1,
			CancelButton:       1,
			DestructiveButtons: []int{0},
		})
		if err != nil || res.Button != 0 {
			return
		}
		w.onMain(action)
	}()
}

// closeSessionNow closes every pane under the session; the last closePane
// removes the session itself. Panes fade out first, as closing one from its
// header does. Main thread.
func (w *workspace) closeSessionNow(s *session) {
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
				w.keepClosedWorktree(p.Worktree)
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
			w.keepClosedWorktree(p.Worktree)
		})
	})
}

// requestCloseByUser closes p from the window. A pane where an agent has run
// asks first: a mistyped ⌘W should not end a session, and the alert offers to
// keep it running in the background instead.
func (w *workspace) requestCloseByUser(p *pane) {
	if p == nil || p.closing {
		return
	}
	if p.agent != nil || p.sawAgent || len(p.lastAgentRestore) > 0 {
		w.closeTarget = p
		if p.agent != nil {
			w.closeMsg = locales.PaneCloseAgentRunning()
		} else {
			w.closeMsg = locales.PaneCloseAgentOpened()
		}
		w.closeOpen = true
		w.invalidate()
		return
	}
	w.requestClose(p)
}

// detachPane takes p out of the layout without ending its program. The pane
// keeps reading its PTY, so the agent inside goes on; Settings › Worktrees
// lists it and reattachPane puts it back.
func (w *workspace) detachPane(p *pane) {
	if p == nil || p.closing || w.detachIndex(p) >= 0 {
		return
	}
	// A pane that already left the layout, as one whose program exited while
	// the alert was open, has nothing to detach.
	if w.paneByID(p.ID) != p {
		return
	}
	p.bgProject = w.projectOf(w.sessionOf(p))
	w.stopStageMirror(p)
	if w.zoomedPane == p.ID {
		w.zoomedPane = ""
		w.syncStageMirrors()
	}
	w.removePaneFromLayout(p)
	w.detached = append(w.detached, p)
	w.fixFocusAfterRemoval(p)
	w.worktreesScanned = false
	w.flashAction(locales.PaneDetached(), locales.SettingsWorktrees(), func() {
		w.settingsTab = settingsWorktrees
		w.openSettings()
	})
	w.invalidate()
	w.scheduleSave()
}

// reattachPane puts a background pane back in the layout, in a session of its
// own, and brings it forward.
func (w *workspace) reattachPane(p *pane) {
	i := w.detachIndex(p)
	if i < 0 {
		return
	}
	w.detached = append(w.detached[:i], w.detached[i+1:]...)
	pr := p.bgProject
	if pr == nil || !w.hasProject(pr) {
		pr = w.projectForPane(p)
	}
	p.bgProject = nil
	if pr == nil {
		w.detached = append(w.detached, p)
		return
	}
	s := &session{ID: newID(), Root: leaf(p)}
	pr.Sessions = append(pr.Sessions, s)
	delete(w.collapsed, pr.ID)
	w.SelectedSession = s.ID
	w.zoomedPane = ""
	w.syncStageMirrors()
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.worktreesScanned = false
	w.dismissSettings()
	w.invalidate()
	w.scheduleSave()
	w.refreshGit()
}

// projectForPane finds the project a pane belongs to: the repository its
// worktree came from, or the folder it was opened in. Main thread.
func (w *workspace) projectForPane(p *pane) *project {
	if p.Worktree != nil && p.Worktree.Root != "" {
		return w.projectFor(filepath.Clean(p.Worktree.Root))
	}
	return w.projectFor(filepath.Clean(p.initialDir))
}

func (w *workspace) hasProject(pr *project) bool {
	for _, have := range w.Projects {
		if have == pr {
			return true
		}
	}
	return false
}

func (w *workspace) detachIndex(p *pane) int {
	for i, d := range w.detached {
		if d == p {
			return i
		}
	}
	return -1
}

// livePanes is every pane with a running program, on stage or in the
// background.
func (w *workspace) livePanes() []*pane {
	return append(w.allPanes(), w.detached...)
}

// removePaneFromLayout drops p's leaf, and its session when that was the
// last pane. Main thread.
func (w *workspace) removePaneFromLayout(p *pane) {
	for _, pr := range w.Projects {
		for si, s := range pr.Sessions {
			if s.Root == nil || s.Root.paneByID(p.ID) == nil {
				continue
			}
			s.Root = s.Root.removing(p.ID)
			if s.Root == nil {
				pr.Sessions = append(pr.Sessions[:si], pr.Sessions[si+1:]...)
			}
			return
		}
	}
}

// fixFocusAfterRemoval moves the focus and selection off p, now that its leaf
// is gone. Main thread.
func (w *workspace) fixFocusAfterRemoval(p *pane) {
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
}

// closePane hangs up the pane's program and collapses its slot. Main thread.
func (w *workspace) closePane(p *pane) {
	p.close()
	if w.zoomedPane == p.ID {
		w.zoomedPane = ""
		w.syncStageMirrors()
	}
	w.removePaneFromLayout(p)
	w.fixFocusAfterRemoval(p)
	w.invalidate()
	w.scheduleSave()
}

// closeProject closes every pane under the project, asking first when one of
// them has an agent in it.
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
	if panesHaveAgent(panes) {
		w.confirmClose(locales.PaneCloseProjectTitle(), locales.PaneCloseProjectBody(), func() { w.closeProjectNow(pr) })
		return
	}
	w.closeProjectNow(pr)
}

func (w *workspace) closeProjectNow(pr *project) {
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
				w.keepClosedWorktree(p.Worktree)
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
	w.syncStageMirrors()
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
	w.refreshGit()
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
	w.refreshGit()
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
		w.stageRailShown = false
	}
	w.syncStageMirrors()
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
		// A background pane is not in the layout, so paneByID cannot see it;
		// when its program ends, its entry goes rather than lingering.
		if i := w.detachIndex(p); i >= 0 {
			title := p.title()
			w.detached = append(w.detached[:i], w.detached[i+1:]...)
			p.close()
			w.worktreesScanned = false
			w.flash(locales.PaneBackgroundExited(title))
			w.invalidate()
			return
		}
		// A pane moved into a worktree is replaced by a new pane with the
		// same id; the old process exiting must not close its successor.
		if w.quitting || w.paneByID(p.ID) != p {
			return
		}
		// The worktree button stops the agent before it moves the pane, so
		// the program can exit mid-flow. The slot stays until the flow
		// replaces the pane or brings it back in the project folder.
		if p.worktreeBusy == worktreeBusyCreate {
			p.exited = true
			w.invalidate()
			return
		}
		// A restored pane whose resume never came up keeps its place: a
		// failed resume must not take the session with it.
		if p.restored && !p.sawAgent && len(p.lastAgentRestore) > 0 {
			p.exited = true
			w.invalidate()
			w.scheduleSave()
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
	w.syncStageMirrors()
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
	w.pendingToastAction, w.pendingAction = "", nil
	w.invalidate()
}

// flashAction queues a toast with a button, as the one that offers to install
// a missing tool. Main thread.
func (w *workspace) flashAction(message, label string, action func()) {
	if message == "" {
		return
	}
	w.pendingToast, w.pendingToastAction, w.pendingAction = message, label, action
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
				// Quitting closes the windows first, so every agent looks
				// gone while the layout is still to be saved. Forgetting the
				// resume here is what emptied the panes on an update restart.
				if w.quitting {
					return
				}
				now := time.Now()
				for _, p := range w.livePanes() {
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
			// Perch reports no events, so its busy state is read from the
			// thread file its directory names. This runs off the UI thread
			// with the process scans above, since it touches the disk.
			perchBusy := make([]bool, len(jobs))
			perchPrompts := make([]string, len(jobs))
			for i, j := range jobs {
				if detections[i] == nil || detections[i].Kind != agentPerch {
					continue
				}
				perchBusy[i], perchPrompts[i] = perchTurnState(j.p.dir())
			}
			w.onMain(func() {
				if w.quitting {
					return
				}
				changed := false
				for i, j := range jobs {
					if j.p.applyAgent(detections[i]) {
						changed = true
					}
					if detections[i] != nil && detections[i].Kind == agentPerch {
						if j.p.applyPerchTurn(perchBusy[i], perchPrompts[i]) {
							changed = true
						}
					}
				}
				if changed {
					w.scheduleSave()
				}
			})
		}
	}()
}
