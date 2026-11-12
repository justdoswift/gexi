package main

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestPaneWorkingReadsHooksSubagentsAndTitles covers what puts a beam on a
// pane: a turn that has not ended, a background subagent, or a title spinner
// that keeps moving. A mark that nothing backs up lapses.
func TestPaneWorkingReadsHooksSubagentsAndTitles(t *testing.T) {
	p := &pane{}
	if p.working() {
		t.Fatal("an idle pane is working")
	}
	p.outAt = time.Now()
	p.activity = &agentActivity{State: activityWorking}
	if !p.working() {
		t.Fatal("a running turn is not working")
	}
	p.outAt = time.Now().Add(-activityQuiet - time.Second)
	if p.working() {
		t.Fatal("a stale turn still counts as working")
	}
	p.subagents = map[string]paneSubagent{"a": {}}
	if !p.working() {
		t.Fatal("a running subagent does not count as working")
	}
}

func beamAt(dark bool, phase float32, still bool) *image.RGBA {
	return beamFrame(dark, phase, still, true)
}

// beamFrame draws the card with or without the light, so a test can compare
// the two.
func beamFrame(dark bool, phase float32, still, lit bool) *image.RGBA {
	tk := defaultTokens(dark, ui.RGB(0xac, 0x46, 0xea))
	now := time.Unix(0, int64(float64(beamCycle)*float64(phase)))
	view := func(c *ui.Context) {
		ui.Column(c).Fill().Background(ui.RGB(0x0e, 0x0e, 0x10)).Padding(20).Children(func() {
			card := ui.Box(c).Width(300).Height(200).Radius(windowRadius).Clip().
				Background(tk.Background)
			if lit {
				card.DrawOver(func(p *ui.Painter, r ui.Rect) {
					paintPaneBeam(p, r, tk, true, now, still, time.Time{})
				})
			}
		})
	}
	tester := ui.NewTester(view, 340, 240)
	tester.Frame()
	return tester.Image()
}

func beamPixel(img *image.RGBA, x, y int) (r, g, b int) {
	o := img.PixOffset(x, y)
	return int(img.Pix[o]), int(img.Pix[o+1]), int(img.Pix[o+2])
}

func beamLuma(img *image.RGBA, x, y int) int {
	r, g, b := beamPixel(img, x, y)
	return r + g + b
}

// TestBeamTravelsTheBorder draws the pair at two points of its revolution
// and checks that each side takes the beam in turn: the right edge is red
// early, when the first beam is there, and blue late, when the second has
// come round to it. The top stays dark in between.
func TestBeamTravelsTheBorder(t *testing.T) {
	early, late := beamAt(true, 0.25, false), beamAt(true, 0.75, false)
	right := func(img *image.RGBA) (r, g, b int) { return beamPixel(img, 318, 120) }
	if r, _, b := right(early); r <= b {
		t.Fatalf("the right edge is not red early: %d, %d", r, b)
	}
	if r, _, b := right(late); b <= r {
		t.Fatalf("the right edge is not blue late: %d, %d", r, b)
	}
	if beamLuma(early, 170, 21) != beamLuma(late, 170, 21) {
		t.Fatal("a beam crossed the top between the two points")
	}
}

// TestBeamStaysOnTheBorder draws the beams over a card and checks that they
// light its border and nothing inside or outside it.
func TestBeamStaysOnTheBorder(t *testing.T) {
	lit := beamAt(true, 0.25, false)
	rest := beamFrame(true, 0.25, false, false)
	if beamLuma(lit, 318, 120) <= beamLuma(rest, 318, 120) {
		t.Fatal("the beam does not light the border")
	}
	for _, p := range [][2]int{{170, 120}, {170, 40}, {40, 120}, {10, 120}, {330, 120}, {170, 230}} {
		if beamLuma(lit, p[0], p[1]) != beamLuma(rest, p[0], p[1]) {
			t.Fatalf("the light reached %v, off the border", p)
		}
	}
}

// TestBeamHoldsStillWithoutMotion: the desktop's reduced-motion form keeps
// the light in one place, so two frames of different times are the same.
func TestBeamHoldsStillWithoutMotion(t *testing.T) {
	a, b := beamAt(true, 0.2, true), beamAt(true, 0.8, true)
	for x := 20; x < 320; x += 7 {
		for y := 20; y < 220; y++ {
			if beamLuma(a, x, y) != beamLuma(b, x, y) {
				t.Fatalf("the still light moved between frames at %d,%d", x, y)
			}
		}
	}
	if beamLuma(a, 318, 120) <= 0 {
		t.Fatal("the still light is not drawn")
	}
}

// TestBeamDrawsOnALightSurface covers the light appearance: the beam is a
// coloured shadow there, so it must darken the bottom edge, keep some of its
// colour, and stay inside the card as the dark one does.
func TestBeamDrawsOnALightSurface(t *testing.T) {
	lit := beamFrame(false, 0.25, false, true)
	rest := beamFrame(false, 0.25, false, false)
	if beamLuma(lit, 318, 120) >= beamLuma(rest, 318, 120) {
		t.Fatalf("the light beam does not darken the border: %d vs %d",
			beamLuma(lit, 318, 120), beamLuma(rest, 318, 120))
	}
	r, g, b := beamPixel(lit, 318, 120)
	if max(r, max(g, b))-min(r, min(g, b)) <= 8 {
		t.Fatal("the light beam lost its colour on a light surface")
	}
	for _, p := range [][2]int{{170, 120}, {170, 40}, {40, 120}, {10, 120}, {330, 120}, {170, 230}} {
		if beamLuma(lit, p[0], p[1]) != beamLuma(rest, p[0], p[1]) {
			t.Fatalf("the light reached %v, off the border", p)
		}
	}
}
