package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// moveProjectRows is the order moveSidebarProject leaves, as ids.
func moveProjectRows(w *workspace, rows []*project, moved []int, to int) []string {
	w.moveSidebarProject(rows, moved, to)
	ids := make([]string, len(w.Projects))
	for i, pr := range w.Projects {
		ids[i] = pr.ID
	}
	return ids
}

func TestMoveSidebarProjectPutsADragWhereItLanded(t *testing.T) {
	projects := func() (*workspace, []*project) {
		rows := []*project{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
		return &workspace{Projects: rows}, rows
	}
	cases := []struct {
		name  string
		moved []int
		to    int
		want  []string
	}{
		// Dragged up: the rows it passes move down.
		{"last to front", []int{3}, 0, []string{"d", "a", "b", "c"}},
		{"down one", []int{0}, 2, []string{"b", "a", "c", "d"}},
		// The end is the number of rows, and a drop there keeps the drag
		// at the bottom.
		{"first to end", []int{0}, 4, []string{"b", "c", "d", "a"}},
		{"drop where it was", []int{1}, 1, []string{"a", "b", "c", "d"}},
		{"drop after itself", []int{1}, 2, []string{"a", "b", "c", "d"}},
		// A drop out of the list changes nothing.
		{"past the end", []int{0}, 5, []string{"a", "b", "c", "d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, rows := projects()
			got := moveProjectRows(w, rows, c.moved, c.to)
			if len(got) != len(c.want) {
				t.Fatalf("projects = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("projects = %v, want %v", got, c.want)
				}
			}
		})
	}
}

// TestSidebarDragsAProjectToAnotherPlace: a project row dragged to the top
// of the sidebar lands there, and the layout keeps that order.
func TestSidebarDragsAProjectToAnotherPlace(t *testing.T) {
	mk := func(id, label string) *project {
		return &project{
			ID: id, Dir: t.TempDir(), Label: label,
			Sessions: []*session{{ID: id + "-s", Label: label, Root: leaf(&pane{ID: id + "-p"})}},
		}
	}
	a, b, c := mk("a", "alpha"), mk("b", "beta"), mk("c", "gamma")
	w := &workspace{
		settings: defaultSettings(),
		Projects: []*project{a, b, c},
		// The first session is the one on screen, so the canvas draws a
		// pane as it does behind a real sidebar.
		SelectedSession: "a-s",
		collapsed:       map[string]bool{},
		gitInfo:         map[string]gitSummary{},
		gitBusy:         map[string]bool{},
		gitGen:          map[string]int{},
	}
	tester := ui.NewTester(w.chromeView, 760, 900)
	tester.Frame()

	src, ok := tester.Find("gamma")
	if !ok {
		t.Fatalf("the gamma row is not there; texts = %q", tester.Texts())
	}
	dst, ok := tester.Find("alpha")
	if !ok {
		t.Fatalf("the alpha row is not there; texts = %q", tester.Texts())
	}
	// Let go above alpha's middle: gamma goes before it, at the top.
	x, y := dst.X+dst.W/2, dst.Y+2
	tester.Press(src.X+src.W/2, src.Y+src.H/2)
	tester.Move(x, y)
	tester.Release(x, y)
	tester.Frame()

	if len(w.Projects) != 3 {
		t.Fatalf("projects = %d, want the three it had", len(w.Projects))
	}
	got := []string{w.Projects[0].ID, w.Projects[1].ID, w.Projects[2].ID}
	if want := []string{"c", "a", "b"}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("projects = %v, want gamma, alpha, beta", got)
	}
	// The press started a drag, not a fold of the project it pressed.
	if w.collapsed["c"] {
		t.Fatal("the drag folded the project it dragged")
	}
}

// sessionWorkspace is two projects of sessions to drag around: the first has
// three, the second two.
func sessionWorkspace(t *testing.T) (*workspace, *project, *project) {
	t.Helper()
	mk := func(pr *project, id, label string) {
		pr.Sessions = append(pr.Sessions, &session{
			ID: id, Label: label, Root: leaf(&pane{ID: id + "-p"}),
		})
	}
	one := &project{ID: "one", Dir: t.TempDir(), Label: "first"}
	mk(one, "one-a", "alpha work")
	mk(one, "one-b", "beta work")
	mk(one, "one-c", "gamma work")
	two := &project{ID: "two", Dir: t.TempDir(), Label: "second"}
	mk(two, "two-a", "delta work")
	mk(two, "two-b", "epsilon work")
	w := &workspace{
		settings:        defaultSettings(),
		Projects:        []*project{one, two},
		SelectedSession: "one-a",
		collapsed:       map[string]bool{},
		gitInfo:         map[string]gitSummary{},
		gitBusy:         map[string]bool{},
		gitGen:          map[string]int{},
	}
	return w, one, two
}

// dragRow presses the row showing from and lets it go at (x, y).
func dragRow(t *testing.T, tester *ui.Tester, from string, x, y float32) {
	t.Helper()
	src, ok := tester.Find(from)
	if !ok {
		t.Fatalf("the %q row is not there; texts = %q", from, tester.Texts())
	}
	tester.Press(src.X+src.W/2, src.Y+src.H/2)
	tester.Move(x, y)
	tester.Release(x, y)
	tester.Frame()
}

