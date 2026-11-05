package main

// splitNode mirrors Gexi's binary split tree: leaves are panes, every
// split keeps its axis and ratio so layout survives edits.
type splitNode struct {
	pane          *pane
	axis          axis
	ratio         float32 // percent the first side takes, 0–100
	first, second *splitNode
}

type axis int

const (
	axisHorizontal axis = iota // side by side
	axisVertical               // stacked
)

func leaf(p *pane) *splitNode { return &splitNode{pane: p} }

func (n *splitNode) isLeaf() bool { return n.pane != nil }

func (n *splitNode) panes() []*pane {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return []*pane{n.pane}
	}
	return append(n.first.panes(), n.second.panes()...)
}

func (n *splitNode) paneByID(id string) *pane {
	for _, p := range n.panes() {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// replacing swaps the leaf holding id with repl.
func (n *splitNode) replacing(id string, repl *splitNode) *splitNode {
	if n.isLeaf() {
		if n.pane.ID == id {
			return repl
		}
		return n
	}
	n.first = n.first.replacing(id, repl)
	n.second = n.second.replacing(id, repl)
	return n
}

// removing drops a leaf and collapses the split it leaves behind. nil means
// the tree is empty.
func (n *splitNode) removing(id string) *splitNode {
	if n.isLeaf() {
		if n.pane.ID == id {
			return nil
		}
		return n
	}
	first, second := n.first.removing(id), n.second.removing(id)
	switch {
	case first == nil && second == nil:
		return nil
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		n.first, n.second = first, second
		return n
	}
}

// splitting puts p after the leaf holding id.
func (n *splitNode) splitting(id string, ax axis, p *pane) *splitNode {
	return n.splittingSide(id, ax, p, false)
}

// splittingSide puts p before or after the leaf holding id. A drop on a
// pane's left or top edge lands before it, its right or bottom edge after.
func (n *splitNode) splittingSide(id string, ax axis, p *pane, before bool) *splitNode {
	target := n.paneByID(id)
	if target == nil {
		return n
	}
	first, second := leaf(target), leaf(p)
	if before {
		first, second = second, first
	}
	return n.replacing(id, &splitNode{
		axis:   ax,
		ratio:  50,
		first:  first,
		second: second,
	})
}

// moving takes the leaf holding id out of the tree and splits the leaf
// holding onto with it. A pane cannot move onto itself, and an unknown id
// leaves the tree as it is.
func (n *splitNode) moving(id string, onto string, ax axis, before bool) *splitNode {
	if id == onto {
		return n
	}
	p := n.paneByID(id)
	if p == nil || n.paneByID(onto) == nil {
		return n
	}
	// Drop the pane before splitting, so the split lands in the tree that is
	// left: its own old slot has collapsed by then.
	rest := n.removing(id)
	if rest == nil {
		return n
	}
	return rest.splittingSide(onto, ax, p, before)
}

// swapping trades the panes of two leaves, which is what a drop in the
// middle of another pane asks for. The split structure and its ratios stay
// where they are; only what lives in each slot changes.
func (n *splitNode) swapping(a, b *pane) *splitNode {
	if n == nil || a == nil || b == nil || a == b {
		return n
	}
	if n.isLeaf() {
		switch n.pane {
		case a:
			return leaf(b)
		case b:
			return leaf(a)
		}
		return n
	}
	n.first = n.first.swapping(a, b)
	n.second = n.second.swapping(a, b)
	return n
}
