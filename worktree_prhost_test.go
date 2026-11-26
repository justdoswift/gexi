package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestGitRemoteHost reads the host out of every clone URL form a repository
// may use, and finds none in a local path.
func TestGitRemoteHost(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://github.com/a/b.git", "github.com"},
		{"https://user@gitlab.example.com/a/b.git", "gitlab.example.com"},
		{"git@github.com:a/b.git", "github.com"},
		{"git@git.gitea.example.com:a/b.git", "git.gitea.example.com"},
		{"ssh://git@host.example.com:2222/a/b.git", "host.example.com"},
		{"git://host.example.com/a/b", "host.example.com"},
		{"/Users/someone/repos/b.git", ""},
		{"file:///Users/someone/repos/b", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := gitRemoteHost(c.url); got != c.want {
			t.Errorf("gitRemoteHost(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// fakeGHHosts puts a script where gh would be that answers auth status with
// the given hosts, as gh 2.40 and later do with --json.
func fakeGHHosts(t *testing.T, hosts ...string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	json := `{"hosts":{`
	for i, h := range hosts {
		if i > 0 {
			json += ","
		}
		json += `"` + h + `":[{"state":"success","active":true,"login":"me"}]`
	}
	json += `}}`
	script := "#!/bin/sh\n" +
		`if [ "$1 $2" = "auth status" ]; then printf '%s\n' '` + json + `'; exit 0; fi` + "\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := ghBinaryOverride
	ghBinaryOverride = func() (string, bool) { return path, true }
	t.Cleanup(func() { ghBinaryOverride = prev })
}

// TestRepoIsGitHub checks the controls' host test: github.com answers without
// gh, an Enterprise host answers when gh is signed in to it, and a private
// git server, or no origin at all, keeps them off.
func TestRepoIsGitHub(t *testing.T) {
	withOrigin := func(t *testing.T, url string) string {
		dir := initRepoTest(t)
		runGitTest(t, dir, "remote", "add", "origin", url)
		return dir
	}

	t.Run("github.com needs no gh", func(t *testing.T) {
		prev := ghBinaryOverride
		ghBinaryOverride = func() (string, bool) { return "", true }
		t.Cleanup(func() { ghBinaryOverride = prev })
		if !repoIsGitHub(withOrigin(t, "git@github.com:a/b.git")) {
			t.Fatal("github.com is not recognised")
		}
	})
	t.Run("enterprise host gh is signed in to", func(t *testing.T) {
		fakeGHHosts(t, "github.com", "ghe.example.com")
		if !repoIsGitHub(withOrigin(t, "git@ghe.example.com:a/b.git")) {
			t.Fatal("an Enterprise host gh is signed in to is not recognised")
		}
	})
	t.Run("private host gh does not know", func(t *testing.T) {
		fakeGHHosts(t, "github.com")
		if repoIsGitHub(withOrigin(t, "git@gitlab.example.com:a/b.git")) {
			t.Fatal("a private git server was taken for GitHub")
		}
	})
	t.Run("no origin", func(t *testing.T) {
		fakeGHHosts(t, "github.com")
		if repoIsGitHub(initRepoTest(t)) {
			t.Fatal("a repository with no origin was taken for GitHub")
		}
	})
	t.Run("not a repository", func(t *testing.T) {
		fakeGHHosts(t, "github.com")
		if repoIsGitHub(t.TempDir()) {
			t.Fatal("a plain folder was taken for GitHub")
		}
	})
}

// TestNonGitHubRepoHasNoWorktreeControls checks that a repository on another
// host gets neither the W button that makes a checkout nor the PR chip that
// follows it, while a checkout the pane is already in keeps a chip of its
// branch and its menu.
func TestNonGitHubRepoHasNoWorktreeControls(t *testing.T) {
	dir := t.TempDir()
	newWS := func() *workspace {
		return &workspace{
			prHosts:       map[string]bool{dir: false},
			prHostProbed:  map[string]time.Time{dir: time.Now()},
			prHostProbing: map[string]bool{},
		}
	}

	t.Run("no W button", func(t *testing.T) {
		p := &pane{initialDir: dir}
		w := newWS()
		view := func(c *ui.Context) {
			tk := themeTokens(c.Theme())
			ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
				w.paneWorktreeButton(c, tk, p)
			})
		}
		tester := ui.NewTester(view, 220, 180)
		tester.Frame()
		if _, ok := tester.Find("WD"); ok {
			t.Fatal("a repository without pull requests still offers a worktree")
		}
	})

	t.Run("checkout keeps its branch chip", func(t *testing.T) {
		p := &pane{Worktree: &paneWorktree{Path: dir, Branch: "b", Base: "main"}}
		w := newWS()
		view := func(c *ui.Context) {
			tk := themeTokens(c.Theme())
			ui.Row(c).Fill().Center().Background(tk.Background).Children(func() {
				w.paneWorktreeButton(c, tk, p)
			})
		}
		tester := ui.NewTester(view, 220, 180)
		tester.Frame()
		if _, ok := tester.Find(locales.WorktreePR()); ok {
			t.Fatal("a repository without pull requests still offers PR")
		}
		if _, ok := tester.Find("b"); !ok {
			t.Fatalf("the checkout's branch is not drawn, only %q", tester.Texts())
		}
	})
}
