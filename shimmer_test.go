package main

import (
	"testing"
	"time"

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

// A session at work shows a loading indicator where its mark sits: the sweep
// across the name alone is easy to miss at a glance.
func TestWorkingSessionShowsASpinner(t *testing.T) {
	p := &pane{ID: "p", outAt: time.Now(), activity: &agentActivity{State: activityWorking}}
	s := &session{ID: "s", Label: "Trace", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{Projects: []*project{pr}}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Padding(12).Children(func() {
			w.sessionRow(c, tk, pr, s, 0)
		})
	}
	tester := ui.NewTester(view, 320, 120)
	tester.Frame()
	if _, ok := tester.AccessByRole(ui.RoleProgress, ""); !ok {
		t.Fatalf("a working session draws no spinner; texts: %v", tester.Texts())
	}

	p.activity = nil
	tester.Frame()
	if _, ok := tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("an idle session still draws a spinner")
	}
}
