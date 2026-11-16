package main

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestScratchCheckpointAlign(t *testing.T) {
	p := &pane{Worktree: &paneWorktree{
		Path: t.TempDir(), Branch: "underlord", Base: "main",
		busy:      worktreeBusyPR,
		busySteps: []worktreeStep{stepCommit, stepWritePR},
		busyAt:    stepCommit,
	}}
	w := &workspace{}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			w.worktreeCheckpoints(c, tk, p)
		})
	}
	tt := ui.NewTester(view, 480, 300)
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(checkpointMotion)
	tt.Frame()
	img := tt.Image()
	f, err := os.Create("/tmp/checkpoint-align.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}

	// Find the accent dot: the pixel cluster whose red is well above blue.
	minX, minY, maxX, maxY := 1<<30, 1<<30, -1, -1
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a > 0 && r>>8 > 120 && b>>8 > 60 && g>>8 < 90 && r>>8 > g>>8+60 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	t.Logf("dot box x %d..%d y %d..%d center %d", minX, maxX, minY, maxY, (minY+maxY)/2)

	row, ok := tt.Find("正在生成提交信息")
	if !ok {
		row, ok = tt.Find("Preparing the commit message")
	}
	if !ok {
		t.Logf("texts: %q", tt.Texts())
		return
	}
	t.Logf("text box x %.1f..%.1f y %.1f..%.1f center %.1f", row.X, row.X+row.W, row.Y, row.Y+row.H, row.Y+row.H/2)
}
