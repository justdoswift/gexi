package main

import (
	"slices"
	"testing"
)

func ids(n *splitNode) []string {
	var out []string
	for _, p := range n.panes() {
		out = append(out, p.ID)
	}
	return out
}

func TestSplitTree(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	c := &pane{ID: "c"}

	n := leaf(a)
	n = n.splitting("a", axisHorizontal, b)
	if got := ids(n); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("after first split: %v", got)
	}

	n = n.splitting("a", axisVertical, c)
	if got := ids(n); len(got) != 3 || got[0] != "a" || got[1] != "c" || got[2] != "b" {
		t.Fatalf("after nested split: %v", got)
	}

	n = n.removing("b")
	if got := ids(n); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("after removing b: %v", got)
	}

	n = n.removing("c")
	if got := ids(n); len(got) != 1 || got[0] != "a" {
		t.Fatalf("after removing c: %v", got)
	}

	if n.removing("a") != nil {
		t.Fatalf("empty tree should be nil")
	}
}

// TestSplitDirLeavesAWorktreeToItsPane pins the rule that a split beside a
// pane in a worktree gexi made starts in the project folder: two panes in
// one checkout would share a branch and index.
func TestSplitDirLeavesAWorktreeToItsPane(t *testing.T) {
	owned := &pane{ID: "owner", Worktree: &paneWorktree{
		Root: "/repo", Path: "/repo-checkout", Branch: "feature",
	}}
	if got := splitDirFor(owned, "/repo", "/repo-checkout/sub"); got != "/repo" {
		t.Fatalf("split beside a worktree started in %q, want the project folder /repo", got)
	}

	// A checkout the pane's own shell moved into is followed: that is where
	// the user is working.
	found := &pane{ID: "found", Worktree: &paneWorktree{Path: "/other", Detected: true}}
	if got := splitDirFor(found, "/repo", "/other"); got != "/other" {
		t.Fatalf("split beside a found checkout started in %q, want its own folder", got)
	}

	// No worktree, or a pane that left it, keeps the current folder.
	plain := &pane{ID: "plain"}
	if got := splitDirFor(plain, "/repo", "/repo/sub"); got != "/repo/sub" {
		t.Fatalf("split in a project folder started in %q, want its current folder", got)
	}
	if got := splitDirFor(owned, "/repo", "/tmp"); got != "/tmp" {
		t.Fatalf("split beside a pane that left its worktree started in %q, want its current folder", got)
	}
}

func TestMovePane(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	c := &pane{ID: "c"}

	n := leaf(a).splitting("a", axisHorizontal, b)
	// Dropped on a's left edge, b takes the slot before it.
	n = n.moving("b", "a", axisHorizontal, true)
	if got := ids(n); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("after moving b left of a: %v", got)
	}
	// Dropped on a's bottom edge, b goes below it.
	n = n.moving("b", "a", axisVertical, false)
	if got := ids(n); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("after moving b below a: %v", got)
	}
	if n.axis != axisVertical || n.first.pane.ID != "a" {
		t.Fatalf("the split is not a, then b, stacked")
	}

	// Moving a pane out of a nested split collapses its old slot, and the
	// remaining panes keep theirs.
	n = n.splitting("a", axisHorizontal, c)
	if got := ids(n); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Fatalf("after splitting a with c: %v", got)
	}
	n = n.moving("c", "b", axisVertical, true)
	if got := ids(n); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Fatalf("after moving c above b: %v", got)
	}

	// A pane cannot move onto itself, and an unknown id changes nothing.
	before := ids(n)
	if got := ids(n.moving("a", "a", axisHorizontal, false)); !slices.Equal(got, before) {
		t.Fatalf("moving a onto itself: %v", got)
	}
	if got := ids(n.moving("z", "a", axisHorizontal, false)); !slices.Equal(got, before) {
		t.Fatalf("moving an unknown pane: %v", got)
	}
}

func TestSwapPanes(t *testing.T) {
	a := &pane{ID: "a"}
	b := &pane{ID: "b"}
	c := &pane{ID: "c"}
	n := leaf(a).splitting("a", axisHorizontal, b).splitting("a", axisVertical, c)
	// a, c, b.
	if got := ids(n); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Fatalf("before swapping: %v", got)
	}
	n = n.swapping(a, b)
	if got := ids(n); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Fatalf("after swapping a and b: %v", got)
	}
	// The structure is untouched: three panes, two splits.
	if got := len(n.panes()); got != 3 || n.axis != axisHorizontal || n.first.axis != axisVertical {
		t.Fatalf("the swap changed the tree: %d panes", got)
	}
}

