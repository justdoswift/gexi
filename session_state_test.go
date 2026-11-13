package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// Putting a session away has to keep everything that opening it again needs:
// the split, each pane's id and command, the zoom, and the names the row
// showed.
func TestStoredSessionKeepsTheTree(t *testing.T) {
	one := &pane{ID: "one", Label: "first", restoreCommand: []string{"claude", "--resume", "abc"}}
	two := &pane{ID: "two", restoreCommand: []string{"/bin/zsh"}}
	s := &session{
		ID:         "session",
		Label:      "Fix login redirect",
		Root:       leaf(one).splitting("one", axisHorizontal, two),
		zoomedPane: "two",
	}
	pr := &project{ID: "pr", Dir: "/tmp/gexi-project", Label: "gexi"}

	rec := storedSessionFor(s, pr)
	if rec.Session.ID != "session" || rec.Session.Label != "Fix login redirect" ||
		rec.Session.ZoomedPane != "two" {
		t.Fatalf("stored session = %+v", rec)
	}
	if rec.ProjectDir != pr.Dir || rec.ProjectLabel != "gexi" || rec.Title != "Fix login redirect" {
		t.Fatalf("stored place = %+v", rec)
	}
	if rec.Session.Root.Type != "split" ||
		rec.Session.Root.First.Pane.ID != "one" ||
		rec.Session.Root.Second.Pane.ID != "two" ||
		rec.Session.Root.Second.Pane.Command[0] != "/bin/zsh" {
		t.Fatalf("stored tree = %+v", rec.Session.Root)
	}
	if got := savedPaneIDs(rec.Session.Root); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("saved pane ids = %v", got)
	}
}

// The records live in workspace.json, not only in memory: a launch puts the
// idle and archived sessions back where they were.
func TestIdleAndArchivedSessionsSurviveALaunch(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	w := &workspace{settings: defaultSettings()}
	w.idleSessions = []storedSession{{
		Session:      savedSession{ID: "p", Label: "Idle work", Root: savedNode{Type: "leaf", Pane: &savedPane{ID: "pane"}}},
		ProjectDir:   "/tmp/project",
		ProjectLabel: "project",
		Title:        "Idle work",
	}}
	w.archivedSessions = []storedSession{{
		Session:      savedSession{ID: "a", Label: "Archived work", Root: savedNode{Type: "leaf", Pane: &savedPane{ID: "pane"}}},
		ProjectDir:   "/tmp/project",
		ProjectLabel: "project",
		Title:        "Archived work",
	}}
	w.saveNow()

	state, ok := loadState()
	if !ok {
		t.Fatal("the layout did not load")
	}
	if len(state.IdleSessions) != 1 || state.IdleSessions[0].Session.ID != "p" {
		t.Fatalf("idle sessions = %+v", state.IdleSessions)
	}
	if len(state.ArchivedSessions) != 1 || state.ArchivedSessions[0].Session.ID != "a" {
		t.Fatalf("archived sessions = %+v", state.ArchivedSessions)
	}
}

// The sidebar fold is closed until it is opened, and the waiting rows are not
// drawn before then.
func TestIdleSectionStartsCollapsed(t *testing.T) {
	w := &workspace{idleSessions: []storedSession{{
		Session:      savedSession{ID: "p", Root: savedNode{Type: "leaf", Pane: &savedPane{ID: "pane"}}},
		ProjectDir:   "/tmp/project",
		ProjectLabel: "gexi",
		Title:        "Idle work",
	}}}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Padding(8).Children(func() {
			w.idleSessionSection(c, tk)
		})
	}
	tester := ui.NewTester(view, 280, 220)
	tester.Frame()
	if !tester.HasText(locales.SessionIdle()) {
		t.Fatalf("the fold is not drawn; it shows %q", tester.Texts())
	}
	if tester.HasText("Idle work") {
		t.Fatal("an idle session was drawn while the fold was closed")
	}
	if err := tester.Click(locales.SessionIdle()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("Idle work") {
		t.Fatalf("opening the fold does not show the session; it shows %q", tester.Texts())
	}
	if !w.idleOpen {
		t.Fatal("the fold did not remember that it was opened")
	}
}

