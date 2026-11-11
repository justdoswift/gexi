package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// A pane's name, typed by hand or made from its first message, outranks the
// agent and the terminal title, and the sidebar row under it shows it too.
func TestPaneLabelNamesThePaneAndItsSession(t *testing.T) {
	p := &pane{ID: "p", Label: "20261111|需求|登录页扫码", agent: &agentDetection{Kind: agentClaude}}
	if got := p.title(); got != "20261111|需求|登录页扫码" {
		t.Fatalf("title = %q, want the pane's own name", got)
	}
	s := &session{ID: "s", Root: leaf(p)}
	if got := sessionLabel(s); got != "20261111|需求|登录页扫码" {
		t.Fatalf("session label = %q, want the pane's name", got)
	}
	// A name given to the session itself still wins over the pane's.
	s.Label = "Login"
	if got := sessionLabel(s); got != "Login" {
		t.Fatalf("session label = %q, want the manual session name", got)
	}
}

// The generated name is the day, the kind of work, and its subject, however
// the agent wraps what it wrote.
func TestPaneNameFromAnswer(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)
	cases := []struct{ answer, want string }{
		{"需求|登录页扫码登录", "20261111|需求|登录页扫码登录"},
		{"```\n问题｜登录后跳回首页\n```", "20261111|问题|登录后跳回首页"},
		{"类型：测试|补充导出用例", "20261111|测试|补充导出用例"},
		{"\"需求|登录支持记住账号\"", "20261111|需求|登录支持记住账号"},
		// An answer that already carries the day keeps one date, not two.
		{"20261111|需求|登录支持记住账号", "20261111|需求|登录支持记住账号"},
		// A bullet or bold makes no difference to the name.
		{"- **测试|补登录回归用例**", "20261111|测试|补登录回归用例"},
		// No kind: the words themselves say what kind of work it is.
		{"修复导出按钮点了没反应", "20261111|问题|修复导出按钮点了没反应"},
		{"把设置页拆成几个组件", "20261111|需求|把设置页拆成几个组件"},
		// Nothing to name.
		{"", ""},
		{"```\n```", ""},
		{"需求|", ""},
	}
	for _, c := range cases {
		if got := paneNameFromAnswer(c.answer, at); got != c.want {
			t.Errorf("paneNameFromAnswer(%q) = %q; want %q", c.answer, got, c.want)
		}
	}
}

// A generated name is long enough for a sentence but not for a paragraph.
func TestPaneNameFromAnswerKeepsOneShortLine(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)
	long := "需求|" + strings.Repeat("很长的业务名称", 8) + "。\n这是第二行"
	got := paneNameFromAnswer(long, at)
	parts := strings.Split(got, "|")
	if len(parts) != 3 {
		t.Fatalf("name = %q, want three parts", got)
	}
	if parts[0] != "20261111" || parts[1] != "需求" {
		t.Fatalf("name = %q", got)
	}
	if n := utf8.RuneCountInString(parts[2]); n > 24 {
		t.Fatalf("subject has %d runes, want at most 24: %q", n, parts[2])
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "。") {
		t.Fatalf("name = %q, want one line without punctuation", got)
	}
}

// A pane's name is part of the layout, so it comes back after a launch.
func TestPaneLabelIsSaved(t *testing.T) {
	p := &pane{ID: "p", Label: "20261111|需求|登录页扫码"}
	s := &session{ID: "s", Root: leaf(p)}
	w := &workspace{Projects: []*project{{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}}}

	state := w.savedState()
	if len(state.Projects) != 1 || len(state.Projects[0].Sessions) != 1 {
		t.Fatalf("saved state: %+v", state)
	}
	saved := *state.Projects[0].Sessions[0].Root.Pane
	if saved.Label != "20261111|需求|登录页扫码" {
		t.Fatalf("saved label = %q", saved.Label)
	}
}

// The pane header's right-click menu renames the pane; a name set by hand
// then stays, whatever the first message would have made of it.
func TestPaneHeaderMenuRenamesThePane(t *testing.T) {
	p := &pane{ID: "p", firstPrompt: "修复登录失败", agent: &agentDetection{Kind: agentClaude}}
	s := &session{ID: "s", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{Projects: []*project{pr}}
	view := func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Padding(12).Children(func() {
			title := ui.Text(c, "Claude Code")
			title.ContextMenu(func(m *ui.Menu) { w.paneHeaderMenu(m, p) })
			ui.Modal(c, &w.paneRenameOpen, func() { w.paneRenameDialog(c, tk) })
		})
	}
	tester := ui.NewTester(view, 420, 260)
	tester.Frame()
	title, ok := tester.Find("Claude Code")
	if !ok {
		t.Fatal("the pane title is not drawn")
	}
	tester.RightClickAt(title.X+title.W/2, title.Y+title.H/2)
	tester.Frame()
	items := tester.Menu()
	hasRename, hasGenerate := false, false
	for _, item := range items {
		switch item {
		case locales.RenamePane():
			hasRename = true
		case locales.PaneNameFromFirstMessage():
			hasGenerate = true
		}
	}
	if !hasRename || !hasGenerate {
		t.Fatalf("the pane menu = %q, want rename and name from first message", items)
	}
	if err := tester.ChooseMenuItem(locales.RenamePane()); err != nil {
		t.Fatalf("the pane has no rename item: %v", err)
	}
	tester.Frame()
	if !tester.Focused(locales.RenamePanePlaceholder()) {
		t.Fatal("the rename prompt does not take the keyboard")
	}
	tester.Type("20261111|需求|登录页扫码")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if p.Label != "20261111|需求|登录页扫码" {
		t.Fatalf("label = %q", p.Label)
	}
	if !p.nameAsked {
		t.Fatal("a name set by hand must keep the first message from naming it")
	}
	if w.paneRenameOpen {
		t.Fatal("the prompt stayed open after Return")
	}
}

// An empty name is not a name: the prompt says so and stays open.
func TestPaneRenameRefusesAnEmptyName(t *testing.T) {
	p := &pane{ID: "p"}
	s := &session{ID: "s", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{Projects: []*project{pr}, paneRenameTarget: p, paneRenameText: "  ", paneRenameOpen: true}
	w.submitPaneRename()
	if !w.paneRenameOpen || w.paneRenameErr == "" {
		t.Fatalf("open = %v, err = %q, want the prompt to ask again", w.paneRenameOpen, w.paneRenameErr)
	}
	if p.Label != "" {
		t.Fatalf("label = %q, want none", p.Label)
	}
}

// A started turn keeps the first message the agent reported, so a name can
// be made from it; later turns do not replace it.
func TestTurnStartKeepsTheFirstPrompt(t *testing.T) {
	p := &pane{ID: "p"}
	s := &session{ID: "s", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{Projects: []*project{pr}}
	if _, err := w.handleEvent(agentEvent{Kind: "turn.start", Pane: "p", Agent: "claude", Prompt: "修复登录失败"}); err != nil {
		t.Fatal(err)
	}
	if p.firstPrompt != "修复登录失败" {
		t.Fatalf("first prompt = %q", p.firstPrompt)
	}
	if _, err := w.handleEvent(agentEvent{Kind: "turn.start", Pane: "p", Agent: "claude", Prompt: "再补一条测试"}); err != nil {
		t.Fatal(err)
	}
	if p.firstPrompt != "修复登录失败" {
		t.Fatalf("first prompt = %q, want the first one kept", p.firstPrompt)
	}
}
