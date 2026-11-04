package main

import "testing"

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
