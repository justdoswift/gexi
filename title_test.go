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
		{"✳ 远程窗格与工作树", "远程窗格与工作树"},
		{"✳\uFE0F 远程窗格与工作树", "远程窗格与工作树"},
		{"✻ amp 终端自动恢复", "amp 终端自动恢复"},
		{"✶ Codex", "Codex"},
		{"✦ opencode", "opencode"},
		{"⠍ Remote resize flashing", "Remote resize flashing"},
		{"⣩", ""},
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

// TestRemoteTitlesDropTheActivityMark: a pane or session on another Mac wears
// the host's title, whose leading mark would sit beside the agent's logo as a
// second icon. Both the header and the sidebar call cleanTitle for it.
func TestRemoteTitlesDropTheActivityMark(t *testing.T) {
	p := &pane{remote: &remotePaneLink{title: "⠍ amp 终端自动恢复"}}
	if got := p.title(); got != "amp 终端自动恢复" {
		t.Fatalf("remote pane title = %q, want %q", got, "amp 终端自动恢复")
	}
	s := &session{remote: &remoteSessionLink{title: "✳ 远程窗格与工作树"}}
	if got := s.title(); got != "远程窗格与工作树" {
		t.Fatalf("remote session title = %q, want %q", got, "远程窗格与工作树")
	}
	if got := sessionLabel(s); got != "远程窗格与工作树" {
		t.Fatalf("remote session label = %q, want %q", got, "远程窗格与工作树")
	}
}
