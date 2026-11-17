package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDroidLaunchInjectsHooks covers the launch gexi builds for Droid: the
// hook settings go right after `droid`, before its own flags and subcommands,
// and the restore command keeps them. Nothing is written to ~/.factory.
func TestDroidLaunchInjectsHooks(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	bin := filepath.Join(t.TempDir(), "gexi")
	t.Setenv("GEXI_BIN", bin)
	home := t.TempDir()
	t.Setenv("HOME", home)

	resolved := resolveAgentCommand(shellCommand("droid --model opus"))
	launch, ok := rawCommand(resolved.launch)
	if !ok {
		t.Fatalf("launch is not a raw command: %q", resolved.launch)
	}
	if got := strings.Count(launch, "--settings"); got != 1 {
		t.Fatalf("launch has %d --settings flags: %q", got, launch)
	}
	if !strings.Contains(launch, shellQuote(droidHookSettingsPath())) {
		t.Fatalf("launch does not name the hook settings: %q", launch)
	}
	settingsAt := strings.Index(launch, "--settings")
	modelAt := strings.Index(launch, "--model")
	if settingsAt < 0 || modelAt < 0 || settingsAt > modelAt {
		t.Fatalf("--settings is not right after droid: %q", launch)
	}
	restore, ok := rawCommand(resolved.restore)
	if !ok {
		t.Fatalf("restore is not a raw command: %q", resolved.restore)
	}
	if !strings.Contains(restore, "--resume --last") {
		t.Fatalf("restore does not resume the newest session: %q", restore)
	}
	if got := strings.Count(restore, "--settings"); got != 1 {
		t.Fatalf("restore has %d --settings flags: %q", got, restore)
	}

	data, err := os.ReadFile(droidHookSettingsPath())
	if err != nil {
		t.Fatalf("hook settings: %v", err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("hook settings are not JSON: %v", err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd"} {
		groups := file.Hooks[event]
		if len(groups) == 0 || len(groups[0].Hooks) == 0 {
			t.Fatalf("%s has no hook in %s", event, data)
		}
		command := groups[0].Hooks[0].Command
		if !strings.Contains(command, shellQuote(bin)) || !strings.Contains(command, "--agent droid") {
			t.Fatalf("%s runs %q, want %s and --agent droid", event, command, bin)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("the launch wrote into ~/.factory: %v", err)
	}
}

// TestDroidResumeKeepsHooks checks a pane restored with --resume does not
// grow a second resume flag and still carries the hook overlay.
func TestDroidResumeKeepsHooks(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", filepath.Join(t.TempDir(), "gexi"))

	resolved := resolveAgentCommand(shellCommand("droid --resume --last"))
	launch, _ := rawCommand(resolved.launch)
	restore, _ := rawCommand(resolved.restore)
	for name, raw := range map[string]string{"launch": launch, "restore": restore} {
		if got := strings.Count(raw, "--resume"); got != 1 {
			t.Fatalf("%s has %d --resume flags: %q", name, got, raw)
		}
		if !strings.Contains(raw, "--settings") {
			t.Fatalf("%s does not carry the hook settings: %q", name, raw)
		}
	}
	if launch != restore {
		t.Fatalf("a resumed droid changed on restore: %q then %q", launch, restore)
	}

	// The resume subcommand takes the overlay before the word, where droid
	// parses its global flags.
	resolved = resolveAgentCommand(shellCommand("droid resume"))
	launch, _ = rawCommand(resolved.launch)
	if !strings.Contains(launch, "droid --settings") ||
		!strings.HasSuffix(launch, " resume") {
		t.Fatalf("the overlay is not before the resume subcommand: %q", launch)
	}
}

// TestDroidEventKind covers the reasons behind Droid's one Notification hook:
// a permission prompt waits on the user, an idle prompt ends the turn (also
// after Ctrl+C), an auth success says nothing about the work, and a Stop kept
// active by another hook keeps the light on.
func TestDroidEventKind(t *testing.T) {
	cases := []struct {
		kind string
		hook hookPayload
		want string
		keep bool
	}{
		{"turn.start", hookPayload{}, "turn.start", true},
		{"needs-input", hookPayload{NotificationType: "permission_prompt"}, "needs-input", true},
		{"needs-input", hookPayload{NotificationType: "elicitation_dialog"}, "needs-input", true},
		{"needs-input", hookPayload{NotificationType: "idle_prompt"}, "turn.end", true},
		{"needs-input", hookPayload{NotificationType: "auth_success"}, "", false},
		{"needs-input", hookPayload{NotificationType: "something_new"}, "", false},
		{"turn.end", hookPayload{}, "turn.end", true},
		{"turn.end", hookPayload{StopHookActive: true}, "", false},
		{"session.start", hookPayload{}, "session.start", true},
	}
	for _, c := range cases {
		got, keep := droidEventKind(c.kind, c.hook)
		if got != c.want || keep != c.keep {
			t.Fatalf("droidEventKind(%q, %+v) = %q, %v; want %q, %v",
				c.kind, c.hook, got, keep, c.want, c.keep)
		}
	}
}

// TestDroidTurnsLightThePane wires Droid's turn hooks through the socket
// handler: the pane works between turn.start and turn.end, and the session
// row and the pane beam read the same flag.
func TestDroidTurnsLightThePane(t *testing.T) {
	p := &pane{ID: "dr12", outAt: time.Now()}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if p.working() {
		t.Fatal("a fresh droid pane is already working")
	}
	if _, err := w.handleEvent(agentEvent{Kind: "turn.start", Pane: "dr12", Agent: "droid"}); err != nil {
		t.Fatal(err)
	}
	if !p.working() {
		t.Fatal("droid's turn.start did not light the pane")
	}
	if got := sessionActivityState(s); got != activityWorking {
		t.Fatalf("the session reads %q while droid works", got)
	}

	if _, err := w.handleEvent(agentEvent{Kind: "turn.end", Pane: "dr12", Agent: "droid"}); err != nil {
		t.Fatal(err)
	}
	if p.working() {
		t.Fatal("droid's turn.end left the pane working")
	}
	if got := sessionActivityState(s); got != "" {
		t.Fatalf("the session still reads %q after droid's turn", got)
	}
}
