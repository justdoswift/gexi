package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Perch has no hooks, so the events.go stream that Claude Code feeds never
// fires for it. What Perch does keep is one thread file per directory, and
// that file holds an inFlightTurn while a turn runs. Reading it is how a
// Perch pane gets the same working/idle state a hooked agent reports.

// perchThreadDir is where the CLI keeps its thread files. The user id folder
// is discovered rather than assumed, since it changes with the signed-in
// account.
func perchThreadDir() string {
	if dir := strings.TrimSpace(os.Getenv("PERCH_CLI_STATE_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".perch", "threads")
}

// perchThreadID is the id the CLI derives for a directory: sha256 of the
// resolved path, first 16 hex characters, prefixed. It is the same value the
// CLI computes, so a pane's folder names its thread without asking Perch.
func perchThreadID(dir string) string {
	resolved := dir
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		resolved = real
	}
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	sum := sha256.Sum256([]byte(resolved))
	return "cli-cwd-" + hex.EncodeToString(sum[:])[:16]
}

// perchThreadFile finds the thread file for a directory. The account folder
// is not known here, so each one is tried; there are only a handful.
func perchThreadFile(dir string) string {
	root := perchThreadDir()
	if root == "" || dir == "" {
		return ""
	}
	name := perchThreadID(dir) + ".json"
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name(), name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// perchTurnState reports whether a turn is running in dir, and the prompt it
// is answering. A missing or unreadable file is idle: Perch may never have
// run there, which is not the same as being busy.
func perchTurnState(dir string) (busy bool, prompt string) {
	path := perchThreadFile(dir)
	if path == "" {
		return false, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	var thread struct {
		InFlightTurn *struct {
			Prompt string `json:"prompt"`
			Pid    int    `json:"pid"`
		} `json:"inFlightTurn"`
	}
	if err := json.Unmarshal(data, &thread); err != nil {
		return false, ""
	}
	if thread.InFlightTurn == nil {
		return false, ""
	}
	// A turn whose process is gone was interrupted; the file keeps it until
	// the next run cleans up, so the pid is checked before calling it busy.
	if thread.InFlightTurn.Pid > 0 && !processAlive(thread.InFlightTurn.Pid) {
		return false, ""
	}
	return true, thread.InFlightTurn.Prompt
}

// processAlive reports whether pid is a running process. Signal 0 is the
// test: it checks the process exists without touching it.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return unix.Kill(pid, 0) == nil
}
