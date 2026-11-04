package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The layout is what comes back on launch: tabs, split ratios, and each
// pane's restore command. The screen comes back as plain text, never as raw
// PTY bytes, which reflow into garbage at a different width.

type workspaceState struct {
	Version     int        `json:"version"`
	SelectedTab string     `json:"selectedTabID,omitempty"`
	FocusedPane string     `json:"focusedPane,omitempty"`
	Tabs        []savedTab `json:"tabs"`
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
	ID               string   `json:"id"`
	Command          []string `json:"command"`
	WorkingDirectory string   `json:"workingDirectory"`
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

func (w *workspace) savedState() workspaceState {
	state := workspaceState{
		Version:     1,
		SelectedTab: w.SelectedTab,
		FocusedPane: w.FocusedPane,
	}
	for _, t := range w.Tabs {
		state.Tabs = append(state.Tabs, savedTab{
			ID:    t.ID,
			Title: t.title(),
			Root:  t.Root.saved(),
		})
	}
	return state
}

// saveNow writes snapshots and the layout. Main thread.
func (w *workspace) saveNow() {
	if err := os.MkdirAll(snapshotsDir(), 0o755); err != nil {
		return
	}
	for _, p := range w.allPanes() {
		if text := normalizeSnapshot(p.text()); text != "" {
			_ = os.WriteFile(filepath.Join(snapshotsDir(), p.ID+".txt"), []byte(text), 0o644)
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
	return state, len(state.Tabs) > 0
}

func (n *savedNode) restore(w *workspace) (*splitNode, error) {
	switch n.Type {
	case "leaf":
		if n.Pane == nil {
			return nil, fmt.Errorf("saved leaf without a pane")
		}
		p, err := newPane(w, n.Pane.ID, n.Pane.Command, n.Pane.WorkingDirectory, loadSnapshot(n.Pane.ID))
		if err != nil {
			return nil, err
		}
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

// restoreOrCreate rebuilds the saved workspace, or starts one tab.
func (w *workspace) restoreOrCreate() {
	state, ok := loadState()
	if !ok {
		if _, err := w.newTab(nil, ""); err != nil {
			return
		}
		return
	}

	for _, st := range state.Tabs {
		root, err := st.Root.restore(w)
		if err != nil {
			continue
		}
		w.Tabs = append(w.Tabs, &tab{ID: st.ID, Root: root})
	}
	if len(w.Tabs) == 0 {
		_, _ = w.newTab(nil, "")
		return
	}

	w.SelectedTab = state.SelectedTab
	if w.selected() == nil || w.SelectedTab == "" {
		w.SelectedTab = w.Tabs[0].ID
	}
	w.FocusedPane = state.FocusedPane
	if w.paneByID(w.FocusedPane) == nil {
		if ps := w.selected().Root.panes(); len(ps) > 0 {
			w.FocusedPane = ps[0].ID
		}
	}
	w.focusReq = w.FocusedPane
}

func loadSnapshot(id string) string {
	data, err := os.ReadFile(filepath.Join(snapshotsDir(), id+".txt"))
	if err != nil {
		return ""
	}
	return string(data)
}

// normalizeSnapshot trims trailing blanks and keeps the last 200 lines, as
// the Swift build does.
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
