package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClaudeLaunchInjectsHooks(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", "/opt/test/bin/gexi")

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
			Matcher string
			Hooks   []struct{ Type, Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("hook settings are not JSON: %v", err)
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "Stop", "Notification", "SessionEnd"} {
		entries := settings.Hooks[name]
		if len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("hook %s missing", name)
		}
		if !strings.Contains(entries[0].Hooks[0].Command, "/opt/test/bin/gexi") {
			t.Fatalf("hook %s does not call gexi: %s", name, entries[0].Hooks[0].Command)
		}
	}
	// An idle Claude fires Notification too; only a permission prompt means
	// the agent is blocked.
	if got := settings.Hooks["Notification"][0].Matcher; got != "permission_prompt" {
		t.Fatalf("Notification matcher is %q, want permission_prompt", got)
	}
	if got := settings.Hooks["PreToolUse"][0].Matcher; got != "TodoWrite|mcp__ccd_turn__update_plan" {
		t.Fatalf("PreToolUse matcher is %q", got)
	}
	if cmd := settings.Hooks["PreToolUse"][0].Hooks[0].Command; !strings.Contains(cmd, " plan ") {
		t.Fatalf("PreToolUse does not call plan: %s", cmd)
	}

	// Resume must keep the hook settings, or a restored pane goes silent.
	restore, _ := rawCommand(resolved.restore)
	if !strings.Contains(restore, "--resume") || !strings.Contains(restore, "--settings") {
		t.Fatalf("restore lost session or hooks: %s", restore)
	}
}

func TestClaudeResumeKeepsHooks(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", "/opt/test/bin/gexi")

	resolved := resolveAgentCommand(shellCommand("claude --resume 2e1130fe-78ba-4fbc-917b-8254bd823f52"))
	raw, _ := rawCommand(resolved.launch)
	if !strings.Contains(raw, "--settings") {
		t.Fatalf("resume has no hook settings: %s", raw)
	}
	if strings.Count(raw, "--resume") != 1 {
		t.Fatalf("resume was not left alone: %s", raw)
	}
	restore, _ := rawCommand(resolved.restore)
	if restore != raw {
		t.Fatalf("resume restore differs from launch: %s", restore)
	}
}

// A bundle has no gexi CLI beside it, so the app binary itself must answer
// the hook's `event` call.
func TestClaudeWithoutClientUsesOwnBinary(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", "")
	t.Setenv("PATH", t.TempDir())

	resolved := resolveAgentCommand(shellCommand("claude"))
	raw, _ := rawCommand(resolved.launch)
	if !strings.Contains(raw, "--settings") {
		t.Fatalf("launch has no hook settings: %s", raw)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("own executable: %v", err)
	}
	data, err := os.ReadFile(hookSettingsPath())
	if err != nil {
		t.Fatalf("hook settings not written: %v", err)
	}
	if !strings.Contains(string(data), exe) {
		t.Fatalf("hooks do not call this binary: %s", data)
	}
}