func sameOrder(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sessionIDs(pr *project) []string {
	ids := make([]string, len(pr.Sessions))
	for i, s := range pr.Sessions {
		ids[i] = s.ID
	}
	return ids
}

// TestSidebarDragsASessionWithinItsProject: a session row dragged up its own
// project lands above the row it was let go on.
func TestSidebarDragsASessionWithinItsProject(t *testing.T) {
	w, one, _ := sessionWorkspace(t)
	tester := ui.NewTester(w.chromeView, 760, 900)
	tester.Frame()
	dst, _ := tester.Find("alpha work")
	dragRow(t, tester, "gamma work", dst.X+dst.W/2, dst.Y+2)
	if got := sessionIDs(one); !sameOrder(got, "one-c", "one-a", "one-b") {
		t.Fatalf("sessions = %v, want gamma, alpha, beta", got)
	}
}

// TestSidebarDragsASessionIntoAnotherProject: a session let go on another
// project's row leaves its own project and takes that place.
func TestSidebarDragsASessionIntoAnotherProject(t *testing.T) {
	w, one, two := sessionWorkspace(t)
	tester := ui.NewTester(w.chromeView, 760, 900)
	tester.Frame()
	dst, _ := tester.Find("epsilon work")
	// Above epsilon's middle: it goes between delta and epsilon.
	dragRow(t, tester, "gamma work", dst.X+dst.W/2, dst.Y+2)
	if got := sessionIDs(one); !sameOrder(got, "one-a", "one-b") {
		t.Fatalf("the first project = %v, want its two sessions left", got)
	}
	if got := sessionIDs(two); !sameOrder(got, "two-a", "one-c", "two-b") {
		t.Fatalf("the second project = %v, want delta, gamma, epsilon", got)
	}
}

// TestSidebarDragsASessionOntoAProjectHeader: a folded project takes a
// session by its header, as its first, and opens to show it.
func TestSidebarDragsASessionOntoAProjectHeader(t *testing.T) {
	w, one, two := sessionWorkspace(t)
	w.collapsed["two"] = true
	tester := ui.NewTester(w.chromeView, 760, 900)
	tester.Frame()
	dst, ok := tester.Find("second")
	if !ok {
		t.Fatalf("the second project's header is not there; texts = %q", tester.Texts())
	}
	dragRow(t, tester, "gamma work", dst.X+dst.W/2, dst.Y+dst.H/2)
	if got := sessionIDs(one); !sameOrder(got, "one-a", "one-b") {
		t.Fatalf("the first project = %v, want its two sessions left", got)
	}
	if got := sessionIDs(two); !sameOrder(got, "one-c", "two-a", "two-b") {
		t.Fatalf("the second project = %v, want gamma first", got)
	}
	if w.collapsed["two"] {
		t.Fatal("the project that took the session stayed folded")
	}
}

// TestMoveSessionKeepsItsPlaceWhenLetGoThere: a drop that lands a session
// where it already sits moves nothing, and a row that is gone takes it last.
func TestMoveSessionKeepsItsPlaceWhenLetGoThere(t *testing.T) {
	mk := func() (*workspace, *project, []*session) {
		pr := &project{ID: "p"}
		for _, id := range []string{"a", "b", "c"} {
			pr.Sessions = append(pr.Sessions, &session{ID: id})
		}
		return &workspace{}, pr, pr.Sessions
	}
	w, pr, s := mk()
	// b let go on a's lower half goes after a: it was there already.
	w.moveSession(pr, s[1], pr, s[0], sessionDropAfter)
	if got := sessionIDs(pr); !sameOrder(got, "a", "b", "c") {
		t.Fatalf("sessions = %v, want them unchanged", got)
	}
	// b let go on itself is no drop at all.
	w.moveSession(pr, s[1], pr, s[1], sessionDropBefore)
	if got := sessionIDs(pr); !sameOrder(got, "a", "b", "c") {
		t.Fatalf("sessions = %v, want them unchanged", got)
	}
	// A row that is no longer there takes it last, as a folded header does.
	w.moveSession(pr, s[0], pr, &session{ID: "gone"}, sessionDropBefore)
	if got := sessionIDs(pr); !sameOrder(got, "b", "c", "a") {
		t.Fatalf("sessions = %v, want the dragged one last", got)
	}
}

// TestAProjectAMoveEmptiedComesBack: a project whose last session was
// dragged into another keeps its row in the layout, and a project whose
// folder is gone does not.
func TestAProjectAMoveEmptiedComesBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEXI_HOME", home)

	// The save keeps a project with no sessions under it.
	empty := t.TempDir()
	w := &workspace{
		settings: defaultSettings(),
		Projects: []*project{{ID: "empty", Dir: empty, Label: "empty"}},
	}
	saved := w.savedState()
	if len(saved.Projects) != 1 || saved.Projects[0].ID != "empty" {
		t.Fatalf("saved projects = %+v, want the one without sessions", saved.Projects)
	}

	// The launch brings it back, and leaves a project whose folder is gone.
	state := workspaceState{Projects: []savedProject{
		{ID: "empty", Dir: empty, Label: "empty"},
		{ID: "gone", Dir: filepath.Join(t.TempDir(), "deleted"), Label: "gone"},
	}}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "workspace.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	w = newWorkspace()
	w.restoreOrCreate()
	if len(w.Projects) != 1 || w.Projects[0].ID != "empty" {
		t.Fatalf("restored projects = %d, want the empty one alone", len(w.Projects))
	}
	if len(w.Projects[0].Sessions) != 0 {
		t.Fatalf("the empty project has %d sessions", len(w.Projects[0].Sessions))
	}
}
