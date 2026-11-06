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