// An idle session can be put in the archive without opening it again.
func TestArchiveIdleSessionMovesItToSettings(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	w.idleSessions = []storedSession{{
		Session: savedSession{ID: "i", Root: savedNode{Type: "leaf", Pane: &savedPane{ID: "pane"}}},
		Title:   "Waiting work",
	}}
	w.archiveIdleSession("i")
	if len(w.idleSessions) != 0 {
		t.Fatalf("the idle row stayed: %+v", w.idleSessions)
	}
	if len(w.archivedSessions) != 1 || w.archivedSessions[0].Session.ID != "i" {
		t.Fatalf("the archive did not receive the session: %+v", w.archivedSessions)
	}
}

// Settings › Archive shows each saved session with the two actions it has:
// open it again, or delete it for good.
func TestArchivePageListsSessionsAndAsksBeforeDeleting(t *testing.T) {
	w := &workspace{settings: defaultSettings(), settingsTab: settingsArchive}
	w.archivedSessions = []storedSession{
		{
			Session:      savedSession{ID: "a", Root: savedNode{Type: "leaf", Pane: &savedPane{ID: "one"}}},
			ProjectDir:   "/tmp/project",
			ProjectLabel: "gexi",
			Title:        "One pane task",
		},
		{
			Session:      savedSession{ID: "b", Root: savedNode{Type: "split", First: &savedNode{Type: "leaf", Pane: &savedPane{ID: "two"}}, Second: &savedNode{Type: "leaf", Pane: &savedPane{ID: "three"}}}},
			ProjectDir:   "/tmp/project",
			ProjectLabel: "gexi",
			Title:        "Two pane task",
		},
	}
	tester := ui.NewTester(w.settingsView, 880, 700)
	tester.Frame()
	for _, text := range []string{
		locales.SettingsArchiveSessions(),
		"One pane task",
		"Two pane task",
		locales.SessionPaneCountOne(),
		locales.SessionPaneCount("2"),
		locales.SessionRestore(),
		locales.ArchiveDelete(),
	} {
		if !tester.HasText(text) {
			t.Fatalf("the page does not show %q; it shows %q", text, tester.Texts())
		}
	}
	if err := tester.Click(locales.ArchiveDelete()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !w.archivedDeleteOpen {
		t.Fatal("Delete ran without asking")
	}
	if !tester.HasText(locales.ArchiveSessionDeleteTitle()) {
		t.Fatalf("the confirmation is not drawn; it shows %q", tester.Texts())
	}
}

// Deleting an archived session forgets its record and the screens kept for
// its panes.
func TestDeleteArchivedSessionDropsSnapshots(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	if err := os.MkdirAll(snapshotsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(snapshotsDir(), "one.txt")
	if err := os.WriteFile(snapshot, []byte("last screen"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &workspace{settings: defaultSettings()}
	w.archivedSessions = []storedSession{{
		Session: savedSession{ID: "a", Root: savedNode{Type: "leaf", Pane: &savedPane{ID: "one"}}},
		Title:   "Archived work",
	}}
	w.deleteArchivedSession("a")
	if len(w.archivedSessions) != 0 {
		t.Fatalf("the record stayed: %+v", w.archivedSessions)
	}
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("the snapshot was not removed: %v", err)
	}
}

func TestSavedSessionDirFindsTheFirstPane(t *testing.T) {
	root := savedNode{
		Type:  "split",
		First: &savedNode{Type: "leaf", Pane: &savedPane{ID: "one", WorkingDirectory: "/tmp/one"}},
		Second: &savedNode{Type: "leaf", Pane: &savedPane{
			ID: "two", WorkingDirectory: "/tmp/two", Worktree: &paneWorktree{Root: "/tmp/repo"},
		}},
	}
	if got := savedSessionDir(root); got != "/tmp/one" {
		t.Fatalf("dir = %q, want the first pane's folder", got)
	}
}
