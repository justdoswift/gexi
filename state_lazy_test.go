package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRestoreStartsOnlyTheShownSession: a window comes back with every
// session in the sidebar and only the one it opens on running.
//
// Restoring used to start a shell and an agent for every saved pane, so a
// window with fourteen sessions paid for fourteen agents before it drew
// anything. The panes of the sessions nobody has opened are layout only, and
// take a terminal the first time they are drawn.
func TestRestoreStartsOnlyTheShownSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEXI_HOME", home)

	dir := t.TempDir()
	state := workspaceState{
		SelectedSession: "s1",
		Projects: []savedProject{{
			ID: "p1", Dir: dir,
			Sessions: []savedSession{
				{ID: "s1", Root: savedNode{Type: "leaf", Pane: &savedPane{
					ID: "shown", Command: []string{"/bin/sh"}, WorkingDirectory: dir,
				}}},
				{ID: "s2", Root: savedNode{Type: "leaf", Pane: &savedPane{
					ID: "hidden", Command: []string{"/bin/sh"}, WorkingDirectory: dir,
				}}},
			},
		}},
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "workspace.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	w := &workspace{InstanceID: "test", settings: defaultSettings()}
	w.restoreOrCreate()

	byID := map[string]*pane{}
	for _, pr := range w.Projects {
		for _, s := range pr.Sessions {
			for _, p := range s.Root.panes() {
				byID[p.ID] = p
			}
		}
	}
	if len(byID) != 2 {
		t.Fatalf("restored %d panes, want 2", len(byID))
	}
	shown := byID["shown"]
	if shown == nil || shown.dormant {
		t.Fatalf("the shown session's pane did not start: %+v", shown)
	}
	if shown.term == nil {
		t.Fatal("the shown session's pane has no terminal")
	}
	hidden := byID["hidden"]
	if hidden == nil || !hidden.dormant {
		t.Fatalf("the hidden session's pane started anyway: %+v", hidden)
	}
	if hidden.term != nil {
		t.Fatal("a dormant pane has a terminal")
	}
	if len(hidden.command) == 0 || hidden.initialDir == "" {
		t.Fatal("a dormant pane forgot what to run or where")
	}
}

// TestWakingAPaneStartsItOnce: the frame that draws a dormant pane starts its
// program, and a second frame does not start another.
func TestWakingAPaneStartsItOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEXI_HOME", home)
	dir := t.TempDir()

	w := &workspace{InstanceID: "test", settings: defaultSettings()}
	p, err := newDormantPane(w, "p1", []string{"/bin/sh"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.dormant || p.term != nil {
		t.Fatalf("a dormant pane came up live: dormant=%v term=%v", p.dormant, p.term)
	}

	w.wakePane(p)
	if p.dormant {
		t.Fatal("the pane is still dormant after waking")
	}
	if p.term == nil {
		t.Fatal("the woken pane has no terminal")
	}
	first := p.term
	w.wakePane(p)
	if p.term != first {
		t.Fatal("a second frame started the pane again")
	}
	p.close()
}

// TestDormantPaneKeepsItsLayout: a pane nobody has opened still knows what it
// would run and where, which is what lets it come up later.
func TestDormantPaneKeepsItsLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEXI_HOME", home)
	dir := t.TempDir()

	w := &workspace{InstanceID: "test", settings: defaultSettings()}
	p, err := newDormantPane(w, "p1", []string{"/bin/sh"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.command) == 0 || p.command[0] != "/bin/sh" {
		t.Fatalf("command: %v", p.command)
	}
	if p.initialDir != dir {
		t.Fatalf("dir: %q, want %q", p.initialDir, dir)
	}
	if p.ID != "p1" {
		t.Fatalf("id: %q", p.ID)
	}
	// A dormant pane answers the questions the sidebar and the stage ask
	// without a terminal to ask.
	if got := p.text(); got != "" {
		t.Fatalf("text: %q", got)
	}
	p.send("echo hi")
}

// TestRestoreKeepsTheShownSessionLive: the session the window opens on starts
// even when the save names it only through the old tab field.
func TestRestoreKeepsTheShownSessionLive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEXI_HOME", home)
	dir := t.TempDir()

	state := workspaceState{
		SelectedTab: "s1",
		Projects: []savedProject{{
			ID: "p1", Dir: dir,
			Sessions: []savedSession{
				{ID: "s1", Root: savedNode{Type: "leaf", Pane: &savedPane{
					ID: "shown", Command: []string{"/bin/sh"}, WorkingDirectory: dir,
				}}},
				{ID: "s2", Root: savedNode{Type: "leaf", Pane: &savedPane{
					ID: "hidden", Command: []string{"/bin/sh"}, WorkingDirectory: dir,
				}}},
			},
		}},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	_ = os.MkdirAll(home, 0o755)
	if err := os.WriteFile(filepath.Join(home, "workspace.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	w := &workspace{InstanceID: "test", settings: defaultSettings()}
	w.restoreOrCreate()

	byID := map[string]*pane{}
	for _, pr := range w.Projects {
		for _, s := range pr.Sessions {
			for _, p := range s.Root.panes() {
				byID[p.ID] = p
			}
		}
	}
	if p := byID["shown"]; p == nil || p.dormant || p.term == nil {
		t.Fatalf("the shown session did not start: %+v", p)
	}
	if p := byID["hidden"]; p == nil || !p.dormant {
		t.Fatalf("the hidden session started: %+v", p)
	}
	for _, p := range byID {
		p.close()
	}
}
