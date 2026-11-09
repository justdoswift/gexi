package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justdoswift/gexi/locales"
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

// TestScanWorktreesListsLeftoverFolder checks a folder left by a checkout
// git could not finish deleting is still on the page: git drops the metadata
// first, so its HEAD is gone and the old scan skipped it.
func TestScanWorktreesListsLeftoverFolder(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	path := filepath.Join(worktreesRoot(), "repo-1234", "leftover")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := scanWorktrees()
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want the leftover folder", entries)
	}
	if e := entries[0]; !e.Broken || e.Path != path || e.Branch != "leftover" {
		t.Fatalf("entry = %+v, want a broken leftover at %s", e, path)
	}
}

// TestKeepLeftBranchIsListed checks a branch kept by Leave is on the page
// with its own status, rather than waiting in git where nothing shows it.
func TestKeepLeftBranchIsListed(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	w := &workspace{}
	wt := &paneWorktree{Root: "/repo", Path: "/repo-kept", Branch: "kept", Base: "main"}
	w.keepLeftBranch(wt, "/repo")
	rows := w.worktreeRows()
	if len(rows) != 1 || rows[0].archived == nil {
		t.Fatalf("rows = %+v, want the kept branch", rows)
	}
	if !rows[0].archived.Kept || rows[0].branch() != "kept" {
		t.Fatalf("archived = %+v, want the kept branch", rows[0].archived)
	}
	if got := rows[0].status(); got != locales.WorktreeStatusKept() {
		t.Fatalf("status = %q, want %q", got, locales.WorktreeStatusKept())
	}
}

func TestGroupWorktreeRowsByProject(t *testing.T) {
	rows := []worktreeRow{
		{entry: worktreeScanEntry{Path: "/a/alpha/wt1", Root: "/a/alpha", Branch: "one"}},
		{entry: worktreeScanEntry{Path: "/a/alpha/wt2", Root: "/a/alpha", Branch: "two"}},
		{archived: &archivedWorktree{ProjectDir: "/b/beta", Path: "/b/beta/wt", Branch: "three"}},
	}
	groups := groupWorktreeRows(rows)
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want alpha and beta", groups)
	}
	if groups[0].name != "alpha" || len(groups[0].rows) != 2 {
		t.Fatalf("first group = %+v, want alpha with two rows", groups[0])
	}
	if groups[1].name != "beta" || groups[1].rows[0].branch() != "three" {
		t.Fatalf("second group = %+v, want beta with three", groups[1])
	}
	if got := rows[2].projectName(); got != "beta" {
		t.Fatalf("archived project = %q, want beta", got)
	}
}

func TestDropArchivedIDs(t *testing.T) {
	entries := []archivedWorktree{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	kept := dropArchivedIDs(entries, map[string]bool{"b": true})
	if len(kept) != 2 || kept[0].ID != "a" || kept[1].ID != "c" {
		t.Fatalf("kept = %+v, want a and c", kept)
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
