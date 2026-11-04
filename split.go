package main

// splitNode mirrors JustCli's binary split tree: leaves are panes, every
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
	target := n.paneByID(id)
	if target == nil {
		return n
	}
	return n.replacing(id, &splitNode{
		axis:   ax,
		ratio:  50,
		first:  leaf(target),
		second: leaf(p),
	})
}
