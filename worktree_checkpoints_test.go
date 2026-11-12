package main

import (
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
