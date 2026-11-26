package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// Panes move between slots by dragging their header: a drop on a side of
// another pane splits that pane toward the side, and a drop in its middle
// swaps the two.

// paneMoved is the value a pane header drags. The pointer carries it while
// the drag lasts, and the pane it names is the one a drop moves.
type paneMoved struct {
	Pane *pane
}

// paneDropPlace is where on a pane a dragged one was let go.
type paneDropPlace int

const (
	paneDropNone paneDropPlace = iota
	paneDropLeft
	paneDropRight
	paneDropTop
	paneDropBottom
	paneDropCenter
)

// dropEdge is how much of a pane, as a fraction of each side, splits toward
// that side. The middle left over swaps.
const dropEdge = 0.3

// paneDropPlaceAt reads a drop at (x, y) in a pane box of w by h. In a
// corner the nearer edge wins, so every drop outside the middle means
// something.
func paneDropPlaceAt(x, y, w, h float32) paneDropPlace {
	if w <= 0 || h <= 0 {
		return paneDropNone
	}
	fx, fy := x/w, y/h
	left, right := fx, 1-fx
	top, bottom := fy, 1-fy
	nearest := min(min(left, right), min(top, bottom))
	switch {
	case nearest > dropEdge:
		return paneDropCenter
	case nearest == left:
		return paneDropLeft
	case nearest == right:
		return paneDropRight
	case nearest == top:
		return paneDropTop
	default:
		return paneDropBottom
	}
}

// dropPlaceOf reads where the pointer lets a pane go over el, or
// paneDropNone when it is not a pane other than target.
func dropPlaceOf(el ui.Element, dragged, target *pane) paneDropPlace {
	if dragged == nil || dragged == target {
		return paneDropNone
	}
	// Bounds is the box body, padding included, which is what the pointer's
	// own position is measured in.
	b := el.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return paneDropNone
	}
	x, y, _ := el.PointerPosition()
	return paneDropPlaceAt(x, y, b.W, b.H)
}

// movePane carries out a drop: dragged moves into the slot place describes
// next to target, or trades places with it in the middle. Main thread.
func (w *workspace) movePane(dragged, target *pane, place paneDropPlace) {
	if dragged == nil || target == nil || dragged == target {
		return
	}
	s := w.sessionOf(target)
	if s == nil || s.Root == nil || w.sessionOf(dragged) != s {
		return
	}
	switch place {
	case paneDropLeft:
		s.Root = s.Root.moving(dragged.ID, target.ID, axisHorizontal, true)
	case paneDropRight:
		s.Root = s.Root.moving(dragged.ID, target.ID, axisHorizontal, false)
	case paneDropTop:
		s.Root = s.Root.moving(dragged.ID, target.ID, axisVertical, true)
	case paneDropBottom:
		s.Root = s.Root.moving(dragged.ID, target.ID, axisVertical, false)
	case paneDropCenter:
		s.Root = s.Root.swapping(dragged, target)
	default:
		return
	}
	// A pane dropped while the session is zoomed takes the stage, so the
	// move it made is the one on screen.
	w.showPane(dragged)
	w.FocusedPane = dragged.ID
	w.focusReq = dragged.ID
	w.invalidate()
	w.scheduleSave()
}

// paneRects lays a session's panes out in a unit square, as the split view
// draws them, so a keyboard move can find the pane beside another.
func (n *splitNode) paneRects(x, y, w, h float32, out map[*pane]ui.Rect) {
	if n == nil {
		return
	}
	if n.isLeaf() {
		out[n.pane] = ui.Rect{X: x, Y: y, W: w, H: h}
		return
	}
	r := n.ratio / 100
	if n.axis == axisHorizontal {
		n.first.paneRects(x, y, w*r, h, out)
		n.second.paneRects(x+w*r, y, w*(1-r), h, out)
		return
	}
	n.first.paneRects(x, y, w, h*r, out)
	n.second.paneRects(x, y+h*r, w, h*(1-r), out)
}

