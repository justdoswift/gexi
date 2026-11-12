package main

import "testing"

func TestToggleZoom(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	s := &session{ID: "s", Root: leaf(a).splitting("a", axisHorizontal, b)}
	w := &workspace{Projects: []*project{{ID: "p", Sessions: []*session{s}}}}

	w.toggleZoom(a)
	if s.zoomedPane != "a" || w.FocusedPane != "a" || w.focusReq != "a" {
		t.Fatalf("after zooming a: %q, focus %q", s.zoomedPane, w.FocusedPane)
	}
	if p := w.zoomedIn(s); p != a {
		t.Fatalf("zoomedIn: %v, want a", p)
	}

	// The same shortcut puts the other panes back.
	w.toggleZoom(a)
	if s.zoomedPane != "" || w.zoomedIn(s) != nil {
		t.Fatalf("after unzooming: %q", s.zoomedPane)
	}
}

// A pane that is gone leaves the session unzoomed rather than blank.
func TestZoomForgetsAMissingPane(t *testing.T) {
	s := &session{ID: "s", Root: leaf(&pane{ID: "a"}), zoomedPane: "gone"}
	w := &workspace{}
	if p := w.zoomedIn(s); p != nil || s.zoomedPane != "" {
		t.Fatalf("zoomedIn: %v, zoomed %q", p, s.zoomedPane)
	}
}

// Each session keeps its own stage: switching in the sidebar and back shows
// the pane it was left zoomed to.
func TestSelectSessionKeepsEachSessionsZoom(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	one := &session{ID: "one", Root: leaf(a), zoomedPane: "a"}
	two := &session{ID: "two", Root: leaf(b)}
	w := &workspace{
		SelectedSession: "one",
		Projects:        []*project{{ID: "p", Sessions: []*session{one, two}}},
	}

	w.selectSession("two")
	if one.zoomedPane != "a" || two.zoomedPane != "" {
		t.Fatalf("switching away changed the zooms: one %q, two %q", one.zoomedPane, two.zoomedPane)
	}
	if w.FocusedPane != "b" {
		t.Fatalf("focus on two: %q", w.FocusedPane)
	}

	w.selectSession("one")
	if p := w.zoomedIn(one); p != a {
		t.Fatalf("back on one: zoomed %v", p)
	}
	if w.FocusedPane != "a" || w.focusReq != "a" {
		t.Fatalf("focus back on one: %q/%q", w.FocusedPane, w.focusReq)
	}
}

// A notification click for the pane a session has zoomed keeps the stage;
// a click that names another pane of that session has to bring the layout
// back, or the pane it opens stays hidden behind the zoomed one.
func TestNotificationClickKeepsTheZoomedPane(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	s := &session{ID: "s", Root: leaf(a).splitting("a", axisHorizontal, b), zoomedPane: "a"}
	other := &session{ID: "t", Root: leaf(&pane{ID: "c"})}
	w := &workspace{
		Projects:        []*project{{ID: "p", Sessions: []*session{s, other}}},
		SelectedSession: "t",
		FocusedPane:     "c",
	}

	w.openNotification("pane:a")
	w.revealPendingNotification()
	if s.zoomedPane != "a" || w.SelectedSession != "s" || w.FocusedPane != "a" {
		t.Fatalf("zoom %q, session %q, focus %q", s.zoomedPane, w.SelectedSession, w.FocusedPane)
	}

	w.openNotification("pane:b")
	w.revealPendingNotification()
	if s.zoomedPane != "" || w.FocusedPane != "b" {
		t.Fatalf("zoom %q, focus %q", s.zoomedPane, w.FocusedPane)
	}
}

// The zoom belongs to the layout, so it is written with it and comes back
// after a launch.
func TestZoomIsSaved(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	s := &session{ID: "s", Root: leaf(a).splitting("a", axisHorizontal, b), zoomedPane: "b"}
	w := &workspace{Projects: []*project{{ID: "p", Dir: t.TempDir(), Sessions: []*session{s}}}}

	state := w.savedState()
	if len(state.Projects) != 1 || len(state.Projects[0].Sessions) != 1 {
		t.Fatalf("saved state: %+v", state)
	}
	if got := state.Projects[0].Sessions[0].ZoomedPane; got != "b" {
		t.Fatalf("saved zoom: %q", got)
	}
}
