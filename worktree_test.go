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

// TestReturnToBaseSwitchesTheProjectCheckout checks that leaving or archiving
// a worktree ends with the project's own checkout back on the branch the
// worktree was branched from, and that a checkout holding the user's changes
// is left alone.
func TestReturnToBaseSwitchesTheProjectCheckout(t *testing.T) {
	root := initRepoTest(t)
	runGitTest(t, root, "checkout", "-b", "side")
	returnToBase(&paneWorktree{Root: root, Base: "main"})
	if got := currentBranch(root); got != "main" {
		t.Fatalf("clean checkout on %q, want main", got)
	}

	runGitTest(t, root, "checkout", "-b", "side-2")
	if err := os.WriteFile(filepath.Join(root, "own.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	returnToBase(&paneWorktree{Root: root, Base: "main"})
	if got := currentBranch(root); got != "side-2" {
		t.Fatalf("a checkout with changes moved to %q, want side-2", got)
	}
}

// TestDetectWorktreeAt finds a linked worktree from a folder inside it, and
// tells it apart from the repository's main checkout.
func TestDetectWorktreeAt(t *testing.T) {
	root := initRepoTest(t)
	path := filepath.Join(t.TempDir(), "checkout")
	runGitTest(t, root, "worktree", "add", "-b", "feature", path)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", path).Run()
	})

	wt, ok := detectWorktreeAt(path)
	if !ok {
		t.Fatal("the linked worktree was not detected")
	}
	if !wt.Detected {
		t.Fatal("a detected worktree is not marked as one gexi found")
	}
	if wt.Branch != "feature" || wt.Base != "main" {
		t.Fatalf("branch %q base %q, want feature and main", wt.Branch, wt.Base)
	}
	if !sameDir(wt.Path, path) || !sameDir(wt.Root, root) {
		t.Fatalf("path %q root %q, want %q under %q", wt.Path, wt.Root, path, root)
	}

	// A folder inside the checkout belongs to the same worktree.
	sub := filepath.Join(path, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inner, ok := detectWorktreeAt(sub)
	if !ok || !sameDir(inner.Path, wt.Path) {
		t.Fatalf("nested folder detected %+v, %v", inner, ok)
	}

	// The main checkout and a folder outside git are not worktrees.
	if wt, ok := detectWorktreeAt(root); ok {
		t.Fatalf("main checkout reported as worktree: %+v", wt)
	}
	if wt, ok := detectWorktreeAt(t.TempDir()); ok {
		t.Fatalf("plain folder reported as worktree: %+v", wt)
	}
}

// TestWorktreeHasNewWork tells a merged checkout with nothing new from one
// that has uncommitted changes or commits made after the merge: the header
// waits on this to offer the next pull request instead of Archive.
func TestWorktreeHasNewWork(t *testing.T) {
	root := initRepoTest(t)
	path := filepath.Join(t.TempDir(), "checkout")
	runGitTest(t, root, "worktree", "add", "-b", "feature", path)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", path).Run()
	})

	wt := &paneWorktree{Root: root, Path: path, Branch: "feature", Base: "main"}
	mergedHead := runGitTest(t, path, "rev-parse", "HEAD")
	wt.MergedHead = mergedHead
	if got, err := worktreeHasNewWork(wt); err != nil || got {
		t.Fatalf("a clean checkout at the merged commit reports new work: %v, %v", got, err)
	}

	// A merge recorded without the commit falls back to the branch being
	// ahead of its base; this one is not.
	wt.MergedHead = ""
	if got, err := worktreeHasNewWork(wt); err != nil || got {
		t.Fatalf("a branch level with its base reports new work: %v, %v", got, err)
	}
	wt.MergedHead = mergedHead

	if err := os.WriteFile(filepath.Join(path, "later.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := worktreeHasNewWork(wt); err != nil || !got {
		t.Fatalf("an uncommitted change is not new work: %v, %v", got, err)
	}

	runGitTest(t, path, "add", "-A")
	runGitTest(t, path, "commit", "-m", "more")
	if got, err := worktreeHasNewWork(wt); err != nil || !got {
		t.Fatalf("a commit after the merge is not new work: %v, %v", got, err)
	}
	wt.MergedHead = ""
	if got, err := worktreeHasNewWork(wt); err != nil || !got {
		t.Fatalf("a branch ahead of its base is not new work: %v, %v", got, err)
	}
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

func TestSanitizeWorktreeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Fix Login Redirect", "fix-login-redirect"},
		{"  feat/foo_bar  ", "feat-foo-bar"},
		{"修复登录", "修复登录"},
		{"!!!", ""},
		{"a--b", "a-b"},
	}
	for _, c := range cases {
		if got := sanitizeWorktreeName(c.in); got != c.want {
			t.Errorf("sanitizeWorktreeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCreateWorktreeNamed(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktreeNamed(root, "Fix Login Redirect")
	if err != nil || wt == nil {
		t.Fatalf("create named worktree: %v %+v", err, wt)
	}
	if wt.Branch != "fix-login-redirect" || filepath.Base(wt.Path) != wt.Branch {
		t.Fatalf("branch %q path %q, want fix-login-redirect", wt.Branch, wt.Path)
	}
	again, err := createWorktreeNamed(root, "fix-login-redirect")
	if err != nil || again == nil {
		t.Fatalf("create second worktree: %v %+v", err, again)
	}
	if again.Branch != "fix-login-redirect-2" {
		t.Fatalf("second branch = %q, want fix-login-redirect-2", again.Branch)
	}
}

func TestWorktreeHasIgnored(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", ".gitignore")
	runGitTest(t, root, "commit", "-m", "ignore env")
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, ".env"), []byte("SECRET=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignored, err := worktreeHasIgnored(wt)
	if err != nil || !ignored {
		t.Fatalf("ignored = %v, %v; want true", ignored, err)
	}
	if err := os.Remove(filepath.Join(wt.Path, ".env")); err != nil {
		t.Fatal(err)
	}
	if ignored, err := worktreeHasIgnored(wt); err != nil || ignored {
		t.Fatalf("ignored after removal = %v, %v; want false", ignored, err)
	}
}

func TestAgentResumeCommand(t *testing.T) {
	amp := &pane{agent: &agentDetection{Kind: agentAmp, SessionID: "T-123"}}
	if got, want := strings.Join(agentResumeCommand(amp), "\x00"), strings.Join(agentAmp.resume("T-123"), "\x00"); got != want {
		t.Fatalf("amp resume = %q, want %q", got, want)
	}
	claude := &pane{agent: &agentDetection{Kind: agentClaude, SessionID: "abc"}}
	if got := agentResumeCommand(claude); got != nil {
		t.Fatalf("claude resume = %v, want nil: its sessions are per folder", got)
	}
	if got := agentResumeCommand(&pane{}); got != nil {
		t.Fatalf("plain pane resume = %v, want nil", got)
	}
}

func TestStagedSummary(t *testing.T) {
	root := initRepoTest(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "a.txt")
	got := stagedSummary(root)
	if !strings.Contains(got, "1 file changed") {
		t.Fatalf("staged summary = %q, want one file", got)
	}
}

func TestProjectPaneCount(t *testing.T) {
	a := &pane{ID: "a", initialDir: "/repo"}
	b := &pane{ID: "b", initialDir: "/repo/sub"}
	c := &pane{ID: "c", initialDir: "/elsewhere/wt"}
	root := &splitNode{
		axis:  axisHorizontal,
		ratio: 50,
		first: leaf(a),
		second: &splitNode{
			axis: axisVertical, ratio: 50, first: leaf(b), second: leaf(c),
		},
	}
	pr := &project{ID: "pr", Dir: "/repo", Sessions: []*session{{ID: "s", Root: root}}}
	w := &workspace{Projects: []*project{pr}}
	if got := w.projectPaneCount(pr); got != 2 {
		t.Fatalf("project panes = %d, want 2", got)
	}
	w.detached = []*pane{{ID: "d", initialDir: "/repo"}}
	if got := w.projectPaneCount(pr); got != 3 {
		t.Fatalf("project panes with background = %d, want 3", got)
	}
}

// Leaving a checkout deletes its folder, so a pane kept running in the
// background must be found along with the panes on stage.
func TestPanesInWorktreeIncludesBackground(t *testing.T) {
	wt := &paneWorktree{Path: "/repo/wt"}
	onStage := &pane{ID: "a", initialDir: "/repo/wt/sub"}
	elsewhere := &pane{ID: "b", initialDir: "/repo"}
	background := &pane{ID: "c", initialDir: "/repo/wt"}
	root := &splitNode{axis: axisHorizontal, ratio: 50, first: leaf(onStage), second: leaf(elsewhere)}
	pr := &project{ID: "pr", Sessions: []*session{{ID: "s", Root: root}}}
	w := &workspace{Projects: []*project{pr}, detached: []*pane{background}}

	got := w.panesInWorktree(wt)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("panes in worktree = %+v, want a and c", got)
	}
}

// A workspace whose last project is closed still has a Worktrees page: the
// archived branches and the restore commands must survive the relaunch even
// when no project or background pane remains.
func TestLoadStateKeepsWorktreeData(t *testing.T) {
	cases := []struct{ name, body string }{
		{"archived", `{"version":2,"archivedWorktrees":[{"id":"a","path":"/repo/wt","branch":"kept","kept":true}]}`},
		{"restore", `{"version":2,"worktreeRestores":{"/repo/wt":["/bin/zsh","-l"]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GEXI_HOME", t.TempDir())
			if err := os.MkdirAll(filepath.Dir(statePath()), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath(), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			state, ok := loadState()
			if !ok {
				t.Fatalf("%s-only state was dropped", tc.name)
			}
			if tc.name == "archived" && len(state.ArchivedWorktrees) != 1 {
				t.Fatalf("archived = %+v", state.ArchivedWorktrees)
			}
			if tc.name == "restore" && len(state.WorktreeRestores) != 1 {
				t.Fatalf("restores = %+v", state.WorktreeRestores)
			}
		})
	}
}

func TestSavedStateKeepsDetachedPane(t *testing.T) {
	w := newWorkspace()
	p := &pane{
		ID: "p", initialDir: "/repo/wt",
		command:        []string{"/bin/zsh", "-l"},
		restoreCommand: []string{"/bin/zsh", "-l"},
		bgProject:      &project{ID: "pr", Dir: "/repo"},
	}
	w.detached = []*pane{p}
	state := w.savedState()
	if len(state.Detached) != 1 {
		t.Fatalf("detached = %+v, want one pane", state.Detached)
	}
	got := state.Detached[0]
	if got.ProjectDir != "/repo" || got.Pane.ID != "p" || got.Pane.WorkingDirectory != "/repo/wt" {
		t.Fatalf("detached pane = %+v", got)
	}
}

func TestRestoreDetachedPaneMissingDir(t *testing.T) {
	w := newWorkspace()
	sp := savedDetachedPane{Pane: savedPane{ID: "p", WorkingDirectory: "/does/not/exist"}}
	if p := sp.restore(w); p != nil {
		t.Fatalf("restored a pane in a missing folder: %+v", p)
	}
}

func TestMoveChangesToWorktree(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "a.txt")
	runGitTest(t, root, "commit", "-m", "a")

	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := moveChangesToWorktree(root, wt.Path)
	if err != nil {
		t.Fatalf("move changes: %v", err)
	}
	if !moved {
		t.Fatal("changes were not reported as moved")
	}
	if files, err := gitChanges(root); err != nil || len(files) != 0 {
		t.Fatalf("project still has changes: %v %v", files, err)
	}
	if got, _ := os.ReadFile(filepath.Join(wt.Path, "a.txt")); string(got) != "two\n" {
		t.Fatalf("worktree a.txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(wt.Path, "new.txt")); string(got) != "n\n" {
		t.Fatalf("worktree new.txt = %q", got)
	}
	// The staged change arrives staged, not as an unstaged edit.
	if got := runGitTest(t, wt.Path, "diff", "--cached", "--name-only"); got != "staged.txt" {
		t.Fatalf("staged file in the worktree = %q", got)
	}
	if got := runGitTest(t, root, "stash", "list"); got != "" {
		t.Fatalf("stash left behind: %q", got)
	}
}

func TestMoveChangesToWorktreeConflictRestoresProject(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "a.txt")
	runGitTest(t, root, "commit", "-m", "a")

	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})

	// Both sides change the same line, so the pop cannot apply.
	if err := os.WriteFile(filepath.Join(wt.Path, "a.txt"), []byte("worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, wt.Path, "commit", "-am", "worktree")
	if err := os.WriteFile(file, []byte("project\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := moveChangesToWorktree(root, wt.Path)
	if err == nil || moved {
		t.Fatalf("conflicting changes moved: %v %v", moved, err)
	}
	if got, _ := os.ReadFile(file); string(got) != "project\n" {
		t.Fatalf("project file = %q, want its change back", got)
	}
	if got := runGitTest(t, root, "stash", "list"); got != "" {
		t.Fatalf("stash left behind: %q", got)
	}
	if got := runGitTest(t, wt.Path, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree still dirty: %q", got)
	}
}

func TestWorktreeCarriedContextIsOneShot(t *testing.T) {
	p := &pane{
		ID: "p",
		Worktree: &paneWorktree{
			Path:    "/tmp/wt",
			Branch:  "juggernaut",
			Carried: "/tmp/project",
		},
	}
	s := &session{ID: "s", Root: leaf(p)}
	w := &workspace{Projects: []*project{{ID: "pr", Dir: "/tmp", Sessions: []*session{s}}}}

	ctx := w.worktreeCarriedContext(p)
	if !strings.Contains(ctx, "/tmp/project") || !strings.Contains(ctx, "moved into this worktree") {
		t.Fatalf("carried context = %q", ctx)
	}
	if again := w.worktreeCarriedContext(p); again != "" {
		t.Fatalf("carried context repeated: %q", again)
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

func TestDropMissingPlainWorktreePane(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	dir := filepath.Join(worktreesRoot(), "Gexi-abc", "missing")
	n := savedNode{
		Type: "leaf",
		Pane: &savedPane{ID: "p", WorkingDirectory: dir},
	}
	n.dropMissingWorktrees("/tmp/project")
	if n.Pane.WorkingDirectory != "/tmp/project" {
		t.Fatalf("working directory = %q", n.Pane.WorkingDirectory)
	}
}

func TestOrphanWorktreeCleanup(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	if !orphanWorktreeSafe(wt.Path) {
		t.Fatal("a clean worktree with no commits ahead should be safe")
	}
	if err := removeOrphanWorktree(wt.Path); err != nil {
		t.Fatalf("remove orphan: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists: %v", err)
	}
	if out := runGitTest(t, root, "branch", "--list", wt.Branch); out != "" {
		t.Fatalf("branch %q was not deleted: %q", wt.Branch, out)
	}
}

func TestRemoveWorktreeKeepingBranch(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	runGitTest(t, wt.Path, "commit", "--allow-empty", "-m", "work")
	if err := removeWorktreeKeeping(wt, true); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists: %v", err)
	}
	if out := runGitTest(t, root, "branch", "--list", wt.Branch); out == "" {
		t.Fatal("the branch was deleted")
	}
	_ = exec.Command("git", "-C", root, "branch", "-D", wt.Branch).Run()
}

// TestRemoveWorktreeClearsLeftoverFolder checks the removal finishes when
// git dropped the checkout's metadata but could not delete the folder: the
// remains are a plain directory no later scan or git command can see.
func TestRemoveWorktreeClearsLeftoverFolder(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	// Git deletes the worktree's gitdir before the folder, so a read-only
	// subdirectory leaves exactly this: metadata gone, folder still there.
	if err := os.RemoveAll(filepath.Join(root, ".git", "worktrees", wt.Branch)); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(wt.Path, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "file"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	if err := removeWorktreeKeeping(wt, false); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("the leftover folder is still there: %v", err)
	}
	if out := runGitTest(t, root, "branch", "--list", wt.Branch); out != "" {
		t.Fatalf("branch %q was not deleted: %q", wt.Branch, out)
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

func TestSummarizeChecks(t *testing.T) {
	cases := []struct {
		name   string
		checks []prCheck
		want   string
	}{
		{"none", nil, "none"},
		{"success", []prCheck{{Conclusion: "SUCCESS"}}, "passing"},
		{"skipped", []prCheck{{Conclusion: "SKIPPED"}}, "passing"},
		{"failure", []prCheck{{Conclusion: "FAILURE"}}, "failing"},
		{"pending", []prCheck{{Status: "IN_PROGRESS"}}, "pending"},
		{"status context", []prCheck{{State: "PENDING"}}, "pending"},
		{"failure wins", []prCheck{{Status: "IN_PROGRESS"}, {Conclusion: "FAILURE"}}, "failing"},
	}
	for _, c := range cases {
		if got := summarizeChecks(c.checks); got != c.want {
			t.Fatalf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMergeBlocker(t *testing.T) {
	w := &workspace{}
	blocked := []paneWorktree{
		{State: "merged"},
		{State: "closed"},
		{State: "open", mergeable: "CONFLICTING"},
		{State: "open", mergeable: "UNKNOWN"},
		{State: "open", mergeable: ""},
		{State: "open", mergeable: "MERGEABLE", checks: "failing"},
		{State: "open", mergeable: "MERGEABLE", checks: "pending"},
		{State: "open", mergeable: "MERGEABLE", checks: "none", review: "REVIEW_REQUIRED"},
		{State: "open", mergeable: "MERGEABLE", checks: "none", review: "CHANGES_REQUESTED"},
		{State: "open", mergeable: "MERGEABLE", checks: "none", mergeState: "BEHIND"},
	}
	for i := range blocked {
		if got := w.mergeBlocker(&blocked[i]); got == "" {
			t.Fatalf("case %d should block", i)
		}
	}
	ready := []paneWorktree{
		{State: "open", mergeable: "MERGEABLE", checks: "none"},
		{State: "open", mergeable: "MERGEABLE", checks: "passing", review: "APPROVED"},
		{State: "open", mergeable: "MERGEABLE", checks: "none", mergeState: "CLEAN"},
	}
	for i := range ready {
		if got := w.mergeBlocker(&ready[i]); got != "" {
			t.Fatalf("case %d should be ready: %q", i, got)
		}
	}
}

// clearCheckout is what Archive does to the repository: the checkout goes,
// the branch stays, so the archived entry can be restored.
func clearCheckout(t *testing.T, wt *paneWorktree) {
	t.Helper()
	runGitTest(t, wt.Root, "worktree", "remove", "--force", wt.Path)
	runGitTest(t, wt.Root, "worktree", "prune")
}

func TestRestoreArchivedWorktree(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	clearCheckout(t, wt)
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("checkout still there: %v", err)
	}

	a := &archivedWorktree{ID: "a", Root: wt.Root, Path: wt.Path, Branch: wt.Branch, Base: wt.Base}
	if err := restoreWorktree(a); err != nil {
		t.Fatalf("restore: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})
	if info, err := os.Stat(wt.Path); err != nil || !info.IsDir() {
		t.Fatalf("restored checkout missing: %v", err)
	}
	if got := currentBranch(wt.Path); got != wt.Branch {
		t.Fatalf("branch = %q, want %q", got, wt.Branch)
	}
}

func TestRestoreArchivedWorktreeKeepsExistingCheckout(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})
	a := &archivedWorktree{ID: "a", Root: wt.Root, Path: wt.Path, Branch: wt.Branch, Base: wt.Base}
	if err := restoreWorktree(a); err != nil {
		t.Fatalf("restore over an existing checkout: %v", err)
	}
}

// A merged archive restores with the merged header; a branch kept by Leave
// is still unmerged, so its restored pane must offer PR instead of Archive.
func TestWorktreeFromArchiveState(t *testing.T) {
	merged := worktreeFromArchive(archivedWorktree{Branch: "merged", PR: 4})
	if merged.State != worktreeMerged || merged.PR != 4 {
		t.Fatalf("merged state = %q, pr = %d", merged.State, merged.PR)
	}
	kept := worktreeFromArchive(archivedWorktree{Branch: "kept", Kept: true})
	if kept.State != "" {
		t.Fatalf("kept state = %q, want unmerged", kept.State)
	}
}

func TestDeleteArchivedWorktree(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	clearCheckout(t, wt)

	a := &archivedWorktree{ID: "a", Root: wt.Root, Path: wt.Path, Branch: wt.Branch, Base: wt.Base}
	if err := deleteArchived(a); err != nil {
		t.Fatalf("delete archived: %v", err)
	}
	if _, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "refs/heads/"+wt.Branch).CombinedOutput(); err == nil {
		t.Fatalf("branch %s still exists", wt.Branch)
	}
	// Deleting again has nothing left to remove, which is still success.
	if err := deleteArchived(a); err != nil {
		t.Fatalf("delete archived twice: %v", err)
	}
}

func TestDeleteArchivedWorktreeMissingRepo(t *testing.T) {
	a := &archivedWorktree{ID: "a", Root: filepath.Join(t.TempDir(), "gone"), Path: "/tmp/none", Branch: "x"}
	if err := deleteArchived(a); err != nil {
		t.Fatalf("delete with a missing repository: %v", err)
	}
}

func TestCommitWorktree(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt.Path).Run()
	})

	if err := os.WriteFile(filepath.Join(wt.Path, "note.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if clean, err := worktreeIsClean(wt); err != nil || clean {
		t.Fatalf("worktree should start dirty: clean=%v err=%v", clean, err)
	}
	runGitTest(t, wt.Path, "add", "-A")
	if err := commitStaged(wt, "Update from Gexi"); err != nil {
		t.Fatalf("commit worktree: %v", err)
	}
	if clean, err := worktreeIsClean(wt); err != nil || !clean {
		t.Fatalf("worktree should be clean: clean=%v err=%v", clean, err)
	}
	if got := runGitTest(t, wt.Path, "log", "-1", "--format=%s"); got != "Update from Gexi" {
		t.Fatalf("commit subject = %q", got)
	}
	if files := runGitTest(t, wt.Path, "show", "--name-only", "--format=", "HEAD"); files != "note.txt" {
		t.Fatalf("commit files = %q", files)
	}
}

func TestCleanCommitMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Update login flow\n", "Update login flow"},
		{"`fix: names`", "fix: names"},
		{"\"add tests\"", "add tests"},
		{"commit message: add tests", "add tests"},
		{"Commit: tighten copy", "tighten copy"},
		{"  spaced  ", "spaced"},
	}
	for _, c := range cases {
		if got := cleanCommitMessage(c.in); got != c.want {
			t.Fatalf("cleanCommitMessage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanPRBody(t *testing.T) {
	cases := []struct{ in, want string }{
		{"The pane flickered on launch.\nIt now reads the size once.\n", "The pane flickered on launch.\nIt now reads the size once."},
		{"```\nThe pane flickered on launch.\n```", "The pane flickered on launch."},
		{"\"The pane flickered on launch.\"", "The pane flickered on launch."},
		{"One paragraph.\n\nAnother one.", "One paragraph.\n\nAnother one."},
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanPRBody(c.in); got != c.want {
			t.Fatalf("cleanPRBody(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := cleanPRBody(strings.Repeat("word ", 1000)); !strings.HasSuffix(got, "…") || len(got) > 2010 {
		t.Fatalf("cleanPRBody kept a runaway answer as %d characters", len(got))
	}
}

// TestWorktreePaneName shortens only the checkouts gexi made itself: the
// folder exactly one level under worktrees/<repo>/.
func TestWorktreePaneName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEXI_HOME", home)
	root := filepath.Join(home, "worktrees", "Gexi-3a77b02a", "weaver")
	if name, ok := worktreePaneName(root); !ok || name != "weaver" {
		t.Fatalf("worktreePaneName(%q) = %q, %v", root, name, ok)
	}
	for _, dir := range []string{
		filepath.Join(home, "worktrees", "Gexi-3a77b02a"),
		filepath.Join(root, "src"),
		t.TempDir(),
	} {
		if name, ok := worktreePaneName(dir); ok {
			t.Errorf("worktreePaneName(%q) = %q, true", dir, name)
		}
	}
}

// TestAgentHeadless maps the agents that can print a commit message to their
// command; the rest keep the manual dialog.
func TestAgentHeadless(t *testing.T) {
	for _, kind := range []agentKind{agentClaude, agentCodex, agentPi, agentAmp} {
		if _, ok := agentHeadless(kind); !ok {
			t.Errorf("%s has no headless command", kind)
		}
	}
	if _, ok := agentHeadless(agentOmp); ok {
		t.Error("omp should not claim a headless command")
	}
}

// TestAgentResolveCommand covers every agent a pane can run and that has a
// headless mode: each one has to be able to edit the files git left in
// conflict, or the conflict branch offers no way out.
func TestAgentResolveCommand(t *testing.T) {
	for _, kind := range []agentKind{agentClaude, agentCodex, agentPerch, agentPi, agentAmp} {
		if _, ok := agentResolveCommand(kind); !ok {
			t.Errorf("%s cannot resolve conflicts", kind)
		}
	}
	if _, ok := agentResolveCommand(agentOmp); ok {
		t.Error("omp should not claim to resolve conflicts")
	}
}

// TestMergeConflicting reads GitHub's two ways of saying the branch and its
// base conflict.
func TestMergeConflicting(t *testing.T) {
	if !mergeConflicting("CONFLICTING", "CLEAN") || !mergeConflicting("MERGEABLE", "DIRTY") {
		t.Error("conflicts not detected")
	}
	if mergeConflicting("MERGEABLE", "CLEAN") {
		t.Error("clean merge called a conflict")
	}
}

// TestStageResolutionAfterAgent: git keeps a file listed as conflicted until
// what replaced the markers is staged. The agent edits only, so the markers
// are the signal that it finished, and the add has to come before the check.
func TestStageResolutionAfterAgent(t *testing.T) {
	dir := initRepoTest(t)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one\ntwo\n")
	runGitTest(t, dir, "add", "-A")
	runGitTest(t, dir, "commit", "-m", "base")

	runGitTest(t, dir, "checkout", "-b", "other")
	write("one\nTHEIRS\n")
	runGitTest(t, dir, "commit", "-am", "theirs")
	runGitTest(t, dir, "checkout", "main")
	write("one\nOURS\n")
	runGitTest(t, dir, "commit", "-am", "ours")

	if _, err := worktreeGit(dir, "merge", "other"); err == nil {
		t.Fatal("the merge did not conflict")
	}
	files, err := conflictedFiles(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("conflicted files: %v %v", files, err)
	}

	// What the agent does: write the combined result, no git commands.
	write("one\nBOTH\n")
	if err := stageResolution(dir, agentClaude, files); err != nil {
		t.Fatalf("staging the agent's edit: %v", err)
	}
	if left, err := conflictedFiles(dir); err != nil || len(left) != 0 {
		t.Fatalf("still conflicted: %v %v", left, err)
	}
	if got := runGitTest(t, dir, "status", "--porcelain"); got != "M  f.txt" {
		t.Fatalf("staged state: %q", got)
	}
}

// TestStageResolutionRefusesMarkers: an agent that stopped midway leaves
// markers, which must not be committed.
func TestStageResolutionRefusesMarkers(t *testing.T) {
	dir := initRepoTest(t)
	marked := "a\n<<<<<<< HEAD\nb\n=======\nc\n>>>>>>> other\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(marked), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stageResolution(dir, agentClaude, []string{"f.txt"}); err == nil {
		t.Fatal("a file with conflict markers was staged")
	}
}

// TestConflictMarkers names the files an agent left markers in.
func TestConflictMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marked := "a\n<<<<<<< HEAD\nb\n=======\nc\n>>>>>>> origin/main\n"
	if err := os.WriteFile(filepath.Join(dir, "marked.txt"), []byte(marked), 0o644); err != nil {
		t.Fatal(err)
	}
	got := conflictMarkers(dir, []string{"clean.txt", "marked.txt"})
	if len(got) != 1 || got[0] != "marked.txt" {
		t.Fatalf("conflictMarkers = %q", got)
	}
}

// TestWorktreeHasNothingToPR: only a clean checkout with nothing ahead of
// its base is reconciled with a pull request gexi stopped knowing; one with
// work to offer keeps offering PR.
func TestWorktreeHasNothingToPR(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %+v, %v", wt, err)
	}
	if !worktreeHasNothingToPR(wt) {
		t.Fatal("a fresh checkout reports work to offer")
	}
	runGitTest(t, wt.Path, "commit", "--allow-empty", "-m", "work")
	if worktreeHasNothingToPR(wt) {
		t.Fatal("a checkout with a commit reports nothing to offer")
	}
	runGitTest(t, wt.Path, "reset", "--hard", "HEAD~1")
	if !worktreeHasNothingToPR(wt) {
		t.Fatal("the checkout with the commit undone still reports work")
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if worktreeHasNothingToPR(wt) {
		t.Fatal("a dirty checkout reports nothing to offer")
	}
}
