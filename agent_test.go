package main

import "testing"

func TestClaudeProjectSlug(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		// The ordinary case: slashes become hyphens.
		{"/Users/gepeng/Documents/coolweb/Gexi", "-Users-gepeng-Documents-coolweb-Gexi"},
		// Dots become hyphens too, which doubles the one before ".claude".
		{
			"/Users/gepeng/Documents/coolweb/JustWrite/.claude/worktrees/writing-loop-v1-54",
			"-Users-gepeng-Documents-coolweb-JustWrite--claude-worktrees-writing-loop-v1-54",
		},
		// gexi's own worktrees sit under "Application Support": spaces count.
		{
			"/Users/gepeng/Library/Application Support/Gexi/worktrees/Gexi-3a77b02a/budapest",
			"-Users-gepeng-Library-Application-Support-Gexi-worktrees-Gexi-3a77b02a-budapest",
		},
	}
	for _, c := range cases {
		if got := claudeProjectSlug(c.dir); got != c.want {
			t.Errorf("claudeProjectSlug(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}
