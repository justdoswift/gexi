package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func initRepoTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitTest(t, dir, "init", "-b", "main")
	runGitTest(t, dir, "config", "user.email", "gexi@example.com")
	runGitTest(t, dir, "config", "user.name", "Gexi")
	runGitTest(t, dir, "commit", "--allow-empty", "-m", "init")
	return dir
}

func TestCreateWorktree(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)

	wt, err := createWorktree(root)
	if err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	if wt == nil {
		t.Fatal("no worktree for a repository root")
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})

	if wt.Base != "main" {
		t.Fatalf("base = %q, want main", wt.Base)
	}
	if wt.Branch == "" || !strings.HasPrefix(filepath.Dir(wt.Path), worktreesRoot()) {
		t.Fatalf("worktree outside GEXI_HOME: %+v", wt)
	}
	if !wt.Naming {
		t.Fatal("a new worktree starts out naming its branch")
	}
	if info, err := os.Stat(wt.Path); err != nil || !info.IsDir() {
		t.Fatalf("worktree path missing: %v", err)
	}
	if got := runGitTest(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != wt.Branch {
		t.Fatalf("branch = %q, want %q", got, wt.Branch)
	}
	// A new branch must not push to the base it started from.
	if _, err := exec.Command("git", "-C", wt.Path, "rev-parse", "--abbrev-ref", "@{upstream}").CombinedOutput(); err == nil {
		t.Fatal("worktree branch tracks an upstream")
	}
}

func TestSyncLocalBase(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitTest(t, t.TempDir(), "init", "--bare", remote)

	root := initRepoTest(t)
	runGitTest(t, root, "remote", "add", "origin", remote)
	runGitTest(t, root, "push", "-u", "origin", "main")

	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})

	runGitTest(t, wt.Path, "commit", "--allow-empty", "-m", "work")
	runGitTest(t, wt.Path, "push", "-u", "origin", wt.Branch)

	// Merge on the remote the way GitHub does, from another checkout.
	other := filepath.Join(t.TempDir(), "other")
	runGitTest(t, filepath.Dir(other), "clone", remote, other)
	runGitTest(t, other, "merge", "--ff-only", "origin/"+wt.Branch)
	runGitTest(t, other, "push", "origin", "main")

	if err := syncLocalBase(wt); err != nil {
		t.Fatalf("sync local base: %v", err)
	}
	want := runGitTest(t, other, "rev-parse", "HEAD")
	if got := runGitTest(t, root, "rev-parse", "HEAD"); got != want {
		t.Fatalf("local main = %s, want %s", got, want)
	}
}

func TestCreateWorktreeSkipsNonRepo(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	wt, err := createWorktree(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wt != nil {
		t.Fatalf("worktree in a folder that is not a repo: %+v", wt)
	}
}

func TestSessionLabelUsesPaneWorktreeBranch(t *testing.T) {
	p := &pane{ID: "p", Worktree: &paneWorktree{Branch: "fix-login"}}
	s := &session{ID: "s", Root: leaf(p)}
	if got := sessionLabel(s); got != "fix-login" {
		t.Fatalf("label = %q, want the branch", got)
	}
}

func TestWorktreeNamingContextIsOneShot(t *testing.T) {
	p := &pane{
		ID: "p",
		Worktree: &paneWorktree{
			Path:   "/tmp/wt",
			Branch: "kyoto",
			Naming: true,
		},
	}
	s := &session{ID: "s", Root: leaf(p)}
	w := &workspace{Projects: []*project{{ID: "pr", Dir: "/tmp", Sessions: []*session{s}}}}

	ctx := w.worktreeNamingContext(p)
	if !strings.Contains(ctx, "kyoto") || !strings.Contains(ctx, "git branch -m") {
		t.Fatalf("naming context = %q", ctx)
	}
	if again := w.worktreeNamingContext(p); again != "" {
		t.Fatalf("naming context repeated: %q", again)
	}
}

func TestDropMissingWorktrees(t *testing.T) {
	n := savedNode{
		Type: "leaf",
		Pane: &savedPane{
			ID:               "p",
			WorkingDirectory: "/tmp/worktrees/kyoto",
			Worktree:         &paneWorktree{Path: "/tmp/worktrees/kyoto"},
		},
	}
	n.dropMissingWorktrees("/tmp/project")
	if n.Pane.WorkingDirectory != "/tmp/project" {
		t.Fatalf("working directory = %q", n.Pane.WorkingDirectory)
	}
	if n.Pane.Worktree != nil {
		t.Fatal("a missing worktree stayed on the pane")
	}
}

func TestReplacingPaneKeepsSlot(t *testing.T) {
	n := &splitNode{
		axis:   axisHorizontal,
		ratio:  50,
		first:  leaf(&pane{ID: "a"}),
		second: leaf(&pane{ID: "b"}),
	}
	np := &pane{ID: "a"}
	n = n.replacing("a", leaf(np))
	if n.paneByID("a") != np {
		t.Fatal("the pane was not replaced")
	}
	if n.paneByID("b") == nil {
		t.Fatal("the other pane was lost")
	}
}
