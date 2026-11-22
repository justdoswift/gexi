package main

import (
	"image"
	"os"
	"strings"
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



// TestDialogPanelIsTheSystemMaterial checks the panel is made of the system's
// own glass rather than a painted fill. A dialog is the largest small surface
// the app draws, and an opaque paper with a hairline round it was what made it
// read as a white board laid over the work; the material frosts what is behind
// it and lights its own rim.
func TestDialogPanelIsTheSystemMaterial(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tk := defaultTokens(dark, ui.LightTheme().Accent)
		g := dialogGlass(tk)
		// The system's own material, as a notification banner is. The shader
		// frosts at a control's density, which over a light page comes out a
		// flat grey board.
		if !g.Native {
			t.Fatalf("dark=%v: the dialog is not the system's material", dark)
		}
		// The tint has to stay light: the material already tones what is
		// behind it, and a dense tint would make the panel opaque again.
		if a := float32(g.Tint.A) / 255; a > 0.75 {
			t.Fatalf("dark=%v: the dialog's tint is %.2f, dense enough to hide the material", dark, a)
		}
		if g.Clarity != 0 {
			t.Fatalf("dark=%v: the dialog asks for clarity %v; at this size the frost is the point", dark, g.Clarity)
		}
	}
}

// TestDialogButtonsAreNotGlass checks the footer's actions are flat fills.
//
// The panel behind them is the system's material, which paints a hole in the
// window. A shader glass on top of that hole has nothing to sample: its frost
// maps the transparency to a flat grey, which is what turned all three
// buttons into grey slabs. A control on a sheet of glass is a control.
func TestDialogButtonsAreNotGlass(t *testing.T) {
	src, err := os.ReadFile("dialog.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func dialogButton(")
	if start < 0 {
		t.Fatal("dialogButton is gone")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("dialogButton has no end")
	}
	fn := body[start : start+end]
	if strings.Contains(fn, "Material(") {
		t.Fatalf("dialogButton sets a material again; its glass samples the panel's hole and comes out grey:\n%s", fn)
	}
	// And the destructive one wears system red, not the theme's ANSI red.
	if !strings.Contains(fn, "systemDanger(") {
		t.Fatal("dialogButton does not use systemDanger: a theme's ANSI red is a muted brick, not system red")
	}
}
