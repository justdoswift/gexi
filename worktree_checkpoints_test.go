package main

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestWorktreeCheckpointsRevealStagesInOrder checks a running pull request
// states the stages it has reached, adding one line at a time, and leaves the
// counter out of the card.
func TestWorktreeCheckpointsRevealStagesInOrder(t *testing.T) {
	p := &pane{Worktree: &paneWorktree{
		Path: t.TempDir(), Branch: "weaver", Base: "main",
		busy:      worktreeBusyPR,
		busySteps: []worktreeStep{stepCommit, stepWritePR, stepPush, stepCreatePR, stepReadPR},
		busyAt:    stepPush,
	}}
	w := &workspace{}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			w.worktreeCheckpoints(c, tk, p)
		})
	}
	tester := ui.NewTester(view, 480, 300)
	tester.Frame()
	want := []string{
		locales.WorktreePROpening(),
		locales.WorktreeStepCommit(),
		locales.WorktreeStepWritePR(),
		locales.WorktreeStepPush(),
	}
	for _, label := range want {
		if _, ok := tester.Find(label); !ok {
			t.Fatalf("the checkpoint %q is not drawn; texts: %v", label, tester.Texts())
		}
	}
	for _, label := range []string{locales.WorktreeStepCreatePR(), locales.WorktreeStepReadPR()} {
		if _, ok := tester.Find(label); ok {
			t.Fatalf("a stage not reached yet is drawn: %q; texts: %v", label, tester.Texts())
		}
	}
	if _, ok := tester.Find("3/5"); ok {
		t.Fatalf("the card still counts its stages; texts: %v", tester.Texts())
	}

	p.Worktree.busyAt = stepReadPR
	tester.Frame()
	for _, label := range []string{locales.WorktreeStepCreatePR(), locales.WorktreeStepReadPR()} {
		if _, ok := tester.Find(label); !ok {
			t.Fatalf("the reached stage %q is not drawn; texts: %v", label, tester.Texts())
		}
	}
	if _, ok := tester.Find("5/5"); ok {
		t.Fatalf("the card still counts its stages; texts: %v", tester.Texts())
	}

	// The card belongs to the operation: once it ends, nothing is left
	// over the terminal.
	p.Worktree.busy = ""
	tester.Frame()
	if _, ok := tester.Find(locales.WorktreeStepCommit()); ok {
		t.Fatalf("an idle checkout still draws its checkpoints; texts: %v", tester.Texts())
	}
}

// inkPixels counts what the card draws over the page: the card's own fill is
// the page's colour, so this is its rim, lift and words. A card or a stage
// that is still faded in shows little of it, in either theme.
func inkPixels(img *image.RGBA) int {
	back := img.At(1, 1)
	br, bg, bb, _ := back.RGBA()
	n := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if absDiff(r, br) > 1200 || absDiff(g, bg) > 1200 || absDiff(b, bb) > 1200 {
				n++
			}
		}
	}
	return n
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestWorktreeCheckpointsAnimateIn checks the card and each stage it gains
// arrive with motion: a merge can start the moment the menu closes, and a
// panel that pops into its corner, or a list that jumps a line taller, reads
// as a redraw rather than as work beginning.
func TestWorktreeCheckpointsAnimateIn(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	p := &pane{Worktree: &paneWorktree{
		Path: t.TempDir(), Branch: "weaver", Base: "main",
		busy:      worktreeBusyMerge,
		busySteps: []worktreeStep{stepMergePR, stepSyncBase},
		busyAt:    stepMergePR,
	}}
	w := &workspace{}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			w.worktreeCheckpoints(c, tk, p)
		})
	}, 480, 300)

	// The first frame is the card at the start of its pose: faded out.
	tester.Frame()
	first := inkPixels(tester.Image())
	if first != 0 {
		t.Fatalf("the card is drawn at full strength on its first frame: %d pixels", first)
	}
	tester.Advance(checkpointMotion / 2)
	mid := inkPixels(tester.Image())
	tester.Advance(checkpointMotion)
	settled := inkPixels(tester.Image())
	if settled == 0 {
		t.Fatal("the card never drew")
	}
	if mid <= first || mid >= settled {
		t.Fatalf("the card has no fade: first %d, mid %d, settled %d", first, mid, settled)
	}

	// A stage reached later rises into its line the same way, and the card
	// is a line taller once it has.
	p.Worktree.busyAt = stepSyncBase
	tester.Frame()
	arriving := inkPixels(tester.Image())
	tester.Advance(checkpointMotion)
	arrived := inkPixels(tester.Image())
	if arriving >= arrived {
		t.Fatalf("the new stage is drawn at full strength on its first frame: %d then %d", arriving, arrived)
	}
	if arrived <= settled {
		t.Fatalf("the new stage never drew: settled %d, arrived %d", settled, arrived)
	}
	if !tester.HasText(locales.WorktreeStepSyncBase("main")) {
		t.Fatalf("the new stage is not drawn; texts %q", tester.Texts())
	}
}
