package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// These tests drive the worktree code against real repositories on disk: a
// bare origin where push matters, several repositories created at once, and
// the failures that used to reach the UI as raw git exit statuses.

func initBareTest(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
}

// initRepoWithOriginTest makes a repository whose origin is a local bare
// clone, with main pushed and origin/HEAD set, the shape createWorktree
// reads its base from.
func initRepoWithOriginTest(t *testing.T) (root string) {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	initBareTest(t, origin)
	root = initRepoTest(t)
	runGitTest(t, root, "remote", "add", "origin", origin)
	runGitTest(t, root, "push", "-u", "origin", "main")
	runGitTest(t, root, "remote", "set-head", "origin", "main")
	return root
}

func waitForTest(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition did not happen in time")
}

// TestWorktreeReposWithSameName checks two repositories whose folders share a
// name keep their worktrees apart, including a folder with spaces in its path.
func TestWorktreeReposWithSameName(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	base := t.TempDir()
	roots := []string{
		filepath.Join(base, "one", "project"),
		filepath.Join(base, "two", "my project"),
	}
	for _, root := range roots {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		runGitTest(t, root, "init", "-b", "main")
		runGitTest(t, root, "config", "user.email", "gexi@example.com")
		runGitTest(t, root, "config", "user.name", "Gexi")
		runGitTest(t, root, "commit", "--allow-empty", "-m", "init")
	}
	var paths []string
	for _, root := range roots {
		wt, err := createWorktree(root)
		if err != nil || wt == nil {
			t.Fatalf("create worktree in %s: %v, %v", root, wt, err)
		}
		paths = append(paths, wt.Path)
		if currentBranch(wt.Path) != wt.Branch {
			t.Fatalf("checkout branch %q, want %q", currentBranch(wt.Path), wt.Branch)
		}
		if !worktreeCheckoutExists(wt) {
			t.Fatalf("checkout %s not found", wt.Path)
		}
	}
	if filepath.Dir(paths[0]) == filepath.Dir(paths[1]) {
		t.Fatalf("two repositories share a worktree folder: %s", filepath.Dir(paths[0]))
	}
}

// TestCreateWorktreeNeedsTheRepositoryRoot checks the calls that must decline
// rather than inventing a worktree.
func TestCreateWorktreeNeedsTheRepositoryRoot(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())

	// A folder below the repository root is not the project.
	root := initRepoTest(t)
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if wt, err := createWorktree(sub); err != nil || wt != nil {
		t.Fatalf("subdirectory made a worktree: %+v, %v", wt, err)
	}

	// A repository with no base branch has nothing to branch from.
	trunk := t.TempDir()
	runGitTest(t, trunk, "init", "-b", "trunk")
	runGitTest(t, trunk, "config", "user.email", "gexi@example.com")
	runGitTest(t, trunk, "config", "user.name", "Gexi")
	runGitTest(t, trunk, "commit", "--allow-empty", "-m", "init")
	if wt, err := createWorktree(trunk); err != nil || wt != nil {
		t.Fatalf("a repository without main made a worktree: %+v, %v", wt, err)
	}
}

