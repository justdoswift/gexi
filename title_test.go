package main

import (
	"testing"
	"time"
)

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

// TestTitleShowsWork: only the animated frames count as work. The star is
// where Claude Code and Amp rest, so it must not keep the sidebar loader on.
func TestTitleShowsWork(t *testing.T) {
	cases := []struct {
		title string
		want  bool
	}{
		{"⠍ Running", true},
		{"⣩", true},
		{"  ◐ Working", true},
		{"✳ Idle", false},
		{"✻ amp 终端", false},
		{"✦ opencode", false},
		{"Claude Code", false},
		{"", false},
	}
	for _, c := range cases {
		if got := titleShowsWork(c.title); got != c.want {
			t.Errorf("titleShowsWork(%q) = %v, want %v", c.title, got, c.want)
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

// A spinner mark that stops moving is the last frame an agent left in the
// title, not work: the sweep in the sidebar must not keep running for it.
func TestTitleStopsCountingWhenItStands(t *testing.T) {
	p := &pane{}
	if !p.titleWorkingFor("⠐ working") {
		t.Fatal("a fresh spinner should count as work")
	}
	if !p.titleWorkingFor("⠐ working") {
		t.Fatal("the same frame inside the window should still count")
	}
	p.titleSeenAt = time.Now().Add(-titleWorkWindow - time.Second)
	if p.titleWorkingFor("⠐ working") {
		t.Fatal("a mark that stood still past the window should stop counting")
	}
	if !p.titleWorkingFor("⠑ working") {
		t.Fatal("a new frame should start counting again")
	}
	if p.titleWorkingFor("amp") {
		t.Fatal("a title without a spinner is not work")
	}
}
