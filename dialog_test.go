package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestDialogFrameEasesInAndOut checks the shared dialog panel and its dimming
// backdrop arrive together and leave as copies, so opening or dismissing a
// dialog is a layer coming over the work rather than a frame flipped.
func TestDialogFrameEasesInAndOut(t *testing.T) {
	open := false
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			dialogFrame(c, tk, &open, 360, func() {
				dialogHeading(c, tk, "Title", "One line about it")
				dialogBody(c, func() {
					ui.Text(c, "Body").FontSize(13).TextColor(tk.Foreground)
				})
			})
		})
	}, 800, 600)
	tester.Frame()
	if got := inkPixels(tester.Image()); got != 0 {
		t.Fatalf("a closed dialog drew %d pixels", got)
	}

	open = true
	tester.Frame()
	first := inkPixels(tester.Image())
	tester.Advance(dialogMotion / 2)
	mid := inkPixels(tester.Image())
	tester.Advance(dialogMotion)
	settled := inkPixels(tester.Image())
	if mid <= first || mid >= settled {
		t.Fatalf("the dialog has no fade: first %d, mid %d, settled %d", first, mid, settled)
	}

	open = false
	tester.Frame()
	during := inkPixels(tester.Image())
	if during == 0 {
		t.Fatal("the dialog vanished in the frame it was dismissed")
	}
	tester.Advance(dialogMotion)
	if got := inkPixels(tester.Image()); got != 0 {
		t.Fatalf("the dialog is still drawn after its exit: %d pixels", got)
	}
}