// TestWorktreeBranchesFromOrigin checks the normal flow: the placeholder
// branch starts at the latest origin base, carries no upstream, and its work
// commits and pushes back to the remote.
func TestWorktreeBranchesFromOrigin(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoWithOriginTest(t)
	origin := runGitTest(t, root, "remote", "get-url", "origin")

	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %+v, %v", wt, err)
	}
	baseHead := runGitTest(t, root, "rev-parse", "origin/main")
	if head := runGitTest(t, wt.Path, "rev-parse", "HEAD"); head != baseHead {
		t.Fatalf("branch starts at %s, want origin/main %s", head, baseHead)
	}
	if _, err := exec.Command("git", "-C", wt.Path, "rev-parse", "--abbrev-ref", "@{u}").CombinedOutput(); err == nil {
		t.Fatal("a new worktree branch tracks an upstream")
	}

	if err := os.WriteFile(filepath.Join(wt.Path, "feature.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if clean, err := worktreeIsClean(wt); err != nil || clean {
		t.Fatalf("a written file is clean: %v, %v", clean, err)
	}
	runGitTest(t, wt.Path, "add", "-A")
	if err := commitStaged(wt, "add the feature"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if clean, err := worktreeIsClean(wt); err != nil || !clean {
		t.Fatalf("a committed checkout is not clean: %v, %v", clean, err)
	}
	if ahead, err := commitsAhead(wt); err != nil || ahead != 1 {
		t.Fatalf("ahead = %d, %v, want 1", ahead, err)
	}
	if err := pushBranch(wt); err != nil {
		t.Fatalf("push: %v", err)
	}
	local := runGitTest(t, wt.Path, "rev-parse", "HEAD")
	remote := strings.TrimSpace(runGitTest(t, origin, "rev-parse", "refs/heads/"+wt.Branch))
	if local != remote {
		t.Fatalf("remote branch at %s, local at %s", remote, local)
	}

	// A second create from the same repository goes to a different folder.
	second, err := createWorktree(root)
	if err != nil || second == nil {
		t.Fatalf("second worktree: %+v, %v", second, err)
	}
	if second.Path == wt.Path || second.Branch == wt.Branch {
		t.Fatalf("two worktrees share %s / %s", second.Path, second.Branch)
	}
}

// TestWorktreeMergeConflictAndAbort covers the conflict path and the rollback
// that aborts it, as the merge flow sees them.
func TestWorktreeMergeConflictAndAbort(t *testing.T) {
	root := initRepoTest(t)
	file := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(file, []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "-A")
	runGitTest(t, root, "commit", "-m", "base file")

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	runGitTest(t, root, "worktree", "add", "-b", "first", first)
	runGitTest(t, root, "worktree", "add", "-b", "second", second)

	if err := os.WriteFile(filepath.Join(first, "shared.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, first, "commit", "-am", "first")
	if err := os.WriteFile(filepath.Join(second, "shared.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, second, "commit", "-am", "second")

	if out, err := exec.Command("git", "-C", first, "merge", "second").CombinedOutput(); err == nil {
		t.Fatalf("the conflicting merge succeeded:\n%s", out)
	}
	files, err := conflictedFiles(first)
	if err != nil {
		t.Fatalf("conflicted files: %v", err)
	}
	if len(files) != 1 || files[0] != "shared.txt" {
		t.Fatalf("conflicted files = %v, want shared.txt", files)
	}
	marked := conflictMarkers(first, files)
	if len(marked) != 1 {
		t.Fatalf("markers = %v, want the file", marked)
	}

	abortMerge(&paneWorktree{Path: first})
	if files, err := conflictedFiles(first); err != nil || len(files) != 0 {
		t.Fatalf("the abort left conflicts: %v, %v", files, err)
	}
	if clean, err := worktreeIsClean(&paneWorktree{Path: first}); err != nil || !clean {
		t.Fatalf("the aborted checkout is not clean: %v, %v", clean, err)
	}
}

// TestArchiveRestoreDeleteRoundTrip follows a merged worktree through the
// Archive, Restore, and Delete actions against disk.
func TestArchiveRestoreDeleteRoundTrip(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %+v, %v", wt, err)
	}
	w := &workspace{}
	w.archiveWorktree(wt, root)
	if len(w.archived) != 1 {
		t.Fatalf("archived entries = %d, want 1", len(w.archived))
	}
	waitForTest(t, 5*time.Second, func() bool {
		_, err := os.Stat(wt.Path)
		return os.IsNotExist(err)
	})
	runGitTest(t, root, "rev-parse", "--verify", "refs/heads/"+wt.Branch)

	entry := w.archived[0]
	if err := restoreWorktree(&entry); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if branch := currentBranch(wt.Path); branch != wt.Branch {
		t.Fatalf("restored branch %q, want %q", branch, wt.Branch)
	}
	if err := deleteArchived(&entry); err != nil {
		t.Fatalf("delete archived: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("the checkout survived the delete: %v", err)
	}
	if _, err := worktreeGit(root, "rev-parse", "--verify", "refs/heads/"+wt.Branch); err == nil {
		t.Fatal("the branch survived the delete")
	}
}

// TestRestoreArchivedFailures checks the two ways a restore must refuse:
// the branch is gone, or the folder is occupied by another branch.
func TestRestoreArchivedFailures(t *testing.T) {
	root := initRepoTest(t)
	path := filepath.Join(t.TempDir(), "checkout")

	gone := archivedWorktree{Root: root, Path: path, Branch: "gone"}
	if err := restoreWorktree(&gone); err == nil {
		t.Fatal("a missing branch was restored")
	}

	runGitTest(t, root, "worktree", "add", "-b", "taken", path)
	taken := archivedWorktree{Root: root, Path: path, Branch: "other"}
	if err := restoreWorktree(&taken); err == nil {
		t.Fatal("an occupied checkout was restored")
	}
}

// TestWorktreeRowsUnifyAllKinds checks the Worktrees page finds every state in
// one list, in order: background, open, on disk, archived.
func TestWorktreeRowsUnifyAllKinds(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	open, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	background, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	idle, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}

	w := &workspace{}
	w.Projects = []*project{{ID: "p", Dir: root, Sessions: []*session{{
		ID:   "s",
		Root: leaf(&pane{ID: "open", Worktree: open, initialDir: open.Path}),
	}}}}
	w.detached = []*pane{{ID: "bg", Worktree: background, initialDir: background.Path}}
	w.archived = []archivedWorktree{{
		ID: "old", ProjectDir: root, Root: root,
		Path: filepath.Join(t.TempDir(), "old"), Branch: "old",
	}}

	rows := w.worktreeRows()
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4: %+v", len(rows), rows)
	}
	if !rows[0].background || rows[0].entry.Path != background.Path {
		t.Fatalf("first row = %+v, want the background checkout", rows[0])
	}
	if rows[0].status() != locales.WorktreeStatusBackground() {
		t.Fatalf("first status = %q", rows[0].status())
	}
	if rows[1].pane == nil || rows[1].background || rows[1].entry.Path != open.Path {
		t.Fatalf("second row = %+v, want the open checkout", rows[1])
	}
	if rows[2].pane != nil || rows[2].entry.Path != idle.Path {
		t.Fatalf("third row = %+v, want the idle checkout", rows[2])
	}
	if rows[3].archived == nil {
		t.Fatalf("last row = %+v, want the archived entry", rows[3])
	}
}

// TestDropGoneWorktree checks a checkout deleted under the running app is
// cleared from its pane instead of being handed to git again.
func TestDropGoneWorktree(t *testing.T) {
	w := &workspace{worktreesScanned: true}
	gone := &pane{Worktree: &paneWorktree{
		Path: filepath.Join(t.TempDir(), "gone"), Branch: "gone", Base: "main",
	}}
	if w.worktreeReady(gone) {
		t.Fatal("a missing checkout is ready")
	}
	if gone.Worktree != nil {
		t.Fatal("the missing binding was kept")
	}
	if w.worktreesScanned {
		t.Fatal("the Worktrees page was not asked to scan again")
	}

	alive := &pane{Worktree: &paneWorktree{Path: t.TempDir(), Branch: "alive", Base: "main"}}
	if !w.worktreeReady(alive) || alive.Worktree == nil {
		t.Fatal("a checkout on disk was dropped")
	}

	raw := &paneWorktree{Path: filepath.Join(t.TempDir(), "gone")}
	if _, err := worktreeGit(raw.Path, "status"); err == nil {
		t.Fatal("git in a missing folder unexpectedly succeeded; the guard is not exercised")
	}
}

// fakeGHTEST puts a script where gh would be. It answers pr view from a
// state file that pr create writes, so creation, reuse, and failures all run
// without the network.
func fakeGHTEST(t *testing.T) (stateFile string) {
	t.Helper()
	dir := t.TempDir()
	stateFile = filepath.Join(dir, "pr.json")
	path := filepath.Join(dir, "gh")
	script := `#!/bin/sh
case "$1 $2" in
"pr create")
  if [ "$FAKE_GH_CREATE_FAIL" = "1" ]; then echo "create failed" >&2; exit 1; fi
  printf '{"number":17,"url":"https://example.invalid/pr/17","state":"OPEN","mergeable":"MERGEABLE","reviewDecision":"","mergeStateStatus":"CLEAN","statusCheckRollup":[]}\n' > "$FAKE_GH_STATE_FILE"
  ;;
"pr view")
  if [ ! -f "$FAKE_GH_STATE_FILE" ]; then
    echo "no pull requests found for branch" >&2
    exit 1
  fi
  cat "$FAKE_GH_STATE_FILE"
  ;;
"pr merge")
  if [ "$FAKE_GH_MERGE_FAIL" = "1" ]; then echo "merge blocked" >&2; exit 1; fi
  ;;
*)
  exit 1
  ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := ghBinaryOverride
	ghBinaryOverride = func() (string, bool) { return path, true }
	t.Cleanup(func() { ghBinaryOverride = prev })
	t.Setenv("FAKE_GH_STATE_FILE", stateFile)
	return stateFile
}

// TestCreatePRFlow covers the pull request path with a stubbed gh: a clean
// branch with commits pushes and opens a PR, dirty work commits first, and an
// open PR is reused rather than duplicated.
func TestCreatePRFlow(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	stateFile := fakeGHTEST(t)
	root := initRepoWithOriginTest(t)
	origin := runGitTest(t, root, "remote", "get-url", "origin")

	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "change.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, wt.Path, "add", "-A")
	if err := commitStaged(wt, "the change"); err != nil {
		t.Fatal(err)
	}
	info, branch, err := createPR(wt, "", nil)
	if err != nil {
		t.Fatalf("create PR: %v", err)
	}
	if info.Number != 17 || info.URL == "" || branch != wt.Branch {
		t.Fatalf("PR = %+v branch %q, want #17 on %q", info, branch, wt.Branch)
	}
	runGitTest(t, origin, "rev-parse", "refs/heads/"+wt.Branch)

	// The same branch with an existing open PR is reused, not pushed again.
	os.Remove(stateFile)
	early, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(early.Path, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, early.Path, "add", "-A")
	if err := commitStaged(early, "x"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte(`{"number":23,"url":"https://example.invalid/pr/23","state":"OPEN","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reused, _, err := createPR(early, "", nil)
	if err != nil || reused.Number != 23 {
		t.Fatalf("existing PR = %+v, %v, want #23", reused, err)
	}
	if _, err := exec.Command("git", "-C", origin, "rev-parse", "--verify", "refs/heads/"+early.Branch).CombinedOutput(); err == nil {
		t.Fatal("reusing an open PR pushed the branch again")
	}
}

// TestCreatePRDirtyFlow checks uncommitted work: a message commits it first,
// and no message is refused before anything is pushed.
func TestCreatePRDirtyFlow(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	fakeGHTEST(t)
	root := initRepoWithOriginTest(t)
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := createPR(wt, "", nil); err == nil || !strings.Contains(err.Error(), "commit message") {
		t.Fatalf("dirty with no message = %v, want the message refusal", err)
	}
	info, _, err := createPR(wt, "commit the dirty file", nil)
	if err != nil || info.Number != 17 {
		t.Fatalf("dirty with a message = %+v, %v", info, err)
	}
	if clean, err := worktreeIsClean(wt); err != nil || !clean {
		t.Fatalf("the checkout after the commit is not clean: %v, %v", clean, err)
	}
}

// TestCreatePRRefusals covers the local errors that must not reach gh.
func TestCreatePRRefusals(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	fakeGHTEST(t)
	root := initRepoWithOriginTest(t)
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := createPR(wt, "", nil); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("a branch with no commits = %v, want the no-commits error", err)
	}

	// A failing gh carries its message through.
	runGitTest(t, wt.Path, "commit", "--allow-empty", "-m", "work")
	t.Setenv("FAKE_GH_CREATE_FAIL", "1")
	if _, _, err := createPR(wt, "", nil); err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("a failing gh = %v, want create failed", err)
	}
}

// TestMergePRChecks covers the merge's own refusals and its success path.
func TestMergePRChecks(t *testing.T) {
	fakeGHTEST(t)
	root := initRepoTest(t)
	path := filepath.Join(t.TempDir(), "checkout")
	runGitTest(t, root, "worktree", "add", "-b", "merged", path)
	wt := &paneWorktree{Root: root, Path: path, Branch: "merged", Base: "main", PR: 17}

	if err := os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergePR(wt); err == nil || !strings.Contains(err.Error(), "commit the changes") {
		t.Fatalf("a dirty merge = %v, want the clean-first refusal", err)
	}
	runGitTest(t, path, "add", "-A")
	runGitTest(t, path, "commit", "-m", "clean")

	t.Setenv("FAKE_GH_MERGE_FAIL", "1")
	if err := mergePR(wt); err == nil || !strings.Contains(err.Error(), "merge blocked") {
		t.Fatalf("a blocked merge = %v, want the gh message", err)
	}
	t.Setenv("FAKE_GH_MERGE_FAIL", "")
	if err := mergePR(wt); err != nil {
		t.Fatalf("merge: %v", err)
	}
}

// TestGhMissing checks the app's advice path when gh is not installed.
func TestGhMissingCheck(t *testing.T) {
	prev := ghBinaryOverride
	ghBinaryOverride = func() (string, bool) { return "", true }
	t.Cleanup(func() { ghBinaryOverride = prev })
	if ghBinary() != "" {
		t.Fatal("the missing-gh override was not used")
	}
	root := initRepoTest(t)
	if _, _, err := createPR(&paneWorktree{Root: root, Path: root, Branch: "x", Base: "main"}, "", nil); !errors.Is(err, errGhMissing) {
		t.Fatalf("create PR without gh = %v, want errGhMissing", err)
	}
}

func TestWorktreeCheckoutInfoBranchWithSlash(t *testing.T) {
	root := initRepoTest(t)
	path := filepath.Join(t.TempDir(), "checkout")
	runGitTest(t, root, "worktree", "add", "-b", "feat/slash", path)
	branch, repoRoot := worktreeCheckoutInfo(path)
	if branch != "feat/slash" {
		t.Fatalf("branch = %q, want feat/slash", branch)
	}
	if !sameDir(repoRoot, root) {
		t.Fatalf("root = %q, want %q", repoRoot, root)
	}
}

// TestCleanupKeepsUsedAndDirtyWorktrees checks the page's Clean up touches
// only checkouts no pane uses, with nothing to lose: a background pane marks
// its checkout used, and uncommitted work is never removed.
func TestCleanupKeepsUsedAndDirtyWorktrees(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	used, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	spare, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty.Path, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &workspace{detached: []*pane{{Worktree: used, initialDir: used.Path}}}
	w.cleanupOrphanWorktrees()
	waitForTest(t, 5*time.Second, func() bool {
		_, err := os.Stat(spare.Path)
		return os.IsNotExist(err)
	})
	time.Sleep(100 * time.Millisecond)
	if !worktreeCheckoutExists(used) {
		t.Fatal("a background pane's checkout was removed")
	}
	if !worktreeCheckoutExists(dirty) {
		t.Fatal("a dirty checkout was removed")
	}
}

// TestKeepClosedWorktreeLeavesCheckout checks closing a pane no longer
// deletes the checkout, the way a mistyped ⌘W used to lose it.
func TestKeepClosedWorktreeLeavesCheckout(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	w := &workspace{}
	p := &pane{
		ID:             "p",
		Worktree:       wt,
		initialDir:     wt.Path,
		restoreCommand: []string{"claude", "--resume", "abc"},
	}
	w.keepClosedWorktree(p)
	if !worktreeCheckoutExists(wt) {
		t.Fatal("closing removed the checkout")
	}
	if got := w.worktreeRestores[filepath.Clean(wt.Path)]; len(got) != 3 || got[0] != "claude" {
		t.Fatalf("restore command = %v, want the pane's agent", got)
	}
	runGitTest(t, root, "rev-parse", "--verify", "refs/heads/"+wt.Branch)
}

// TestDetachPaneModel checks closing a pane into the background keeps its
// process binding, drops only its leaf, and remembers where to put it back.
func TestDetachPaneModel(t *testing.T) {
	root := initRepoTest(t)
	a := &pane{ID: "a", initialDir: root}
	b := &pane{ID: "b", initialDir: root}
	pr := &project{ID: "p", Dir: root, Sessions: []*session{{
		ID: "s", Root: &splitNode{axis: axisHorizontal, ratio: 50, first: leaf(a), second: leaf(b)},
	}}}
	w := &workspace{Projects: []*project{pr}, SelectedSession: "s", FocusedPane: "a"}
	w.detachPane(a)
	if len(w.detached) != 1 || w.detached[0] != a {
		t.Fatalf("detached = %+v, want the pane", w.detached)
	}
	if w.sessionOf(a) != nil {
		t.Fatal("the detached pane is still in the layout")
	}
	if s := w.sessionOf(b); s == nil || len(s.Root.panes()) != 1 {
		t.Fatalf("the sibling did not keep its place: %+v", s)
	}
	if a.bgProject != pr {
		t.Fatal("the pane did not remember its project")
	}
	if w.FocusedPane != "b" {
		t.Fatalf("focus = %q, want the sibling", w.FocusedPane)
	}
	// Detaching again is a no-op, not a second entry.
	w.detachPane(a)
	if len(w.detached) != 1 {
		t.Fatalf("detached twice = %d entries", len(w.detached))
	}
}

// TestWorktreeRowsFindPaneByFolder checks a pane opened in a checkout without
// worktree metadata still marks that checkout as open on the page.
func TestWorktreeRowsFindPaneByFolder(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	w := &workspace{}
	w.Projects = []*project{{ID: "p", Dir: root, Sessions: []*session{{
		ID:   "s",
		Root: leaf(&pane{ID: "plain", initialDir: wt.Path}),
	}}}}
	rows := w.worktreeRows()
	if len(rows) != 1 || rows[0].pane == nil || rows[0].background {
		t.Fatalf("rows = %+v, want the checkout marked open", rows)
	}
	if rows[0].status() != locales.WorktreeStatusOpen() {
		t.Fatalf("status = %q, want open", rows[0].status())
	}
}

// TestWorktreesPageDrawsEveryState checks the unified page renders the branch
// and state of a background, open, on-disk, and archived worktree.
func TestWorktreesPageDrawsEveryState(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	open, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	background, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	idle, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	w := &workspace{}
	w.Projects = []*project{{ID: "p", Dir: root, Sessions: []*session{{
		ID:   "s",
		Root: leaf(&pane{ID: "open", Worktree: open, initialDir: open.Path}),
	}}}}
	w.detached = []*pane{{ID: "bg", Worktree: background, initialDir: background.Path}}
	w.archived = []archivedWorktree{{
		ID: "old", ProjectDir: root, Root: root,
		Path: filepath.Join(t.TempDir(), "old"), Branch: "old-branch",
	}}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(20, 28, 32, 28).Gap(16).Children(func() {
				w.worktreesPage(c, tk)
			})
		})
	}
	tester := ui.NewTester(view, 760, 900)
	tester.Frame()
	want := []string{
		open.Branch, background.Branch, idle.Branch, "old-branch",
		locales.WorktreeStatusBackground(), locales.WorktreeStatusOpen(),
		locales.WorktreeStatusOnDisk(), locales.WorktreeStatusArchived(),
	}
	for _, text := range want {
		if _, ok := tester.Find(text); !ok {
			t.Fatalf("the page does not show %q; it shows %q", text, tester.Texts())
		}
	}
}

