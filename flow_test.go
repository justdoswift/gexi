package main

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A project's flows are the prompt at each boundary: an empty field is not a
// flow, and the prompt that is kept is trimmed.
func TestSubmitProjectFlowsKeepsOnlyTypedPrompts(t *testing.T) {
	pr := &project{ID: "pr", Dir: "/tmp/example"}
	w := &workspace{Projects: []*project{pr}}
	w.flowTarget = pr
	w.flowOpen = true
	w.flowDraft = flowDraft{
		afterWorktree: "  ",
		afterPR:       "  发布  ",
		afterMerge:    "",
	}

	w.submitProjectFlows()

	if len(pr.Flows) != 1 || pr.Flows[string(flowAfterPR)] != "发布" {
		t.Fatalf("flows = %+v, want only the PR prompt", pr.Flows)
	}
	if w.flowOpen || w.flowTarget != nil {
		t.Fatalf("dialog stayed open: open=%v target=%v", w.flowOpen, w.flowTarget)
	}
}

// flowPrompt reads the prompt for one boundary only: a prompt after the PR
// must not run after a merge.
func TestProjectFlowPromptIsPerStage(t *testing.T) {
	pr := &project{Flows: projectFlows{
		string(flowAfterWorktree): "检查改动",
		string(flowAfterMerge):    " 发布 ",
	}}
	if got := pr.flowPrompt(flowAfterWorktree); got != "检查改动" {
		t.Fatalf("worktree prompt = %q", got)
	}
	if got := pr.flowPrompt(flowAfterMerge); got != "发布" {
		t.Fatalf("merge prompt = %q", got)
	}
	if got := pr.flowPrompt(flowAfterPR); got != "" {
		t.Fatalf("PR prompt = %q, want none", got)
	}
}

// queueFlow finds the project through the pane's session, and queues the
// prompt only when that boundary has one. The merge's archive choice travels
// with the task, so the checkout can wait for the flow.
func TestQueueFlowUsesTheProjectsPrompt(t *testing.T) {
	p := &pane{ID: "pane"}
	s := &session{ID: "s", Root: leaf(p)}
	pr := &project{
		ID: "pr", Dir: "/tmp/example", Sessions: []*session{s},
		Flows: projectFlows{string(flowAfterMerge): "发布"},
	}
	w := &workspace{Projects: []*project{pr}}

	if !w.queueFlow(p, flowAfterMerge, true) {
		t.Fatal("the merge flow was not queued")
	}
	if len(w.flows) != 1 {
		t.Fatalf("queued %d flows, want 1", len(w.flows))
	}
	if task := w.flows[0]; task.paneID != "pane" || task.prompt != "发布" || !task.archiveAfter {
		t.Fatalf("task = %+v", task)
	}
	if w.queueFlow(p, flowAfterPR, false) {
		t.Fatal("a boundary without a prompt was queued")
	}
}

// A flow's prompt is typed by gexi, so the agent's hook must not take it for
// the user's first message and name the pane after it. The user's own first
// message still does.
func TestFlowPromptDoesNotNameThePane(t *testing.T) {
	p := &pane{injectedPrompt: "发布"}
	if p.noteFirstPrompt("发布") {
		t.Fatal("the flow prompt became the pane's first message")
	}
	if p.firstPrompt != "" || p.injectedPrompt != "" {
		t.Fatalf("firstPrompt = %q, injectedPrompt = %q", p.firstPrompt, p.injectedPrompt)
	}
	if !p.noteFirstPrompt("修登录跳转") {
		t.Fatal("the user's first message was ignored")
	}
	if p.firstPrompt != "修登录跳转" {
		t.Fatalf("firstPrompt = %q", p.firstPrompt)
	}
}

// typedConn keeps what a pane types into its program, the way the pty under
// an agent sees it.
type typedConn struct {
	mu  sync.Mutex
	buf bytes.Buffer
	r   *io.PipeReader
}

func (c *typedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *typedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}
func (c *typedConn) Close() error { return c.r.Close() }
func (c *typedConn) typed() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// flowPane is a pane with an agent already recognized, whose typed input can
// be read back.
func flowPane(t *testing.T) (*pane, *typedConn) {
	t.Helper()
	r, _ := io.Pipe()
	conn := &typedConn{r: r}
	term, err := terminal.New(terminal.Options{
		Conn:         conn,
		ResizeByConn: true,
		Font: terminal.Font{
			Family: "JetBrains Mono Nerd Font Mono, monospace",
			Size:   12,
			Weight: 400,
		},
	})
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	return &pane{ID: "pane", term: term, agent: &agentDetection{}}, conn
}