// swapPaneIn trades p with the nearest pane in the direction of dx or dy,
// as Option-Cmd-arrow moves a pane between splits: the keyboard's way to
// do what dragging a pane header does.
func (w *workspace) swapPaneIn(p *pane, dx, dy int) bool {
	s := w.sessionOf(p)
	if s == nil || s.Root == nil {
		return false
	}
	rects := map[*pane]ui.Rect{}
	s.Root.paneRects(0, 0, 1, 1, rects)
	r, ok := rects[p]
	if !ok {
		return false
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	var best *pane
	bestDist := float32(1e9)
	for q, qr := range rects {
		if q == p {
			continue
		}
		qx, qy := qr.X+qr.W/2, qr.Y+qr.H/2
		var along, across float32
		if dx != 0 {
			along = float32(dx) * (qx - cx)
			across = qy - cy
			if along <= 0.001 || abs32(across) > (r.H+qr.H)/2 {
				continue
			}
		} else {
			along = float32(dy) * (qy - cy)
			across = qx - cx
			if along <= 0.001 || abs32(across) > (r.W+qr.W)/2 {
				continue
			}
		}
		if d := along + abs32(across)*0.5; d < bestDist {
			bestDist, best = d, q
		}
	}
	if best == nil {
		return false
	}
	s.Root = s.Root.swapping(p, best)
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
	w.scheduleSave()
	return true
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// A drop hint is the one blue in a neutral window, so it reads as an action
// on either theme: dropAccent for its border and label, dropTint for the
// wash over the slot the pane would take.
var (
	dropAccent = ui.RGB(0x3b, 0x82, 0xf6)
	dropTint   = ui.RGBA(0x3b, 0x82, 0xf6, 0.16)
)

// paneDropHint paints the half a dragged pane would take, or the whole pane
// when it would trade places, with the label of what letting go does. It is
// ignored by the pointer so the pane under it stays the drop target.
func paneDropHint(c *ui.Context, place paneDropPlace, card bool) {
	hint := ui.Box(c).Absolute().Left(0).Top(0).IgnoreInput(true)
	switch place {
	case paneDropLeft:
		hint = hint.WidthPercent(50).HeightPercent(100)
	case paneDropRight:
		hint = hint.LeftPercent(50).WidthPercent(50).HeightPercent(100)
	case paneDropTop:
		hint = hint.WidthPercent(100).HeightPercent(50)
	case paneDropBottom:
		hint = hint.TopPercent(50).WidthPercent(100).HeightPercent(50)
	case paneDropCenter:
		hint = hint.WidthPercent(100).HeightPercent(100)
	default:
		return
	}
	// Round only the corners the pane card itself rounds for this slot: the
	// hint has to follow the card's corner curve, while the edge it splits
	// along stays square. A compact pane has no card, so neither does it.
	r := float32(0)
	if card {
		r = windowRadius
	}
	var radii [4]float32
	switch place {
	case paneDropLeft:
		radii = [4]float32{r, 0, 0, r}
	case paneDropRight:
		radii = [4]float32{0, r, r, 0}
	case paneDropTop:
		radii = [4]float32{r, r, 0, 0}
	case paneDropBottom:
		radii = [4]float32{0, 0, r, r}
	default:
		radii = [4]float32{r, r, r, r}
	}
	hint.Radius(radii[0], radii[1], radii[2], radii[3]).
		Background(dropTint).Border(1, dropAccent).Children(func() {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Row(c).Background(dropAccent).Radius(chromeRadius).Padding(4, 9).Children(func() {
				ui.Text(c, paneDropLabel(place)).FontSize(11).FontWeight(500).
					TextColor(ui.RGB(0xff, 0xff, 0xff))
			})
		})
	})
}

func paneDropLabel(place paneDropPlace) string {
	switch place {
	case paneDropLeft:
		return locales.DropSplitLeft()
	case paneDropRight:
		return locales.DropSplitRight()
	case paneDropTop:
		return locales.DropSplitUp()
	case paneDropBottom:
		return locales.DropSplitDown()
	default:
		return locales.DropSwapPanes()
	}
}
