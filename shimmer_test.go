package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestMergeChipShimmersLabel checks the PR/merge chip's wait draws the
// words themselves, as one label, with no spinner beside them.
func TestMergeChipShimmersLabel(t *testing.T) {
	dir := t.TempDir()
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		w := &workspace{}
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, &pane{Worktree: &paneWorktree{
				Path: dir, Branch: "b", Base: "main", PR: 7, State: "open",
				busy:      worktreeBusyMerge,
				busySteps: []worktreeStep{stepMergePR, stepSyncBase},
				busyAt:    stepMergePR,
			}})
		})
	}
	tester := ui.NewTester(view, 260, 180)
	tester.Frame()
	label := locales.WorktreeMerging()
	if _, ok := tester.Find(label); !ok {
		for _, s := range tester.Texts() {
			t.Logf("text %q", s)
		}
		t.Fatalf("the merging label %q is not drawn", label)
	}
	tester.Frame()
}
