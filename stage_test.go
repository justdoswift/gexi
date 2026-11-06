package main

import (
	"strings"
	"testing"
)

func paneIDs(ps []*pane) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// TestStagePeekKeepsTheTail covers what a card shows: the last lines of the
// screen, trailing blanks gone, and a long wrapped line cut at the end the
// terminal was showing rather than at its beginning.
func TestStagePeekKeepsTheTail(t *testing.T) {
	long := "head" + strings.Repeat("x", 30) + "TAIL"
	text := "one\ntwo\nthree\nfour\nfive\n" + long + "\n\n\n"
	got := stagePeekLinesOf(text)
	if len(got) != stagePeekLines {
		t.Fatalf("got %d lines: %q", len(got), got)
	}
	if got[0] != "three" || got[1] != "four" || got[2] != "five" {
		t.Fatalf("tail lines: %q", got)
	}
	if runes := []rune(got[3]); len(runes) != stagePeekCols || !strings.HasSuffix(got[3], "TAIL") {
		t.Fatalf("long line cut from the wrong end: %q", got[3])
	}

	if got := stagePeekLinesOf("only\n\n\n"); len(got) != 1 || got[0] != "only" {
		t.Fatalf("blank tail: %q", got)
	}
}

// TestStageRailPanesFollowTheSplit keeps the cards in the layout's order and
// leaves the stage out; a single pane has no rail at all.
func TestStageRailPanesFollowTheSplit(t *testing.T) {
	a, b, c := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}
	s := &session{ID: "s", Root: &splitNode{
		axis:   axisHorizontal,
		ratio:  50,
		first:  leaf(a),
		second: &splitNode{axis: axisVertical, ratio: 50, first: leaf(b), second: leaf(c)},
	}}
	w := &workspace{}
	if got := w.stageRailPanes(s); got != nil {
		t.Fatalf("unzoomed rail: %v", paneIDs(got))
	}

	w.zoomedPane = "b"
	got := w.stageRailPanes(s)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("rail panes: %v", paneIDs(got))
	}

	one := &session{ID: "one", Root: leaf(a)}
	w.zoomedPane = "a"
	if got := w.stageRailPanes(one); got != nil {
		t.Fatalf("a lone pane has a rail: %v", paneIDs(got))
	}
}

// TestStagePaneSwapsTheStage: a card takes the stage and the pane it replaces
// joins the rail, focus included.
func TestStagePaneSwapsTheStage(t *testing.T) {
	a, b := &pane{ID: "a"}, &pane{ID: "b"}
	s := &session{ID: "s", Root: &splitNode{
		axis: axisHorizontal, ratio: 50, first: leaf(a), second: leaf(b),
	}}
	w := &workspace{
		Projects:    []*project{{Sessions: []*session{s}}},
		zoomedPane:  "a",
		FocusedPane: "a",
	}

	w.stagePane(b)
	if w.zoomedPane != "b" || w.FocusedPane != "b" || w.focusReq != "b" {
		t.Fatalf("staged %q, focus %q/%q", w.zoomedPane, w.FocusedPane, w.focusReq)
	}
	got := w.stageRailPanes(s)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("rail after the swap: %v", paneIDs(got))
	}
}

// TestStageRailHidesUnderTheDrawer: both live on the right edge, and the
// drawer is the one that covers it.
func TestStageRailHidesUnderTheDrawer(t *testing.T) {
	a, b := &pane{ID: "a"}, &pane{ID: "b"}
	s := &session{ID: "s", Root: &splitNode{
		axis: axisHorizontal, ratio: 50, first: leaf(a), second: leaf(b),
	}}
	w := &workspace{zoomedPane: "a"}
	if !w.stageRailVisible(s) {
		t.Fatal("the rail is hidden while idle")
	}
	w.drawer.open = true
	if w.stageRailVisible(s) {
		t.Fatal("the rail draws under the open drawer")
	}
}
