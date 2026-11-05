package main

import "testing"

func TestToggleZoom(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	s := &session{ID: "s", Root: leaf(a).splitting("a", axisHorizontal, b)}
	w := &workspace{}

	w.toggleZoom(a)
	if w.zoomedPane != "a" || w.FocusedPane != "a" || w.focusReq != "a" {
		t.Fatalf("after zooming a: %q, focus %q", w.zoomedPane, w.FocusedPane)
	}
	if p := w.zoomedIn(s); p != a {
		t.Fatalf("zoomedIn: %v, want a", p)
	}

	// The same shortcut puts the other panes back.
	w.toggleZoom(a)
	if w.zoomedPane != "" || w.zoomedIn(s) != nil {
		t.Fatalf("after unzooming: %q", w.zoomedPane)
	}
}

// A pane that is gone leaves the session unzoomed rather than blank.
func TestZoomForgetsAMissingPane(t *testing.T) {
	s := &session{ID: "s", Root: leaf(&pane{ID: "a"})}
	w := &workspace{zoomedPane: "gone"}
	if p := w.zoomedIn(s); p != nil || w.zoomedPane != "" {
		t.Fatalf("zoomedIn: %v, zoomed %q", p, w.zoomedPane)
	}
}

// Choosing another session shows its own panes.
func TestSelectSessionLeavesZoom(t *testing.T) {
	a := &pane{ID: "a"}
	s := &session{ID: "s", Root: leaf(a)}
	w := &workspace{
		zoomedPane: "a",
		Projects:   []*project{{ID: "p", Sessions: []*session{s}}},
	}
	w.selectSession("s")
	if w.zoomedPane != "" {
		t.Fatalf("zoom survived selecting a session: %q", w.zoomedPane)
	}
}
