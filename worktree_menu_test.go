package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestWorktreeChipMenu checks the chevron beside the chip opens the
// worktree menu, where leaving the checkout is: the rare action shares
// the chip instead of holding a button of its own.
func TestWorktreeChipMenu(t *testing.T) {
	dir := t.TempDir()
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		w := &workspace{}
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, &pane{Worktree: &paneWorktree{
				Path: dir, Branch: "b", Base: "main",
			}})
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

// TestMissingWorktreeChip checks a checkout deleted under the running app
// stops offering PR and clears the pane's binding instead of running git in
// a folder that is gone.
func TestMissingWorktreeChip(t *testing.T) {
	p := &pane{Worktree: &paneWorktree{
		Path: filepath.Join(t.TempDir(), "gone"), Branch: "b", Base: "main",
	}}
	w := &workspace{}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, p)
		})
	}
	tester := ui.NewTester(view, 220, 180)
	tester.Frame()
	if _, ok := tester.Find(locales.WorktreePR()); ok {
		t.Fatal("a missing checkout still offers PR")
	}
	r, ok := tester.Find(locales.WorktreeGone())
	if !ok {
		t.Fatal("the missing chip is not drawn")
	}
	tester.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tester.Frame()
	if p.Worktree != nil {
		t.Fatal("the gone worktree was not cleared")
	}
}

func TestDetectedWorktreeChipHasNoMenu(t *testing.T) {
	dir := t.TempDir()
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		w := &workspace{}
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, &pane{Worktree: &paneWorktree{
				Path: dir, Branch: "b", Base: "main", Detected: true,
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

// TestLeavingChipIsOneCenteredSpinner checks that leaving a checkout takes
// over the whole chip: the loading sits where the chip is, not squeezed into
// the chevron half at its right end.
func TestLeavingChipIsOneCenteredSpinner(t *testing.T) {
	dir := t.TempDir()
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		w := &workspace{}
		ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
			w.paneWorktreeButton(c, tk, &pane{
				Worktree:     &paneWorktree{Path: dir, Branch: "b", Base: "main"},
				worktreeBusy: worktreeBusyLeave,
			})
		})
	}
	tester := ui.NewTester(view, 220, 180)
	tester.Frame()
	r, ok := tester.Find(locales.WorktreeLeaving())
	if !ok {
		t.Fatal("the leaving spinner is not drawn")
	}
	if _, ok := tester.Find(locales.WorktreePR()); ok {
		t.Fatal("the PR label still sits beside the leaving spinner")
	}
	// The chip is the spinner's own box plus its padding, so the spinner
	// is in the middle of the room the label and chevron left.
	if r.W != 12 || r.H != 12 {
		t.Fatalf("the spinner takes %vx%v, want 12x12", r.W, r.H)
	}
	if center := r.X + r.W/2; center < 109.5 || center > 110.5 {
		t.Fatalf("the spinner's center is at %.1f, want the chip's 110", center)
	}
}

