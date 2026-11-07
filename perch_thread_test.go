package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPerchThreadIDMatchesCLI(t *testing.T) {
	// The CLI derives the id as sha256(realpath(dir))[:16]. This is the value
	// the repo's own folder produced when Perch ran there, so a change to the
	// derivation shows up here.
	got := perchThreadID("/Users/gepeng/Documents/coolweb/Gexi")
	if got != "cli-cwd-82c3f0c6ed46c00c" {
		t.Fatalf("perchThreadID = %q, want cli-cwd-82c3f0c6ed46c00c", got)
	}
}

func TestPerchTurnState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PERCH_CLI_STATE_DIR", root)

	dir := t.TempDir()
	account := filepath.Join(root, "user-test")
	if err := os.MkdirAll(account, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(account, perchThreadID(dir)+".json")

	// No file at all is idle, not busy.
	if busy, _ := perchTurnState(dir); busy {
		t.Fatal("a directory with no thread file should be idle")
	}

	// A finished turn leaves no inFlightTurn.
	write := func(v any) {
		data, _ := json.Marshal(v)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(map[string]any{"version": 1, "threadId": perchThreadID(dir)})
	if busy, _ := perchTurnState(dir); busy {
		t.Fatal("a thread with no inFlightTurn should be idle")
	}

	// A running turn, with this test's own pid so the liveness check passes.
	write(map[string]any{
		"version":  1,
		"threadId": perchThreadID(dir),
		"inFlightTurn": map[string]any{
			"prompt": "do the thing",
			"pid":    os.Getpid(),
		},
	})
	busy, prompt := perchTurnState(dir)
	if !busy {
		t.Fatal("a live inFlightTurn should be busy")
	}
	if prompt != "do the thing" {
		t.Fatalf("prompt = %q", prompt)
	}

	// A turn whose process died was interrupted, and must not read as busy.
	write(map[string]any{
		"version":  1,
		"threadId": perchThreadID(dir),
		"inFlightTurn": map[string]any{
			"prompt": "orphaned",
			"pid":    999999,
		},
	})
	if busy, _ := perchTurnState(dir); busy {
		t.Fatal("a turn whose process is gone should be idle")
	}
}

func TestApplyPerchTurn(t *testing.T) {
	p := &pane{}
	if !p.applyPerchTurn(true, "working on it") {
		t.Fatal("first busy should change state")
	}
	if p.activity == nil || p.activity.State != activityWorking {
		t.Fatalf("activity = %+v", p.activity)
	}
	if p.applyPerchTurn(true, "working on it") {
		t.Fatal("the same busy state should not report a change")
	}
	if !p.applyPerchTurn(false, "") {
		t.Fatal("going idle should change state")
	}
	if p.activity.State != activityIdle {
		t.Fatalf("state = %q", p.activity.State)
	}
}

func TestWorktreeNamingNoteIsOneShot(t *testing.T) {
	wt := &paneWorktree{Path: "/tmp/wt", Branch: "placeholder", Naming: true, Carried: "/tmp/project"}
	note := worktreeNamingNote(wt)
	if note == "" {
		t.Fatal("a fresh worktree should have a note")
	}
	// The note builder does not clear the flags; worktreeNamingContext does.
	if worktreeNamingNote(wt) == "" {
		t.Fatal("the builder is pure and should still produce the note")
	}
}

func TestWritePerchWorktreeRule(t *testing.T) {
	dir := t.TempDir()
	wt := &paneWorktree{Path: dir}

	if err := writePerchWorktreeRule(wt, "rename the branch"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".perch", "rules", perchRuleName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rule file not written: %v", err)
	}
	if !strings.Contains(string(data), "rename the branch") {
		t.Fatalf("rule body = %q", data)
	}

	// An empty note removes the file, so a settled worktree stops nagging.
	if err := writePerchWorktreeRule(wt, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("an empty note should remove the rule file")
	}
}
