package main

import (
	"image"
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

// TestDialogFooterCarriesOneFilledAction checks the footer never shows two
// saturated pills: where a primary action is present, a destructive one
// steps back to the quiet glass so the eye knows which button Return takes.
func TestDialogFooterCarriesOneFilledAction(t *testing.T) {
	draw := func(actions ...dialogAction) *image.RGBA {
		tester := ui.NewTester(func(c *ui.Context) {
			tk := themeTokens(c.Theme())
			ui.Column(c).Fill().Background(tk.Background).Children(func() {
				ui.Column(c).Width(420).Padding(0).Background(tk.Background).Children(func() {
					dialogFooter(c, tk, actions...)
				})
			})
		}, 520, 120)
		tester.Frame()
		tester.Advance(dialogMotion * 3)
		return tester.Image()
	}
	// A saturated fill is the danger red. The demoted button keeps that red
	// on its words, so what tells the two cases apart is the area: a filled
	// pill covers a couple of thousand pixels, the words a hundred or so.
	redPixels := func(img *image.RGBA) int {
		n := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a == 0 {
					continue
				}
				if r>>8 > 150 && g>>8 < 110 && b>>8 < 110 {
					n++
				}
			}
		}
		return n
	}
	const fill = 500

	alone := draw(
		dialogAction{label: "Cancel", kind: dialogQuiet},
		dialogAction{label: "Delete", kind: dialogDanger},
	)
	if got := redPixels(alone); got < fill {
		t.Fatalf("a destructive action on its own lost its danger fill: %d red pixels", got)
	}

	besidePrimary := draw(
		dialogAction{label: "Cancel", kind: dialogQuiet},
		dialogAction{label: "Close", kind: dialogDanger},
		dialogAction{label: "Keep Running", kind: dialogPrimary, auto: true},
	)
	if got := redPixels(besidePrimary); got >= fill {
		t.Fatalf("a destructive action beside a primary one still fills %d red pixels", got)
	}
}



// TestDialogPanelSitsAboveThePage checks the panel reads as its own sheet of
// paper rather than as a slightly dimmed copy of what it covers. The glass is
// a GPU effect, so a headless frame cannot show it; this composes what it
// draws from the tint dialogPaper returns: the page, dimmed by the backdrop,
// with that tint laid over at the opacity glass.paint uses (0.88 of the
// tint's alpha). A nearly clear tint leaves the panel within a few levels of
// the page, which is what read as grey and muddy; a sheet of paper sits
// clearly above it.
func TestDialogPanelSitsAboveThePage(t *testing.T) {
	// How far above the page the panel has to sit to read as raised. The
	// old near-clear tint came in under 4 in both appearances. Dark mode
	// tops out near 17: its paper and its canvas are only a few levels
	// apart to begin with.
	const raised = 10.0
	for _, dark := range []bool{false, true} {
		tk := defaultTokens(dark, ui.LightTheme().Accent)
		shade := float32(0.12)
		if dark {
			shade = 0.24
		}
		page := float32(tk.Muted.R) * (1 - shade)
		paper := dialogPaper(tk)
		a := float32(paper.A) / 255 * 0.88
		panel := float32(paper.R)*a + page*(1-a)
		if panel-page < raised {
			t.Fatalf("dark=%v: the panel (%.1f) sits only %.1f above the page (%.1f); it reads as a dimmed copy, not a sheet",
				dark, panel, panel-page, page)
		}
	}
}
