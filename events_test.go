package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd"} {
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

// A claude started by hand in a pane never sees --settings, so the user's own
// settings carry the same hooks, guarded by GEXI_PANE.
func TestGlobalClaudeHooksMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEXI_BIN", "/opt/test/bin/gexi")
	t.Cleanup(func() { globalHooksInstalled = false })

	path := globalClaudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "/opt/other/bridge"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installGlobalClaudeHooks(); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Installing again must replace Gexi's entries, not stack another set.
	if err := installGlobalClaudeHooks(); err != nil {
		t.Fatalf("install again: %v", err)
	}

	var doc struct {
		Model string `json:"model"`
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string }
		} `json:"hooks"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("settings are not JSON: %v", err)
	}
	if doc.Model != "opus" {
		t.Fatalf("unrelated setting lost: %s", data)
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd"} {
		entries := doc.Hooks[name]
		var gexi []string
		for _, e := range entries {
			for _, h := range e.Hooks {
				if strings.Contains(h.Command, "GEXI_PANE") {
					gexi = append(gexi, h.Command)
				}
			}
		}
		if len(gexi) != 1 {
			t.Fatalf("%s has %d gexi hooks, want 1: %s", name, len(gexi), data)
		}
		if !strings.Contains(gexi[0], "/opt/test/bin/gexi") || !strings.Contains(gexi[0], " event ") {
			t.Fatalf("%s hook does not call gexi: %s", name, gexi[0])
		}
	}
	// Another tool's hook under the same event stays where it was.
	kept := false
	for _, e := range doc.Hooks["Stop"] {
		for _, h := range e.Hooks {
			if h.Command == "/opt/other/bridge" {
				kept = true
			}
		}
	}
	if !kept {
		t.Fatalf("another tool's Stop hook was dropped: %s", data)
	}
	if got := doc.Hooks["Notification"][len(doc.Hooks["Notification"])-1].Matcher; got != "permission_prompt" {
		t.Fatalf("Notification matcher is %q, want permission_prompt", got)
	}
}
