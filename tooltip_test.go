package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestTooltipsReadOnTheChromeTheme: the chrome's theme paints no background,
// so the window's material shows where nothing is drawn. A tip writes on the
// inverse of that theme, whose text is the theme's background: with the
// transparent one every tip was a dark box with no words in it.
func TestTooltipsReadOnTheChromeTheme(t *testing.T) {
	pr := &project{ID: "p", Dir: t.TempDir(), Label: "gexi"}
	pr.Sessions = []*session{{ID: "s", Label: "Session", Root: leaf(&pane{ID: "pane"})}}
	w := &workspace{
		settings: defaultSettings(),
		Projects: []*project{pr},
		gitInfo:  map[string]gitSummary{},
		gitBusy:  map[string]bool{},
		gitGen:   map[string]int{},
	}
	old := sidebarMaterial
	sidebarMaterial = true
	defer func() { sidebarMaterial = old }()

	tester := ui.NewTester(w.chromeView, 760, 900)
	tester.SetDark(false)
	tester.SetVibrancy(true)
	tester.Frame()
	row, ok := tester.Find("Session")
	if !ok {
		t.Fatalf("the row is not there; texts = %q", tester.Texts())
	}
	tester.Move(sidebarWidth-25, row.Y+row.H/2)
	tester.Advance(700 * time.Millisecond)
	tester.Frame()

	// The tip is the one patch of the theme's text colour above the row. What
	// inside it is not that colour, two pixels in from its edge, is the words
	// it shows: the fill alone is a box with nothing written on it.
	img := tester.Image()
	fill := ui.LightTheme().Text
	isFill := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return uint8(r>>8) == fill.R && uint8(g>>8) == fill.G && uint8(b>>8) == fill.B
	}
	x0, y0, x1, y1 := img.Bounds().Max.X, img.Bounds().Max.Y, 0, 0
	for y := int(row.Y) - 40; y < int(row.Y); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if !isFill(x, y) {
				continue
			}
			x0, y0 = min(x0, x), min(y0, y)
			x1, y1 = max(x1, x), max(y1, y)
		}
	}
	if x1 <= x0 {
		t.Fatalf("no tip above the row at %v", row)
	}
	words := 0
	for y := y0 + 2; y < y1-1; y++ {
		for x := x0 + 2; x < x1-1; x++ {
			if !isFill(x, y) {
				words++
			}
		}
	}
	if words == 0 {
		t.Fatalf("the tip at %d,%d-%d,%d shows no words", x0, y0, x1, y1)
	}
}