// TestWorktreesPageShowsClearProgress checks a batch deletion reports how far
// it has got in the row that started it, with the spinner where the delete
// button was, instead of leaving the page looking idle while git works. It
// also checks the button asks first in the app's own dialog.
func TestWorktreesPageShowsClearProgress(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	w := &workspace{settingsTab: settingsWorktrees}
	w.archived = []archivedWorktree{
		{ID: "a", ProjectDir: "/tmp/project", Root: "/tmp/project", Path: "/tmp/project/a", Branch: "a"},
		{ID: "b", ProjectDir: "/tmp/project", Root: "/tmp/project", Path: "/tmp/project/b", Branch: "b"},
	}
	w.archivedClear = &archivedClearProgress{done: 1, total: 2}
	tester := ui.NewTester(w.settingsView, 880, 700)
	tester.Frame()
	progress := locales.WorktreeClearProgress("1", "2")
	r, ok := tester.Find(progress)
	if !ok {
		t.Fatalf("the page does not show %q; it shows %q", progress, tester.Texts())
	}
	if r.W != 12 || r.H != 12 {
		t.Fatalf("the progress spinner takes %vx%v, want 12x12", r.W, r.H)
	}
	if !tester.HasText(locales.WorktreeClearArchivedDetail("2")) {
		t.Fatalf("the row lost its detail while clearing; it shows %q", tester.Texts())
	}

	// Once the batch is done, the button asks first, in the app's own
	// dialog rather than a system sheet.
	w.archivedClear = nil
	tester.Frame()
	if err := tester.Click(locales.ArchiveDelete()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !w.clearArchivedOpen {
		t.Fatal("Delete started the batch without asking")
	}
	if !tester.HasText(locales.WorktreeClearArchivedTitle()) {
		t.Fatalf("the confirmation is not drawn; the page shows %q", tester.Texts())
	}
}

// TestArchiveDialogShowsWhatGoes checks the archive confirmation names the
// branch and separates the files that go with the folder, so the destructive
// part is not buried in one paragraph.
func TestArchiveDialogShowsWhatGoes(t *testing.T) {
	w := &workspace{
		archiveTarget:  &pane{ID: "p"},
		archiveOpen:    true,
		archiveBranch:  "feat/sidebar-search",
		archiveDirty:   true,
		archiveIgnored: true,
	}
	tester := ui.NewTester(func(c *ui.Context) {
		w.archiveWorktreeDialog(c, themeTokens(c.Theme()))
	}, 600, 500)
	tester.Frame()
	for _, text := range []string{
		"feat/sidebar-search",
		locales.WorktreeArchiveBody(),
		locales.WorktreeArchiveKeep(),
		locales.WorktreeArchiveDirty(),
		locales.WorktreeArchiveIgnored(),
	} {
		if !tester.HasText(text) {
			t.Fatalf("the dialog does not show %q; it shows %q", text, tester.Texts())
		}
	}
}

// TestCoreGitRefusesMissingCheckout checks the PR and merge cores themselves
// stop on a deleted checkout, not only the header that calls them.
func TestCoreGitRefusesMissingCheckout(t *testing.T) {
	fakeGHTEST(t)
	gone := &paneWorktree{
		Path:   filepath.Join(t.TempDir(), "gone"),
		Branch: "gone", Base: "main", PR: 5,
	}
	if _, _, err := createPR(gone, "message", nil); !errors.Is(err, errWorktreeGone) {
		t.Fatalf("create PR in a missing folder = %v, want errWorktreeGone", err)
	}
	if err := mergePR(gone); !errors.Is(err, errWorktreeGone) {
		t.Fatalf("merge in a missing folder = %v, want errWorktreeGone", err)
	}
}

// TestRequestCloseByUserAsks checks a pane where an agent ran opens the
// close alert instead of closing at once, and a plain shell still closes
// straight away.
func TestRequestCloseByUserAsks(t *testing.T) {
	w := &workspace{}
	agentPane := &pane{ID: "agent", sawAgent: true}
	w.requestCloseByUser(agentPane)
	if w.closeTarget != agentPane || !w.closeOpen {
		t.Fatalf("an agent pane closed without asking: target=%v open=%v", w.closeTarget, w.closeOpen)
	}
	if agentPane.closing {
		t.Fatal("the agent pane started closing while the alert is up")
	}
	if w.closeMsg == "" {
		t.Fatal("the alert has no message")
	}

	w = &workspace{}
	resumed := &pane{ID: "resumed", lastAgentRestore: []string{"claude", "--resume", "x"}}
	w.requestCloseByUser(resumed)
	if w.closeTarget != resumed {
		t.Fatal("a pane with a saved resume closed without asking")
	}

	w = &workspace{}
	plain := &pane{ID: "plain"}
	w.requestCloseByUser(plain)
	if w.closeTarget != nil || !plain.closing {
		t.Fatal("a plain shell did not close at once")
	}
}

// TestCloseProjectAsks checks every project close stops at the alert, even
// with no agent in it, and that the alert says what the close takes with it.
func TestCloseProjectAsks(t *testing.T) {
	plain := &pane{ID: "plain"}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{{ID: "s", Root: leaf(plain)}}}
	w := &workspace{Projects: []*project{pr}}
	w.closeProject(pr)
	if !w.closeProjectOpen || w.closeProjectTarget != pr {
		t.Fatalf("the project closed without asking: open=%v target=%v", w.closeProjectOpen, w.closeProjectTarget)
	}
	if plain.closing {
		t.Fatal("the panes started closing while the alert is up")
	}
	if w.closeProjectMsg != locales.PaneCloseProjectBodyIdle() {
		t.Fatalf("message = %q, want the plain close consequence", w.closeProjectMsg)
	}

	agent := &pane{ID: "agent", sawAgent: true}
	pr = &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{{ID: "s", Root: leaf(agent)}}}
	w = &workspace{Projects: []*project{pr}}
	w.closeProject(pr)
	if w.closeProjectMsg != locales.PaneCloseProjectBody() {
		t.Fatalf("message = %q, want the agent close consequence", w.closeProjectMsg)
	}
}

// TestCloseProjectDialogActions checks Cancel leaves the project in place and
// Close removes it, through the buttons the alert actually draws.
func TestCloseProjectDialogActions(t *testing.T) {
	pr := &project{ID: "pr", Dir: t.TempDir()}
	w := &workspace{Projects: []*project{pr}}
	w.closeProject(pr)
	tester := ui.NewTester(func(c *ui.Context) {
		w.closeProjectDialog(c, themeTokens(c.Theme()))
	}, 600, 500)
	tester.Frame()
	if !tester.HasText(locales.PaneCloseProjectTitle()) || !tester.HasText(locales.PaneCloseProjectBodyIdle()) {
		t.Fatalf("the alert does not show the close question; it shows %q", tester.Texts())
	}
	if err := tester.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if w.closeProjectTarget != nil || len(w.Projects) != 1 {
		t.Fatal("Cancel closed the project")
	}

	w.closeProject(pr)
	tester.Frame()
	if err := tester.Click(locales.CloseProject()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if len(w.Projects) != 0 || w.closeProjectTarget != nil {
		t.Fatal("Close left the project open")
	}
}
