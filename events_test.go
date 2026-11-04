package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClaudeLaunchInjectsHooks(t *testing.T) {
	t.Setenv("JUSTCLI_HOME", t.TempDir())
	t.Setenv("JUSTCLI_BIN", "/opt/test/bin/justcli")

	resolved := resolveAgentCommand(shellCommand("claude"))
	raw, ok := rawCommand(resolved.launch)
	if !ok {
		t.Fatalf("launch is not a shell command: %v", resolved.launch)
	}
	if !strings.Contains(raw, "--session-id") {
		t.Fatalf("launch lost the session id: %s", raw)
	}
	if !strings.Contains(raw, "--settings") {
		t.Fatalf("launch has no hook settings: %s", raw)
	}

	data, err := os.ReadFile(hookSettingsPath())
	if err != nil {
		t.Fatalf("hook settings not written: %v", err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("hook settings are not JSON: %v", err)
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd"} {
		entries := settings.Hooks[name]
		if len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("hook %s missing", name)
		}
		if !strings.Contains(entries[0].Hooks[0].Command, "/opt/test/bin/justcli") {
			t.Fatalf("hook %s does not call justcli: %s", name, entries[0].Hooks[0].Command)
		}
	}

	// Resume must keep the hook settings, or a restored pane goes silent.
	restore, _ := rawCommand(resolved.restore)
	if !strings.Contains(restore, "--resume") || !strings.Contains(restore, "--settings") {
		t.Fatalf("restore lost session or hooks: %s", restore)
	}
}

func TestClaudeWithoutClientSkipsHooks(t *testing.T) {
	t.Setenv("JUSTCLI_HOME", t.TempDir())
	t.Setenv("JUSTCLI_BIN", "")
	t.Setenv("PATH", t.TempDir())

	resolved := resolveAgentCommand(shellCommand("claude"))
	raw, _ := rawCommand(resolved.launch)
	if strings.Contains(raw, "--settings") {
		t.Fatalf("launch points at a missing client: %s", raw)
	}
}
