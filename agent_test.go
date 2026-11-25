package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
		{shellCommand("amp"), true},
		{shellCommand("amp threads continue --last"), true},
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

func TestDimAgent(t *testing.T) {
	if got := strings.Join(agentDim.launch(), "\x00"); got != strings.Join(shellCommand("dima"), "\x00") {
		t.Fatalf("dim launch = %q", got)
	}
	id := "11111111-1111-4111-8111-111111111111"
	if got := strings.Join(agentDim.resume(id), "\x00"); got != strings.Join(shellCommand(dimResumeCommand(id)), "\x00") {
		t.Fatalf("dim resume = %q", got)
	}
	if got := agentDim.name(); got != "Dim" {
		t.Fatalf("dim name = %q", got)
	}
	for _, name := range []string{"dima", "dimcode", "dim-nightly"} {
		if kind := agentNamed(name); kind != agentDim {
			t.Errorf("agentNamed(%q) = %q, want dim", name, kind)
		}
	}
	if kind := agentNamed("dimcode-server"); kind != "" {
		t.Errorf("the server binary is not a pane's agent, got %q", kind)
	}
}

// TestNewestDimSessionID reads the pane's session the way detection does: one
// read-only query against the home's own catalog.
func TestNewestDimSessionID(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	home := t.TempDir()
	database := filepath.Join(home, "rust", "dimcode.sqlite")
	if err := os.MkdirAll(filepath.Dir(database), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `
create table session_catalog (session_id text primary key, updated_at_ms integer not null, preview text, cwd text not null);
insert into session_catalog values ('11111111-1111-4111-8111-111111111111', 10, null, '/work/a');
insert into session_catalog values ('22222222-2222-4222-8222-222222222222', 20, null, '/work/a');
insert into session_catalog values ('33333333-3333-4333-8333-333333333333', 30, null, '/work/b');
`
	if out, err := exec.Command(sqlite, database, seed).CombinedOutput(); err != nil {
		t.Fatalf("seed: %v: %s", err, out)
	}
	t.Setenv("DIMCODE_HOME", home)
	if got := newestDimSessionID("/work/a"); got != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("newest for /work/a = %q", got)
	}
	if got := newestDimSessionID("/work/nowhere"); got != "" {
		t.Fatalf("unknown directory = %q, want empty", got)
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