// TestThreePanesShareTheWidthEvenly checks a third pane splits the row into
// thirds rather than halving one of the halves. Two panes are 50/50; adding a
// third beside the second used to leave 50 / 25 / 25, which reads as one wide
// pane with two slivers rather than as three peers.
func TestThreePanesShareTheWidthEvenly(t *testing.T) {
	a, b, c := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}
	root := leaf(a)
	root = root.splitting("a", axisHorizontal, b)
	root = root.splitting("b", axisHorizontal, c)

	// The tree is (a | (b | c)) or ((a | b) | c); either way the shares
	// have to come out equal.
	shares := map[string]float32{}
	var walk func(n *splitNode, lo, hi float32)
	walk = func(n *splitNode, lo, hi float32) {
		if n == nil {
			return
		}
		if n.isLeaf() {
			shares[n.pane.ID] = hi - lo
			return
		}
		mid := lo + (hi-lo)*n.ratio/100
		walk(n.first, lo, mid)
		walk(n.second, mid, hi)
	}
	walk(root, 0, 100)

	for _, id := range []string{"a", "b", "c"} {
		if got := shares[id]; got < 33 || got > 34 {
			t.Fatalf("pane %s takes %.1f%% of the width; three panes should be thirds (all: %v)",
				id, got, shares)
		}
	}
}

// TestFourPanesKeepTheirGrid checks evening out does not flatten a grid: a
// two-by-two stays two rows of two, not four columns.
func TestFourPanesKeepTheirGrid(t *testing.T) {
	a, b, c, d := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}, &pane{ID: "d"}
	root := leaf(a)
	root = root.splitting("a", axisVertical, b)   // a over b
	root = root.splitting("a", axisHorizontal, c) // a | c, over b
	root = root.splitting("b", axisHorizontal, d) // a | c, over b | d
	if got := len(root.panes()); got != 4 {
		t.Fatalf("the tree holds %d panes, want 4", got)
	}
	// Every leaf still gets a quarter of the window.
	shares := map[string]float32{}
	var walk func(n *splitNode, w, h float32)
	walk = func(n *splitNode, w, h float32) {
		if n == nil {
			return
		}
		if n.isLeaf() {
			shares[n.pane.ID] = w * h
			return
		}
		if n.axis == axisHorizontal {
			walk(n.first, w*n.ratio/100, h)
			walk(n.second, w*(100-n.ratio)/100, h)
		} else {
			walk(n.first, w, h*n.ratio/100)
			walk(n.second, w, h*(100-n.ratio)/100)
		}
	}
	walk(root, 100, 100)
	for _, id := range []string{"a", "b", "c", "d"} {
		// w and h are percentages of the window, so their product is a
		// ten-thousandth of it: a quarter is 2500.
		if got := shares[id]; got < 2400 || got > 2600 {
			t.Fatalf("pane %s covers %.0f/10000 of the window; a 2x2 should be quarters (all: %v)",
				id, got, shares)
		}
	}
}

// TestThreePanesStackEvenly checks the rule holds on the other axis too. A
// vertical run inside a horizontal split is where a second row of panes
// lives, and stopping the walk at the first axis mismatch left every
// vertical run at 50/50.
func TestThreePanesStackEvenly(t *testing.T) {
	a, b, c := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}
	root := leaf(a)
	root = root.splitting("a", axisVertical, b)
	root = root.splitting("b", axisVertical, c)

	shares := map[string]float32{}
	var walk func(n *splitNode, lo, hi float32)
	walk = func(n *splitNode, lo, hi float32) {
		if n == nil {
			return
		}
		if n.isLeaf() {
			shares[n.pane.ID] = hi - lo
			return
		}
		mid := lo + (hi-lo)*n.ratio/100
		walk(n.first, lo, mid)
		walk(n.second, mid, hi)
	}
	walk(root, 0, 100)

	for _, id := range []string{"a", "b", "c"} {
		if got := shares[id]; got < 33 || got > 34 {
			t.Fatalf("pane %s takes %.1f%% of the height; three stacked panes should be thirds (all: %v)",
				id, got, shares)
		}
	}
}

// TestVerticalRunInsideHorizontalSplit checks the walk reaches a vertical run
// that hangs under a horizontal split. This is the case the first version
// never reached: it stopped at the root because the root's axis was the
// other one, so a second row of panes stayed at 50/50 no matter how many
// panes it held.
func TestVerticalRunInsideHorizontalSplit(t *testing.T) {
	a, b, c, d := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}, &pane{ID: "d"}
	root := leaf(a)
	root = root.splitting("a", axisHorizontal, b) // a | b
	root = root.splitting("b", axisVertical, c)   // a | (b over c)
	root = root.splitting("c", axisVertical, d)   // a | (b over c over d)

	// The right column holds three panes stacked, so each is a third of the
	// height. Under the old walk the column was never evened and kept
	// 50/25/25.
	heights := map[string]float32{}
	var walk func(n *splitNode, w, h float32)
	walk = func(n *splitNode, w, h float32) {
		if n == nil {
			return
		}
		if n.isLeaf() {
			heights[n.pane.ID] = h
			return
		}
		if n.axis == axisHorizontal {
			walk(n.first, w*n.ratio/100, h)
			walk(n.second, w*(100-n.ratio)/100, h)
		} else {
			walk(n.first, w, h*n.ratio/100)
			walk(n.second, w, h*(100-n.ratio)/100)
		}
	}
	walk(root, 100, 100)

	for _, id := range []string{"b", "c", "d"} {
		if got := heights[id]; got < 33 || got > 34 {
			t.Fatalf("pane %s takes %.1f%% of the height; three stacked in a column should be thirds (all: %v)",
				id, got, heights)
		}
	}
}
