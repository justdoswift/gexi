package main

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestHaloEffectLastsItsDuration covers when the sweep is on: nothing at
// rest, the full second after a task ends, and the shorter fade the desktop
// asks for when it asks for less motion.
func TestHaloEffectLastsItsDuration(t *testing.T) {
	tk := defaultTokens(false, ui.RGB(0xac, 0x46, 0xea))
	p := &pane{}
	var running bool
	tt := ui.NewTester(func(c *ui.Context) {
		running = p.haloEffect(c, tk) != nil
	}, 20, 20)

	if running {
		t.Fatal("a pane that has not finished anything lit up")
	}

	p.haloStart = tt.Now()
	tt.Frame()
	tt.Advance(500 * time.Millisecond)
	if !running {
		t.Fatal("the sweep was gone halfway through its second")
	}
	tt.Advance(600 * time.Millisecond)
	if running || !p.haloStart.IsZero() {
		t.Fatal("the sweep outlived its second")
	}

	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	p.haloStart = tt.Now()
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if !running {
		t.Fatal("the still light was gone halfway through its 600ms")
	}
	tt.Advance(400 * time.Millisecond)
	if running {
		t.Fatal("the still light outlived its 600ms")
	}
}

// TestHaloStaysInsideItsPane draws the effect over one card and checks that
// it changes that card and nothing around it: the chrome beside and below a
// pane never takes the light.
func TestHaloStaysInsideItsPane(t *testing.T) {
	tk := defaultTokens(false, ui.RGB(0xac, 0x46, 0xea))
	frame := func(at float32) *image.RGBA {
		view := func(c *ui.Context) {
			ui.Column(c).Fill().Background(ui.RGB(0x08, 0x08, 0x0a)).Padding(20).Children(func() {
				ui.Box(c).Width(300).Height(180).Background(tk.Background).Radius(6).Clip().Children(func() {
					ui.Column(c).Fill().Effect(haloAt(at, tk, false)).Padding(12).Children(func() {
						ui.Text(c, "done").FontSize(13).TextColor(tk.Foreground)
					})
				})
			})
		}
		return ui.NewTester(view, 360, 260).Image()
	}
	rest, lit := frame(0), frame(0.12)
	at := func(img *image.RGBA, x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3]}
	}
	for _, p := range [][2]int{{5, 5}, {350, 250}, {350, 5}, {5, 250}} {
		if at(rest, p[0], p[1]) != at(lit, p[0], p[1]) {
			t.Fatalf("the light reached %v, outside the pane", p)
		}
	}
	if at(rest, 180, 40) == at(lit, 180, 40) {
		t.Fatal("the light did not reach the pane's own top")
	}
}
