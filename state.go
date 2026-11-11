package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The layout is what comes back on launch: projects and their sessions, split
// ratios, and each pane's restore command. The screen comes back as plain
// text, never as raw PTY bytes, which reflow into garbage at a different
// width.

type workspaceState struct {
	Version         int            `json:"version"`
	SelectedSession string         `json:"selectedSession,omitempty"`
	FocusedPane     string         `json:"focusedPane,omitempty"`
	Projects        []savedProject `json:"projects,omitempty"`

	// Version 1 had no projects: its tabs were sessions without a folder of
	// their own, so they are grouped by directory on the way in.
	SelectedTab string     `json:"selectedTabID,omitempty"`
	Tabs        []savedTab `json:"tabs,omitempty"`

	// The files drawer: whether it was open, which tab it showed, and how
	// wide it was.
	DrawerOpen      bool    `json:"drawerOpen,omitempty"`
	DrawerTab       string  `json:"drawerTab,omitempty"`
	DrawerListWidth float32 `json:"drawerListWidth,omitempty"`
	DrawerFileWidth float32 `json:"drawerFileWidth,omitempty"`

	// Merged worktrees the user cleared away with Archive. Their checkouts
	// are gone but their branches are kept, so Settings › Archived can put
	// them back.
	ArchivedWorktrees []archivedWorktree `json:"archivedWorktrees,omitempty"`
	// Detached are panes kept running in the background. Their commands come
	// back on launch, so "Keep Running" survives a restart as far as a new
	// process can: the agent resumes in the same checkout.
	Detached []savedDetachedPane `json:"detached,omitempty"`
	// WorktreeRestores remembers the command of a pane whose checkout was
	// kept when the pane closed, keyed by the checkout path.
	WorktreeRestores map[string][]string `json:"worktreeRestores,omitempty"`
}

// savedDetachedPane is a background pane and the project it returns to when
// reattached.
type savedDetachedPane struct {
	Pane       savedPane `json:"pane"`
	ProjectDir string    `json:"projectDir,omitempty"`
}

type savedProject struct {
	ID    string `json:"id"`
	Dir   string `json:"dir"`
	Label string `json:"label,omitempty"`
	// RemoteHost is set by layouts saved while gexi had remote hosts.
	// Restore drops those projects; the field stays so old layouts load.
	RemoteHost string         `json:"remoteHost,omitempty"`
	Collapsed  bool           `json:"collapsed,omitempty"`
	Sessions   []savedSession `json:"sessions"`
}

type savedSession struct {
	ID string `json:"id"`
	// Label is the name the user gave the session, so a rename survives a
	// launch instead of falling back to the branch or terminal title.
	Label string    `json:"label,omitempty"`
	Root  savedNode `json:"root"`
	// ZoomedPane is the pane the session was left filling it, so the stage
	// comes back after a launch the way it was left.
	ZoomedPane string `json:"zoomedPane,omitempty"`
}

type savedTab struct {
	ID    string    `json:"id"`
	Title string    `json:"title,omitempty"`
	Root  savedNode `json:"root"`
}

type savedNode struct {
	Type   string     `json:"type"`
	Pane   *savedPane `json:"pane,omitempty"`
	Axis   string     `json:"axis,omitempty"`
	Ratio  float32    `json:"ratio,omitempty"`
	First  *savedNode `json:"first,omitempty"`
	Second *savedNode `json:"second,omitempty"`
}

type savedPane struct {
	ID               string        `json:"id"`
	Command          []string      `json:"command"`
	WorkingDirectory string        `json:"workingDirectory"`
	Worktree         *paneWorktree `json:"worktree,omitempty"`
	// Label is the name the user set or gexi made from the first message,
	// so the pane comes back with it.
	Label string `json:"label,omitempty"`
}

func statePath() string {
	return filepath.Join(supportDir(), "workspace.json")
}

func snapshotsDir() string {
	return filepath.Join(supportDir(), "snapshots")
}