// flowProject is a project with one session holding p and the given flow.
func flowProject(p *pane, stage flowStage, prompt string) *project {
	return &project{
		ID: "pr", Dir: "/tmp/example",
		Sessions: []*session{{ID: "s", Root: leaf(p)}},
		Flows:    projectFlows{string(stage): prompt},
	}
}

// waitForTyped waits for what the pane typed to land in its program, which is
// written from the terminal's own goroutine.
func waitForTyped(t *testing.T, conn *typedConn, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && conn.typed() != want {
		time.Sleep(10 * time.Millisecond)
	}
	if got := conn.typed(); got != want {
		t.Fatalf("typed = %q, want %q", got, want)
	}
}

// The prompts live in workspace.json with the rest of the layout, so a launch
// brings the project's workflow back.
func TestProjectFlowsSurviveALaunch(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	w := &workspace{settings: defaultSettings()}
	pr := &project{
		ID: "pr", Dir: "/tmp/example",
		Sessions: []*session{{ID: "s", Root: leaf(&pane{ID: "pane"})}},
		Flows:    projectFlows{string(flowAfterWorktree): "检查改动", string(flowAfterMerge): "发布"},
	}
	w.Projects = []*project{pr}
	w.saveNow()

	state, ok := loadState()
	if !ok {
		t.Fatal("the layout did not load")
	}
	if len(state.Projects) != 1 {
		t.Fatalf("projects = %+v", state.Projects)
	}
	got := state.Projects[0].Flows
	if got[string(flowAfterWorktree)] != "检查改动" || got[string(flowAfterMerge)] != "发布" {
		t.Fatalf("flows after a launch = %+v", got)
	}
}

// A flow is typed into the agent, so it waits for the agent to be idle and
// then goes in once, with the return that sends it.
func TestDrainFlowsTypesThePromptWhenTheAgentIsIdle(t *testing.T) {
	p, conn := flowPane(t)
	w := &workspace{Projects: []*project{flowProject(p, flowAfterPR, "发布")}}

	if !w.queueFlow(p, flowAfterPR, false) {
		t.Fatal("the flow was not queued")
	}

	// A prompt typed mid-turn would land in someone else's work.
	p.outAt = time.Now()
	p.activity = &agentActivity{State: activityWorking}
	w.drainFlows()
	if got := conn.typed(); got != "" {
		t.Fatalf("typed while the agent was working: %q", got)
	}
	if len(w.flows) != 1 {
		t.Fatalf("flows = %d, want the prompt still queued", len(w.flows))
	}

	p.outAt = time.Now().Add(-2 * activityQuiet)
	p.activity = nil
	w.drainFlows()

	waitForTyped(t, conn, "发布\r")
	if p.injectedPrompt != "发布" {
		t.Fatalf("injectedPrompt = %q", p.injectedPrompt)
	}
	if len(w.flows) != 0 {
		t.Fatalf("flows = %d, want the task gone once it is typed", len(w.flows))
	}
}

// A merged checkout set to archive after a merge waits for the flow's turn:
// the task stays until the agent that took the prompt has stopped working.
func TestMergedCheckoutWaitsForTheFlowsTurn(t *testing.T) {
	p, conn := flowPane(t)
	w := &workspace{Projects: []*project{flowProject(p, flowAfterMerge, "发布")}}

	if !w.queueFlow(p, flowAfterMerge, true) {
		t.Fatal("the merge flow was not queued")
	}
	w.drainFlows()
	waitForTyped(t, conn, "发布\r")
	if len(w.flows) != 1 {
		t.Fatalf("flows = %d, want the task kept for its turn", len(w.flows))
	}

	// The turn is running: the task stays and records that it started, so
	// the checkout is not archived under a working agent.
	p.outAt = time.Now()
	p.activity = &agentActivity{State: activityWorking}
	w.drainFlows()
	if len(w.flows) != 1 || !w.flows[0].started {
		t.Fatalf("flows = %+v, want the turn recorded and the task kept", w.flows)
	}
}

