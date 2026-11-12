package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestWorktreeCheckpointsShowWithoutHover checks a running pull request states
// its stages in the pane itself: every stage is drawn, with no hint to open.
func TestWorktreeCheckpointsShowWithoutHover(t *testing.T) {
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
		locales.WorktreeStepCreatePR(),
		locales.WorktreeStepReadPR(),
	}
	for _, label := range want {
		if _, ok := tester.Find(label); !ok {
			t.Fatalf("the checkpoint %q is not drawn; texts: %v", label, tester.Texts())
		}
	}

	// The card belongs to the operation: once it ends, nothing is left
	// over the terminal.
	p.Worktree.busy = ""
	tester.Frame()
	if _, ok := tester.Find(locales.WorktreeStepCommit()); ok {
		t.Fatalf("an idle checkout still draws its checkpoints; texts: %v", tester.Texts())
	}
}