func (p *pane) saved() savedPane {
	return savedPane{
		ID:               p.ID,
		Command:          p.savedCommand(),
		WorkingDirectory: p.dir(),
		Worktree:         p.Worktree,
		Label:            p.Label,
	}
}

func (n *splitNode) saved() savedNode {
	if n.isLeaf() {
		pane := n.pane.saved()
		return savedNode{
			Type: "leaf",
			Pane: &pane,
		}
	}
	ax := "horizontal"
	if n.axis == axisVertical {
		ax = "vertical"
	}
	return savedNode{
		Type:   "split",
		Axis:   ax,
		Ratio:  n.ratio,
		First:  ptr(n.first.saved()),
		Second: ptr(n.second.saved()),
	}
}

func ptr(n savedNode) *savedNode { return &n }

// dropMissingWorktrees moves panes whose checkout is gone back to the
// project folder, and forgets the worktree, instead of restoring into a
// missing path. A split pane inside a worktree has no worktree of its own,
// so its folder is checked too.
func (n *savedNode) dropMissingWorktrees(fallback string) {
	if n == nil {
		return
	}
	if n.Pane != nil {
		p := n.Pane
		missing := func(dir string) bool {
			info, err := os.Stat(dir)
			return err != nil || !info.IsDir()
		}
		if p.Worktree != nil && missing(p.Worktree.Path) {
			if filepath.Clean(p.WorkingDirectory) == filepath.Clean(p.Worktree.Path) {
				p.WorkingDirectory = fallback
			}
			p.Worktree = nil
		}
		root := filepath.Clean(worktreesRoot()) + string(os.PathSeparator)
		if p.Worktree == nil && strings.HasPrefix(filepath.Clean(p.WorkingDirectory), root) && missing(p.WorkingDirectory) {
			p.WorkingDirectory = fallback
		}
	}
	n.First.dropMissingWorktrees(fallback)
	n.Second.dropMissingWorktrees(fallback)
}

func (w *workspace) savedState() workspaceState {
	// A session on another Mac is not this window's to restore: its host
	// brings it back next time the two are paired, and the pairing is
	// what the sidebar draws it from.
	selected, focused := w.SelectedSession, w.FocusedPane
	if s := w.sessionByID(selected); s != nil && s.remote != nil {
		selected, focused = "", ""
		if first := w.firstSession(); first != nil {
			selected = first.ID
			if first.Root != nil {
				if ps := first.Root.panes(); len(ps) > 0 {
					focused = ps[0].ID
				}
			}
		}
	}
	state := workspaceState{
		Version:           2,
		SelectedSession:   selected,
		FocusedPane:       focused,
		DrawerOpen:        w.drawer.open,
		DrawerTab:         "changes",
		DrawerListWidth:   w.drawer.listWidth,
		DrawerFileWidth:   w.drawer.fileWidth,
		ArchivedWorktrees: w.archived,
		WorktreeRestores:  w.worktreeRestores,
	}
	if w.drawer.tab == drawerTabFiles {
		state.DrawerTab = "files"
	}
	for _, pr := range w.Projects {
		saved := savedProject{
			ID:        pr.ID,
			Dir:       pr.Dir,
			Label:     pr.Label,
			Collapsed: w.collapsed[pr.ID],
		}
		for _, s := range pr.Sessions {
			if s.Root == nil {
				continue
			}
			saved.Sessions = append(saved.Sessions, savedSession{
				ID:         s.ID,
				Label:      s.Label,
				Root:       s.Root.saved(),
				ZoomedPane: s.zoomedPane,
			})
		}
		if len(saved.Sessions) == 0 {
			continue
		}
		state.Projects = append(state.Projects, saved)
	}
	for _, p := range w.detached {
		projectDir := ""
		if p.bgProject != nil {
			projectDir = p.bgProject.Dir
		}
		state.Detached = append(state.Detached, savedDetachedPane{
			Pane:       p.saved(),
			ProjectDir: projectDir,
		})
	}
	return state
}

