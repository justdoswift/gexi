package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

func TestPaneDropPlaceAt(t *testing.T) {
	const w, h = 400, 300
	cases := []struct {
		x, y float32
		want paneDropPlace
	}{
		{20, 150, paneDropLeft},
		{380, 150, paneDropRight},
		{200, 20, paneDropTop},
		{200, 280, paneDropBottom},
		{200, 150, paneDropCenter},
		// In a corner the nearer edge wins.
		{30, 20, paneDropTop},
		{20, 40, paneDropLeft},
		{380, 280, paneDropRight},
		// A corner only splits: the swap is the middle wherever the pane is.
		{150, 120, paneDropCenter},
	}
	for _, c := range cases {
		if got := paneDropPlaceAt(c.x, c.y, w, h); got != c.want {
			t.Errorf("drop at (%.0f, %.0f): got %v, want %v", c.x, c.y, got, c.want)
		}
	}
	if got := paneDropPlaceAt(10, 10, 0, 0); got != paneDropNone {
		t.Errorf("a pane without a box: %v", got)
	}
}

// A dragged title reports the side of the pane it is over, and still knows
// the place when it is let go: the drop frame has no drag left, so the
// pointer, not the drag, tells it.
func TestPaneDropReadsThePointer(t *testing.T) {
	source := &pane{ID: "a"}
	target := &pane{ID: "b"}
	over, at := paneDropNone, paneDropNone
	dropped := 0

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Padding(20).Gap(10).Children(func() {
			ui.Box(c).Size(80, 60).Label("source").Drag(paneMoved{Pane: source})
			paneEl := ui.Box(c).Size(200, 120).Label("target")
			if v, ok := ui.DragOver[paneMoved](paneEl); ok {
				over = dropPlaceOf(paneEl, v.Pane, target)
			}
			if v, ok := ui.Drop[paneMoved](paneEl); ok {
				dropped++
				at = dropPlaceOf(paneEl, v.Pane, target)
			}
		})
	}, 400, 300)

	src, ok := tt.Find("source")
	if !ok {
		t.Fatal("no source")
	}
	dst, ok := tt.Find("target")
	if !ok {
		t.Fatal("no target")
	}
	tt.Press(src.X+10, src.Y+10)
	tt.Move(dst.X+dst.W-20, dst.Y+dst.H/2)
	if over != paneDropRight {
		t.Errorf("over the right edge: %v", over)
	}
	tt.Release(dst.X+dst.W-20, dst.Y+dst.H/2)
	if dropped != 1 || at != paneDropRight {
		t.Errorf("dropped %d times at %v", dropped, at)
	}

	// A drop in the middle swaps, and reads the same side while over it.
	tt.Press(src.X+10, src.Y+10)
	tt.Move(dst.X+dst.W/2, dst.Y+dst.H/2)
	if over != paneDropCenter {
		t.Errorf("over the middle: %v", over)
	}
	tt.Release(dst.X+dst.W/2, dst.Y+dst.H/2)
	if dropped != 2 || at != paneDropCenter {
		t.Errorf("dropped %d times at %v", dropped, at)
	}
}

// The hint covers the half the pane would move to, so the label reads on
// that side, and the pane under it stays the drop target.
func TestPaneDropHintCoversItsSide(t *testing.T) {
	build := func(place paneDropPlace) *ui.Tester {
		return ui.NewTester(func(c *ui.Context) {
			root := ui.Box(c).Size(400, 300).Label("root")
			root.Children(func() { paneDropHint(c, place, false) })
		}, 400, 300)
	}
	find := func(place paneDropPlace, label string) (ui.Rect, bool) {
		return build(place).Find(label)
	}
	if r, ok := find(paneDropLeft, locales.DropSplitLeft()); !ok || r.X+r.W/2 > 200 {
		t.Errorf("left hint label at %.0f, want the left half", r.X+r.W/2)
	}
	if r, ok := find(paneDropRight, locales.DropSplitRight()); !ok || r.X+r.W/2 < 200 {
		t.Errorf("right hint label at %.0f, want the right half", r.X+r.W/2)
	}
	if r, ok := find(paneDropTop, locales.DropSplitUp()); !ok || r.Y+r.H/2 > 150 {
		t.Errorf("top hint label at %.0f, want the top half", r.Y+r.H/2)
	}
	if r, ok := find(paneDropBottom, locales.DropSplitDown()); !ok || r.Y+r.H/2 < 150 {
		t.Errorf("bottom hint label at %.0f, want the bottom half", r.Y+r.H/2)
	}
	if r, ok := find(paneDropCenter, locales.DropSwapPanes()); !ok || r.X+r.W/2 < 180 || r.X+r.W/2 > 220 || r.Y+r.H/2 < 130 || r.Y+r.H/2 > 170 {
		t.Errorf("swap hint label at (%.0f, %.0f), want the middle", r.X+r.W/2, r.Y+r.H/2)
	}
}

// The hint's border follows the card's rounded corner instead of cutting
// across it, and stays square on the edge it would split along.
func TestPaneDropHintFollowsTheCardCorner(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(20).Children(func() {
			card := ui.Box(c).Size(160, 120).Radius(16).Clip().
				Background(ui.RGB(0xff, 0xff, 0xff)).Border(1, ui.RGB(0xe5, 0xe5, 0xe5))
			card.Children(func() { paneDropHint(c, paneDropLeft, true) })
		})
	}, 200, 150)
	img := tt.Image()
	isAccent := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return b > 0x8000 && b > r+0x3000 && g < b
	}
	// Outside the card's corner arc, where a square hint would show.
	if isAccent(22, 22) {
		t.Errorf("the hint paints outside the card's corner at (22, 22)")
	}
	// On the hint's top border, away from the corner.
	if !isAccent(60, 21) {
		t.Errorf("the hint's border is missing on its top edge")
	}
}
