package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestWorktreeChipMenu checks the chevron beside the chip opens the
// worktree menu, where leaving the checkout is: the rare action shares
// the chip instead of holding a button of its own.
func TestWorktreeChipMenu(t *testing.T) {
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		w := &workspace{}
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, &pane{Worktree: &paneWorktree{Branch: "b", Base: "main"}})
		})
	}
	tester := ui.NewTester(view, 220, 180)
	tester.Frame()
	r, ok := tester.Find(locales.WorktreePR())
	if !ok {
		t.Fatal("the PR chip is not drawn")
	}
	// The chevron sits past the label, across the rule.
	tester.ClickAt(r.X+r.W+13, r.Y+r.H/2)
	tester.Frame()
	for _, s := range tester.Texts() {
		if s == locales.WorktreeLeave() {
			return
		}
	}
	t.Fatalf("the menu does not show %q, only %q", locales.WorktreeLeave(), tester.Texts())
}

// TestDetectedWorktreeChipHasNoMenu checks a checkout gexi only found keeps
// the pull request control but offers no Leave: the checkout is not gexi's
// to remove.
func TestDetectedWorktreeChipHasNoMenu(t *testing.T) {
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		w := &workspace{}
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, &pane{Worktree: &paneWorktree{
				Branch: "b", Base: "main", Detected: true,
			}})
		})
	}
	tester := ui.NewTester(view, 220, 180)
	tester.Frame()
	r, ok := tester.Find(locales.WorktreePR())
	if !ok {
		t.Fatal("the PR chip is not drawn")
	}
	tester.ClickAt(r.X+r.W+13, r.Y+r.H/2)
	tester.Frame()
	for _, s := range tester.Texts() {
		if s == locales.WorktreeLeave() {
			t.Fatal("a detected checkout offers Leave")
		}
	}
}
