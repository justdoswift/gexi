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
}

type savedProject struct {
	ID         string         `json:"id"`
	Dir        string         `json:"dir"`
	Label      string         `json:"label,omitempty"`
	RemoteHost string         `json:"remoteHost,omitempty"`
	Collapsed  bool           `json:"collapsed,omitempty"`
	Sessions   []savedSession `json:"sessions"`
}

type savedSession struct {
	ID   string    `json:"id"`
	Root savedNode `json:"root"`
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
}

func statePath() string {
	return filepath.Join(supportDir(), "workspace.json")
}

func snapshotsDir() string {
	return filepath.Join(supportDir(), "snapshots")
}

func (n *splitNode) saved() savedNode {
	if n.isLeaf() {
		return savedNode{
			Type: "leaf",
			Pane: &savedPane{
				ID:               n.pane.ID,
				Command:          n.pane.restoreCommand,
				WorkingDirectory: n.pane.dir(),
				Worktree:         n.pane.Worktree,
			},
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

// dropMissingWorktrees moves panes whose worktree is gone back to the project
// folder, and forgets the worktree, instead of restoring into a missing path.
func (n *savedNode) dropMissingWorktrees(fallback string) {
	if n == nil {
		return
	}
	if n.Pane != nil && n.Pane.Worktree != nil {
		if info, err := os.Stat(n.Pane.Worktree.Path); err != nil || !info.IsDir() {
			if filepath.Clean(n.Pane.WorkingDirectory) == filepath.Clean(n.Pane.Worktree.Path) {
				n.Pane.WorkingDirectory = fallback
			}
			n.Pane.Worktree = nil
		}
	}
	n.First.dropMissingWorktrees(fallback)
	n.Second.dropMissingWorktrees(fallback)
}

func (w *workspace) savedState() workspaceState {
	state := workspaceState{
		Version:         2,
		SelectedSession: w.SelectedSession,
		FocusedPane:     w.FocusedPane,
	}
	for _, pr := range w.Projects {
		saved := savedProject{
			ID:         pr.ID,
			Dir:        pr.Dir,
			Label:      pr.Label,
			RemoteHost: pr.RemoteHost,
			Collapsed:  w.collapsed[pr.ID],
		}
		for _, s := range pr.Sessions {
			if s.Root == nil {
				continue
			}
			saved.Sessions = append(saved.Sessions, savedSession{
				ID:   s.ID,
				Root: s.Root.saved(),
			})
		}
		if len(saved.Sessions) == 0 {
			continue
		}
		state.Projects = append(state.Projects, saved)
	}
	return state
}

// saveNow writes snapshots and the layout. Main thread.
func (w *workspace) saveNow() {
	if err := os.MkdirAll(snapshotsDir(), 0o755); err != nil {
		return
	}
	if w.settings.RestoreScreen {
		for _, p := range w.allPanes() {
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
	return state, len(state.Projects) > 0 || len(state.Tabs) > 0
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
		p.Worktree = n.Pane.Worktree
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

	for _, sp := range state.Projects {
		pr := &project{ID: sp.ID, Dir: sp.Dir, Label: sp.Label, RemoteHost: sp.RemoteHost}
		for _, ss := range sp.Sessions {
			ss.Root.dropMissingWorktrees(sp.Dir)
			root, err := ss.Root.restore(w)
			if err != nil {
				continue
			}
			pr.Sessions = append(pr.Sessions, &session{ID: ss.ID, Root: root})
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

	if len(w.Projects) == 0 {
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
			if ps := s.Root.panes(); len(ps) > 0 {
				w.FocusedPane = ps[0].ID
			}
		}
	}
	w.focusReq = w.FocusedPane
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