// saveNow writes snapshots and the layout. Main thread.
func (w *workspace) saveNow() {
	if err := os.MkdirAll(snapshotsDir(), 0o755); err != nil {
		return
	}
	if w.settings.RestoreScreen {
		for _, p := range w.livePanes() {
			if text := normalizeSnapshot(p.text()); text != "" {
				_ = os.WriteFile(filepath.Join(snapshotsDir(), p.ID+".txt"), []byte(text), 0o644)
			}
		}
	}

	data, err := json.MarshalIndent(w.savedState(), "", "  ")
	if err != nil {
		return
	}
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, statePath())
}

// scheduleSave debounces writes after model changes.
func (w *workspace) scheduleSave() {
	if w.win == nil {
		return
	}
	if w.saveTimer != nil {
		w.saveTimer.Stop()
	}
	w.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		w.onMain(func() { w.saveNow() })
	})
}

func loadState() (workspaceState, bool) {
	var state workspaceState
	data, err := os.ReadFile(statePath())
	if err != nil {
		return state, false
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, false
	}
	return state, len(state.Projects) > 0 || len(state.Tabs) > 0 || len(state.Detached) > 0 ||
		len(state.ArchivedWorktrees) > 0 || len(state.WorktreeRestores) > 0
}

// restore brings a background pane back with the command it was saved with.
// A checkout that is gone drops the worktree binding rather than opening a
// pane in a missing folder.
func (sp savedDetachedPane) restore(w *workspace) *pane {
	pane := sp.Pane
	dir := pane.WorkingDirectory
	if pane.Worktree != nil {
		if info, err := os.Stat(pane.Worktree.Path); err != nil || !info.IsDir() {
			pane.Worktree = nil
			if sp.ProjectDir != "" {
				dir = sp.ProjectDir
			}
		}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	snapshot := ""
	if w.settings.RestoreScreen {
		snapshot = loadSnapshot(pane.ID)
	}
	p, err := newPane(w, pane.ID, pane.Command, dir, snapshot)
	if err != nil {
		return nil
	}
	p.restored = true
	p.Worktree = pane.Worktree
	p.Label = pane.Label
	p.nameAsked = p.Label != ""
	return p
}

func (n *savedNode) restore(w *workspace) (*splitNode, error) {
	switch n.Type {
	case "leaf":
		if n.Pane == nil {
			return nil, fmt.Errorf("saved leaf without a pane")
		}
		snapshot := ""
		if w.settings.RestoreScreen {
			snapshot = loadSnapshot(n.Pane.ID)
		}
		p, err := newPane(w, n.Pane.ID, n.Pane.Command, n.Pane.WorkingDirectory, snapshot)
		if err != nil {
			return nil, err
		}
		p.restored = true
		p.Worktree = n.Pane.Worktree
		p.Label = n.Pane.Label
		p.nameAsked = p.Label != ""
		return leaf(p), nil
	case "split":
		if n.First == nil || n.Second == nil {
			return nil, fmt.Errorf("saved split without both sides")
		}
		first, err := n.First.restore(w)
		if err != nil {
			return nil, err
		}
		second, err := n.Second.restore(w)
		if err != nil {
			return nil, err
		}
		ax := axisHorizontal
		if n.Axis == "vertical" {
			ax = axisVertical
		}
		ratio := n.Ratio
		if ratio <= 0 || ratio >= 100 {
			ratio = 50
		}
		return &splitNode{axis: ax, ratio: ratio, first: first, second: second}, nil
	default:
		return nil, fmt.Errorf("unknown saved node %q", n.Type)
	}
}

// restoreOrCreate rebuilds the saved workspace, or starts one project with a
// session in it.
func (w *workspace) restoreOrCreate() {
	state, ok := loadState()
	if !ok {
		_, _, _ = w.openProject("")
		return
	}

	w.archived = state.ArchivedWorktrees
	w.worktreeRestores = state.WorktreeRestores
	if w.worktreeRestores == nil {
		w.worktreeRestores = map[string][]string{}
	}

	for _, sp := range state.Projects {
		// Remote projects are gone; their panes were ssh sessions.
		if sp.RemoteHost != "" {
			continue
		}
		pr := &project{ID: sp.ID, Dir: sp.Dir, Label: sp.Label}
		for _, ss := range sp.Sessions {
			ss.Root.dropMissingWorktrees(sp.Dir)
			root, err := ss.Root.restore(w)
			if err != nil {
				continue
			}
			s := &session{ID: ss.ID, Label: ss.Label, Root: root}
			// A zoom that names a pane the layout no longer has would leave
			// the session blank, so it is dropped.
			if root.paneByID(ss.ZoomedPane) != nil {
				s.zoomedPane = ss.ZoomedPane
			}
			pr.Sessions = append(pr.Sessions, s)
		}
		if len(pr.Sessions) == 0 {
			continue
		}
		if sp.Collapsed {
			w.collapsed[pr.ID] = true
		}
		w.Projects = append(w.Projects, pr)
	}

	// Version 1: tabs group into projects by the folder they ran in.
	if len(w.Projects) == 0 {
		for _, st := range state.Tabs {
			root, err := st.Root.restore(w)
			if err != nil {
				continue
			}
			ps := root.panes()
			if len(ps) == 0 {
				continue
			}
			pr := w.projectFor(ps[0].dir())
			pr.Sessions = append(pr.Sessions, &session{ID: st.ID, Root: root})
		}
	}

	// Background panes come back with their saved command, so a pane kept
	// running is still in the Worktrees page after a restart.
	for _, sp := range state.Detached {
		p := sp.restore(w)
		if p == nil {
			continue
		}
		if sp.ProjectDir != "" {
			for _, pr := range w.Projects {
				if pr.Dir == sp.ProjectDir {
					p.bgProject = pr
					break
				}
			}
		}
		w.detached = append(w.detached, p)
	}

	if len(w.Projects) == 0 && len(w.detached) == 0 {
		_, _, _ = w.openProject("")
		return
	}

	w.SelectedSession = state.SelectedSession
	if w.SelectedSession == "" {
		w.SelectedSession = state.SelectedTab
	}
	if w.sessionByID(w.SelectedSession) == nil {
		if first := w.firstSession(); first != nil {
			w.SelectedSession = first.ID
		}
	}
	w.FocusedPane = state.FocusedPane
	if w.paneByID(w.FocusedPane) == nil {
		if s := w.selected(); s != nil && s.Root != nil {
			if p := w.zoomedIn(s); p != nil {
				w.FocusedPane = p.ID
			} else if ps := s.Root.panes(); len(ps) > 0 {
				w.FocusedPane = ps[0].ID
			}
		}
	}
	w.focusReq = w.FocusedPane
	// A session restored zoomed needs its rail's previews started before the
	// first frame draws them.
	w.syncStageMirrors()
	// The drawer comes back as it was left, without sliding in.
	w.drawer.listWidth = state.DrawerListWidth
	w.drawer.fileWidth = state.DrawerFileWidth
	if state.DrawerTab == "files" {
		w.drawer.tab = drawerTabFiles
	}
	if state.DrawerOpen {
		w.drawer.open = true
		w.drawer.shown = true
		w.resetDrawer()
		if w.drawer.tab == drawerTabChanges {
			w.refreshGit()
		}
	}

	if s := w.selected(); s != nil && s.Root != nil {
		for _, p := range s.Root.panes() {
			if p.Worktree != nil {
				w.refreshWorktree(p)
			}
		}
	}
}

func loadSnapshot(id string) string {
	data, err := os.ReadFile(filepath.Join(snapshotsDir(), id+".txt"))
	if err != nil {
		return ""
	}
	return string(data)
}

// normalizeSnapshot trims trailing blanks and keeps the last 200 lines.
func normalizeSnapshot(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	return strings.Join(lines, "\n")
}