// The project row's menu is where the workflow is set: hover the row, choose
// Flows, pick a step, type its note, and save. What is saved is only the step
// that was typed into.
func TestProjectFlowsDialogSavesFromTheRowMenu(t *testing.T) {
	pr := &project{
		ID: "pr", Dir: t.TempDir(), Label: "gexi-flow-test",
		Sessions: []*session{{ID: "s", Root: leaf(&pane{ID: "p", Label: "zsh"})}},
	}
	w := &workspace{Projects: []*project{pr}, collapsed: map[string]bool{}}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			ui.Column(c).Width(240).FillHeight().Padding(8).Children(func() {
				w.projectRow(c, tk, pr)
			})
			w.projectFlowDialog(c, tk)
		})
	}, 700, 520)
	tester.Frame()

	row, ok := tester.Find("gexi-flow-test")
	if !ok {
		t.Fatal("the project row is not drawn")
	}
	tester.Move(row.X+row.W/2, row.Y+row.H/2)
	tester.Frame()
	more, ok := tester.Access(locales.More())
	if !ok {
		t.Fatal("the row's menu button is not drawn on hover")
	}
	tester.ClickAt(more.Bounds.X+more.Bounds.W/2, more.Bounds.Y+more.Bounds.H/2)
	tester.Frame()
	if err := tester.ChooseMenuItem(locales.Flows()); err != nil {
		t.Fatalf("the row menu has no flows item: %v", err)
	}
	tester.Frame()
	if !w.flowOpen {
		t.Fatalf("the dialog did not open; texts = %q", tester.Texts())
	}
	if !tester.HasText(locales.FlowStageWorktree()) || !tester.HasText(locales.FlowStageMerge()) {
		t.Fatalf("the dialog does not list its steps; texts = %q", tester.Texts())
	}

	merge, ok := tester.Find(locales.FlowStageMerge())
	if !ok {
		t.Fatal("the merge step is not drawn")
	}
	tester.ClickAt(merge.X+merge.W/2, merge.Y+merge.H/2)
	tester.Frame()
	tester.Type("发布")
	tester.Frame()
	if err := tester.Click(locales.Save()); err != nil {
		t.Fatalf("save: %v", err)
	}
	tester.Frame()

	if w.flowOpen {
		t.Fatal("the dialog stayed open after saving")
	}
	if got := pr.flowPrompt(flowAfterMerge); got != "发布" {
		t.Fatalf("the saved merge flow = %q, want 发布", got)
	}
	if got := pr.flowPrompt(flowAfterWorktree); got != "" {
		t.Fatalf("an untouched step was saved as %q", got)
	}
}

// The pointer leaves the project row to reach the menu's items, so the "…"
// that opened the menu has to stay behind them: while it was drawn only on
// hover, choosing Flows closed the menu and opened nothing.
func TestProjectFlowsOpenAfterThePointerLeavesTheRow(t *testing.T) {
	pr := &project{ID: "pr", Dir: t.TempDir(), Label: "gexi-flow-test"}
	w := &workspace{Projects: []*project{pr}, collapsed: map[string]bool{}, settings: defaultSettings()}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			ui.Column(c).Width(240).FillHeight().Padding(8).Children(func() {
				w.projectRow(c, tk, pr)
			})
			w.projectFlowDialog(c, tk)
		})
	}, 700, 520)
	tester.Frame()

	row, ok := tester.Find("gexi-flow-test")
	if !ok {
		t.Fatal("the project row is not drawn")
	}
	tester.Move(row.X+row.W/2, row.Y+row.H/2)
	tester.Frame()
	more, ok := tester.Access(locales.More())
	if !ok {
		t.Fatal("the row's menu button is not drawn on hover")
	}
	tester.ClickAt(more.Bounds.X+more.Bounds.W/2, more.Bounds.Y+more.Bounds.H/2)
	tester.Frame()
	// The pointer moves off the row to choose an item.
	tester.Move(690, 510)
	tester.Frame()
	if err := tester.ChooseMenuItem(locales.Flows()); err != nil {
		t.Fatalf("the row menu has no flows item: %v", err)
	}
	tester.Frame()
	if !w.flowOpen {
		t.Fatalf("the dialog did not open; texts = %q", tester.Texts())
	}
}
