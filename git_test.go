package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The drawer reads one letter per change, and a rename carries the path it
// came from in a second record.
func TestParseGitStatus(t *testing.T) {
	out := "" +
		"## main...origin/main [ahead 1]\x00" +
		" M main.go\x00" +
		"?? new file.txt\x00" +
		"A  added.go\x00" +
		"R  renamed.go\x00old.go\x00" +
		"UU conflict.txt\x00" +
		"D  gone.go\x00"
	got, branch := parseGitStatus([]byte(out))
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
	want := []changedFile{
		{Path: "added.go", Status: 'A'},
		{Path: "conflict.txt", Status: '!'},
		{Path: "gone.go", Status: 'D'},
		{Path: "main.go", Status: 'M'},
		{Path: "new file.txt", Status: 'U'},
		{Path: "renamed.go", OldPath: "old.go", Status: 'R'},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A real repository: the list carries the change, and the diff has HEAD's
// text on one side and the file on the other.
func TestGitChangesAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "first")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(untracked, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, summary, err := gitSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v, want two", changes)
	}
	if summary.Branch == "" || !summary.Changed || summary.Added != 1 || summary.Deleted != 0 {
		t.Errorf("summary = %+v, want the branch, a change, and one added line", summary)
	}
	byPath := map[string]changedFile{}
	for _, c := range changes {
		byPath[c.Path] = c
	}
	if c := byPath["a.txt"]; c.Status != 'M' {
		t.Errorf("a.txt = %+v, want modified", c)
	}
	if c := byPath["b.txt"]; c.Status != 'U' {
		t.Errorf("b.txt = %+v, want untracked", c)
	}

	d := loadDiff(dir, byPath["a.txt"])
	if d.Old != "one\n" || d.New != "one\ntwo\n" {
		t.Errorf("diff = %+v, want HEAD against the file", d)
	}
	if d := loadDiff(dir, byPath["b.txt"]); d.Old != "" || d.New != "new\n" {
		t.Errorf("untracked diff = %+v, want nothing against the file", d)
	}
}

// A folder Git does not track says so, rather than looking empty.
func TestGitChangesNotRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := gitChanges(dir); err != errNotRepo {
		t.Fatalf("gitChanges in a plain folder = %v, want errNotRepo", err)
	}
}

// The tree lists folders before files, each alphabetical, and leaves .git
// out of the way.
func TestReadDrawerDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z", ".git"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"b.go", "A.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := readDrawerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.name)
	}
	want := []string{"z", "A.md", "b.go"}
	if len(names) != len(want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entries = %v, want %v", names, want)
		}
	}
	if !entries[0].dir {
		t.Errorf("%q should be a folder", entries[0].name)
	}
}
