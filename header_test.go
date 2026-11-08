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
