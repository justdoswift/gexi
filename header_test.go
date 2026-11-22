package main

import "testing"

func TestCheckoutNameIsTheLastSegment(t *testing.T) {
	t.Setenv("GEXI_HOME", "/tmp/gexi-home")
	cases := []struct {
		dir  string
		want string
	}{
		{"/Users/gepeng/src/gexi", "gexi"},
		{"/tmp/gexi-home/worktrees/Gexi/kez", "kez"},
		// Only the folder gexi itself made is a worktree name; a folder
		// inside one is a place of its own.
		{"/tmp/gexi-home/worktrees/Gexi/kez/internal", "internal"},
		// The repo's folder is not a worktree.
		{"/tmp/gexi-home/worktrees/Gexi", "Gexi"},
		{"/", "/"},
	}
	for _, c := range cases {
		if got := checkoutName(c.dir); got != c.want {
			t.Errorf("checkoutName(%q) = %q; want %q", c.dir, got, c.want)
		}
	}
}

// A pane's header reads the width the frame before laid out, so a resize that
// crosses one of its thresholds would show a part on that frame and drop it on
// the next. headerWidth holds the last width two frames agreed on, so the
// parts stay put while a resize is still settling.
func TestHeaderWidthHoldsWhileAResizeSettles(t *testing.T) {
	p := &pane{}

	// The first frame has nothing to hold: it takes what it measured.
	if got := p.headerWidth(400); got != 400 {
		t.Fatalf("first width = %v; want 400", got)
	}

	// A resize landing on one frame is not the size yet: the header keeps
	// the one it had, so a part does not appear and go.
	if got := p.headerWidth(300); got != 400 {
		t.Fatalf("width after one frame at 300 = %v; want 400", got)
	}
	// The second frame at that width settles it.
	if got := p.headerWidth(300); got != 300 {
		t.Fatalf("width after two frames at 300 = %v; want 300", got)
	}

	// A frame that measured nothing keeps the width it had.
	if got := p.headerWidth(0); got != 300 {
		t.Fatalf("width from an unmeasured frame = %v; want 300", got)
	}
}
