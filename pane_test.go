package main

import (
	"slices"
	"testing"
)

// An update restart closes the windows before the layout is saved, so the
// agent can already be gone when the resume command is read. The last
// agent's resume has to survive that, or every pane comes back as a bare
// shell.
func TestSavedCommandKeepsAgentResume(t *testing.T) {
	shell := []string{"/bin/zsh", "-l"}
	resume := shellCommand("claude --resume 11111111-1111-4111-8111-111111111111")
	p := &pane{command: shell, restoreCommand: shell, baseRestore: shell}

	p.applyAgent(&agentDetection{
		Kind:      agentClaude,
		SessionID: "11111111-1111-4111-8111-111111111111",
		Resume:    resume,
	})
	if got := p.savedCommand(); !slices.Equal(got, resume) {
		t.Fatalf("with the agent running: saved %v", got)
	}

	p.applyAgent(nil)
	if got := p.savedCommand(); !slices.Equal(got, resume) {
		t.Fatalf("after the agent exited: saved %v, want %v", got, resume)
	}
}

// A pane's `zsh -lc` never reads ~/.zshrc, so the PATH the interactive
// shell builds is what keeps a tool installed in ~/.local/bin — claude —
// startable from a gexi opened in Finder.
func TestMergePaths(t *testing.T) {
	cases := []struct{ shell, inherited, want string }{
		{"", "/usr/bin", "/usr/bin"},
		{"/Users/me/.local/bin:/usr/bin", "/usr/bin:/bin", "/Users/me/.local/bin:/usr/bin:/bin"},
		{"/usr/bin::/usr/bin", "/bin", "/usr/bin:/bin"},
		{"/opt/homebrew/bin", "", "/opt/homebrew/bin"},
	}
	for _, c := range cases {
		if got := mergePaths(c.shell, c.inherited); got != c.want {
			t.Errorf("mergePaths(%q, %q) = %q, want %q", c.shell, c.inherited, got, c.want)
		}
	}
}

func TestWithPath(t *testing.T) {
	env := []string{"HOME=/Users/me", "PATH=/usr/bin:/bin", "TERM=dumb"}
	got := withPath("/Users/me/.local/bin:/usr/bin", env)
	if want := "PATH=/Users/me/.local/bin:/usr/bin:/bin"; got[1] != want {
		t.Fatalf("PATH = %q, want %q", got[1], want)
	}
	if got[0] != "HOME=/Users/me" || got[2] != "TERM=dumb" {
		t.Fatalf("other variables changed: %q", got)
	}

	// No PATH at all: the shell's is added.
	got = withPath("/usr/bin", []string{"HOME=/Users/me"})
	if want := []string{"HOME=/Users/me", "PATH=/usr/bin"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A failed probe leaves the environment alone.
	env = []string{"PATH=/usr/bin"}
	if got := withPath("", env); !slices.Equal(got, env) {
		t.Fatalf("empty shell path changed env: %q", got)
	}
}

func TestResumesAgent(t *testing.T) {
	cases := []struct {
		command []string
		want    bool
	}{
		{shellCommand("claude --resume 11111111-1111-4111-8111-111111111111"), true},
		{shellCommand("codex resume --last"), true},
		{shellCommand("omp --continue"), true},
		{shellCommand("pi --continue"), true},
		{shellCommand("droid --resume --last"), true},
		{[]string{"/bin/zsh", "-l"}, false},
		{shellCommand("claude"), false},
	}
	for _, c := range cases {
		if got := resumesAgent(c.command); got != c.want {
			t.Fatalf("resumesAgent(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}
