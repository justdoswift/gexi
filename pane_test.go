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

func TestResumesAgent(t *testing.T) {
	cases := []struct {
		command []string
		want    bool
	}{
		{shellCommand("claude --resume 11111111-1111-4111-8111-111111111111"), true},
		{shellCommand("codex resume --last"), true},
		{shellCommand("omp --continue"), true},
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
