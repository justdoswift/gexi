package main

import (
	"strings"
	"testing"
)

func TestAgentLaunch(t *testing.T) {
	got := strings.Join(agentClaude.launch(), "\x00")
	want := strings.Join(shellCommand("claude"), "\x00")
	if got != want {
		t.Fatalf("claude launch = %q, want %q", got, want)
	}
	if unknown := agentKind("unknown").launch(); unknown != nil {
		t.Fatalf("unknown agent launch = %v, want nil", unknown)
	}
}

func TestPaneRunsAgent(t *testing.T) {
	cases := []struct {
		command []string
		want    bool
	}{
		{shellCommand("claude --resume 11111111-1111-4111-8111-111111111111"), true},
		{shellCommand("codex resume --last"), true},
		{[]string{"claude"}, true},
		{shellCommand("pi --continue"), true},
		{shellCommand("npm test"), false},
		{[]string{"/bin/zsh", "-l"}, false},
	}
	for _, c := range cases {
		if got := paneRunsAgent(c.command); got != c.want {
			t.Errorf("paneRunsAgent(%v) = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestAgentPaneFallsBackToShell(t *testing.T) {
	launch := withShellFallback(shellCommand("claude --resume abc"))
	raw, ok := rawCommand(launch)
	if !ok {
		t.Fatalf("fallback launch %v is not a shell command", launch)
	}
	want := "claude --resume abc ; exec " + shellQuote(loginShell()) + " -l"
	if raw != want {
		t.Fatalf("fallback raw = %q, want %q", raw, want)
	}
	// The pane still reads as an agent pane: the fallback trails the agent.
	if !paneRunsAgent(launch) {
		t.Fatalf("paneRunsAgent(%q) = false, want true", raw)
	}
	// A command that is not a shell wrapper cannot take a fallback.
	if got := withShellFallback([]string{"claude"}); len(got) != 1 || got[0] != "claude" {
		t.Fatalf("bare command = %v, want unchanged", got)
	}
}

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
