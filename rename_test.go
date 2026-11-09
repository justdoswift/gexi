package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A session is named after its branch until the user renames it; the manual
// name then outranks the branch and the agent's name, so an agent renaming
// its branch cannot change what the sidebar shows.
func TestSessionLabelKeepsTheManualName(t *testing.T) {
	s := &session{
		ID:    "s",
		Label: "Fix login redirect",
		Root: leaf(&pane{
			ID:       "p",
			agent:    &agentDetection{Kind: agentClaude},
			Worktree: &paneWorktree{Branch: "fix-login-redirect"},
		}),
	}
	if got := sessionLabel(s); got != "Fix login redirect" {
		t.Fatalf("label = %q, want the manual name", got)
	}
	s.Label = ""
	if got := sessionLabel(s); got != "fix-login-redirect" {
		t.Fatalf("label without a manual name = %q, want the branch", got)
	}
}

// The manual name is part of the layout, so it comes back after a launch.
func TestSessionLabelIsSaved(t *testing.T) {
	s := &session{ID: "s", Label: "Fix login redirect", Root: leaf(&pane{ID: "p"})}
	w := &workspace{Projects: []*project{{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}}}

	state := w.savedState()
	if len(state.Projects) != 1 || len(state.Projects[0].Sessions) != 1 {
		t.Fatalf("saved state: %+v", state)
	}
	if got := state.Projects[0].Sessions[0].Label; got != "Fix login redirect" {
		t.Fatalf("saved label = %q", got)
	}
}

// The sidebar row's right-click menu renames the session there: the menu
// opens on the row, the prompt takes the name, and the rows shows it at once.
func TestSessionRowMenuRenamesTheSession(t *testing.T) {
	s := &session{ID: "s", Root: leaf(&pane{
		ID:       "p",
		Worktree: &paneWorktree{Branch: "gyrocopter"},
	})}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{Projects: []*project{pr}}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Padding(12).Children(func() {
			w.sessionRow(c, tk, pr, s, 0)
			ui.Modal(c, &w.sidebarRenameOpen, func() { w.sidebarRenameDialog(c, tk) })
		})
	}
	tester := ui.NewTester(view, 320, 220)
	tester.Frame()
	row, ok := tester.Find("gyrocopter")
	if !ok {
		t.Fatal("the session row is not drawn")
	}
	tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
	tester.Frame()
	items := tester.Menu()
	hasRename, hasClose := false, false
	for _, item := range items {
		switch item {
		case locales.Rename():
			hasRename = true
		case locales.CloseSession():
			hasClose = true
		}
	}
	if !hasRename || !hasClose {
		t.Fatalf("the row menu = %q, want rename and close", items)
	}
	if err := tester.ChooseMenuItem(locales.Rename()); err != nil {
		t.Fatalf("the row has no rename item: %v", err)
	}
	tester.Frame()
	if !tester.Focused(locales.RenameSessionPlaceholder()) {
		t.Fatal("the rename prompt does not take the keyboard")
	}
	tester.Type("Fix login redirect")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if s.Label != "Fix login redirect" {
		t.Fatalf("label = %q", s.Label)
	}
	if w.sidebarRenameOpen {
		t.Fatal("the prompt stayed open after Return")
	}
	if _, ok := tester.Find("Fix login redirect"); !ok {
		t.Fatal("the row kept its old name")
	}
}

// A project row has the same menu: a folder can be named in the sidebar
// without touching its directory.
func TestProjectRowMenuRenamesTheProject(t *testing.T) {
	pr := &project{ID: "pr", Dir: "/tmp/example-project"}
	w := &workspace{Projects: []*project{pr}, collapsed: map[string]bool{}}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Padding(12).Children(func() {
			w.projectRow(c, tk, pr)
			ui.Modal(c, &w.sidebarRenameOpen, func() { w.sidebarRenameDialog(c, tk) })
		})
	}
	tester := ui.NewTester(view, 320, 220)
	tester.Frame()
	row, ok := tester.Find("example-project")
	if !ok {
		t.Fatal("the project row is not drawn")
	}
	tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
	tester.Frame()
	if err := tester.ChooseMenuItem(locales.Rename()); err != nil {
		t.Fatalf("the project has no rename item: %v", err)
	}
	tester.Frame()
	if !tester.Focused(locales.RenameProjectPlaceholder()) {
		t.Fatal("the rename prompt does not take the keyboard")
	}
	tester.Type("Example")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if pr.Label != "Example" || pr.Dir != "/tmp/example-project" {
		t.Fatalf("project label %q, dir %q", pr.Label, pr.Dir)
	}
	if _, ok := tester.Find("Example"); !ok {
		t.Fatal("the row kept its old name")
	}
}
