package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A linked worktree keeps its branch and repository root in the git directory
// its .git file names; the Worktrees page must read them without git.
func TestWorktreeCheckoutInfoReadsGitFiles(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	wt := filepath.Join(dir, "worktrees", "repo-hash", "axe")
	gitdir := filepath.Join(repo, ".git", "worktrees", "axe")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(wt, ".git"), "gitdir: "+gitdir+"\n")
	write(filepath.Join(gitdir, "HEAD"), "ref: refs/heads/axe\n")
	write(filepath.Join(gitdir, "commondir"), "../..\n")

	branch, root := worktreeCheckoutInfo(wt)
	if branch != "axe" {
		t.Fatalf("branch = %q, want axe", branch)
	}
	if !sameDir(root, repo) {
		t.Fatalf("root = %q, want %q", root, repo)
	}
}

func TestWorktreeCheckoutInfoDetachedHead(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "checkout")
	gitdir := filepath.Join(dir, "git", "worktrees", "x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte("0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	branch, _ := worktreeCheckoutInfo(wt)
	if branch != "0123456" {
		t.Fatalf("branch = %q, want the short commit", branch)
	}
}

func TestPanesHaveAgent(t *testing.T) {
	if panesHaveAgent([]*pane{{}, {}}) {
		t.Fatal("plain shells should not ask")
	}
	if !panesHaveAgent([]*pane{{}, {sawAgent: true}}) {
		t.Fatal("a pane that ran an agent should ask")
	}
	if !panesHaveAgent([]*pane{{lastAgentRestore: []string{"claude", "--resume", "x"}}}) {
		t.Fatal("a pane with a saved resume should ask")
	}
}
