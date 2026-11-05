package main

import "testing"

func TestCleanTitleDropsAgentSpinner(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"◐ worktree、PR、merge 的转圈 loading", "worktree、PR、merge 的转圈 loading"},
		{"◑ Claude Code", "Claude Code"},
		{"◓", ""},
		{"  ◒  Fix the tests  ", "Fix the tests"},
		{"Claude Code", "Claude Code"},
		{"● Claude Code", "● Claude Code"},
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanTitle(c.title); got != c.want {
			t.Errorf("cleanTitle(%q) = %q; want %q", c.title, got, c.want)
		}
	}
}
