package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			Branch: "juggernaut",
			Naming: true,
		},
	}
	s := &session{ID: "s", Root: leaf(p)}
	w := &workspace{Projects: []*project{{ID: "pr", Dir: "/tmp", Sessions: []*session{s}}}}

	ctx := w.worktreeNamingContext(p)
	if !strings.Contains(ctx, "juggernaut") || !strings.Contains(ctx, "git branch -m") {
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

func TestDeepSeekCommitMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		var body struct {
			Model    string
			Messages []struct{ Role, Content string }
			Stream   bool
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "deepseek-chat" {
			t.Errorf("model = %q", body.Model)
		}
		if body.Stream {
			t.Error("stream should be false")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "Add login handlers\n"}}},
		})
	}))
	defer srv.Close()
	t.Setenv("DEEPSEEK_BASE_URL", srv.URL)
	t.Setenv("DEEPSEEK_MODEL", "")
	got, err := deepseekCommitMessage("test-key", "prompt")
	if err != nil || got != "Add login handlers" {
		t.Fatalf("message = %q, err = %v", got, err)
	}
}

func TestDeepSeekCommitMessageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid key", http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv("DEEPSEEK_BASE_URL", srv.URL)
	if _, err := deepseekCommitMessage("bad", "prompt"); err == nil {
		t.Fatal("expected an error")
	}
}
