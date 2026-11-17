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

// Only unzooming clears a zoom. Splitting, cycling, dropping a pane, and a
// click on a notification each hand the stage to the pane they bring forward
// instead, with the pane that leaves it shrinking back into the rail.
func TestZoomHandsTheStageOver(t *testing.T) {
	a, b, c := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}
	s := &session{ID: "s", Root: leaf(a).splitting("a", axisHorizontal, b), zoomedPane: "a"}
	w := &workspace{
		Projects:        []*project{{ID: "p", Sessions: []*session{s}}},
		SelectedSession: "s",
		FocusedPane:     "a",
	}

	// Cycling walks the stage through the session.
	w.cyclePane(1)
	if s.zoomedPane != "b" || w.FocusedPane != "b" {
		t.Fatalf("after cycling: zoomed %q, focus %q", s.zoomedPane, w.FocusedPane)
	}
	if !a.stageCardShrink {
		t.Fatal("the pane that left the stage does not shrink into its card")
	}

	// A split opens its new pane on the stage, as splitPane asks for.
	s.Root = s.Root.splitting("b", axisVertical, c)
	w.showPane(c)
	if s.zoomedPane != "c" {
		t.Fatalf("after splitting: zoomed %q, want the new pane", s.zoomedPane)
	}
	if !b.stageCardShrink {
		t.Fatal("the pane the split replaced does not shrink into its card")
	}
}

// Closing a pane inside a zoom narrows the session: the pane that was first
// in the rail takes the stage, and closing one behind the stage leaves the
// zoom alone.
func TestClosingAPaneInsideAZoomKeepsTheZoom(t *testing.T) {
	a, b, c := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}
	for _, p := range []*pane{a, b, c} {
		term, _ := probeTerminal(t)
		p.term = term
	}
	root := leaf(a).splitting("a", axisHorizontal, b).splitting("a", axisVertical, c)
	s := &session{ID: "s", Root: root, zoomedPane: "a"}
	w := &workspace{
		Projects:        []*project{{ID: "p", Sessions: []*session{s}}},
		SelectedSession: "s",
		FocusedPane:     "a",
	}

	w.closePane(c)
	if p := w.zoomedIn(s); p != a {
		t.Fatalf("closing a pane behind the stage left %v on it, want a", p)
	}

	w.closePane(a)
	if p := w.zoomedIn(s); p != b {
		t.Fatalf("closing the staged pane left %v on it, want b", p)
	}
	if w.FocusedPane != "b" || w.focusReq != "b" {
		t.Fatalf("focus after the close: %q/%q, want b", w.FocusedPane, w.focusReq)
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
// a click that names another pane of that session brings that pane to the
// stage, so the pane it opens is not left hidden behind the zoom.
func TestNotificationClickBringsItsPaneToTheStage(t *testing.T) {
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
	if s.zoomedPane != "b" || w.FocusedPane != "b" {
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
