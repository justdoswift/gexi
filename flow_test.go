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

// A project's steps are what the dialog keeps: an empty one is dropped, and
// the rest keep their order and the anchor they run at.
func TestSubmitProjectFlowsKeepsOnlyTypedSteps(t *testing.T) {
	pr := &project{ID: "pr", Dir: "/tmp/example"}
	w := &workspace{Projects: []*project{pr}}
	w.flowTarget = pr
	w.flowOpen = true
	w.flowDraft = flowDraft{steps: []*flowStep{
		{At: string(flowAfterPR), Prompt: "  发布  "},
		{At: string(flowAfterMerge), Prompt: "   "},
		{At: string(flowAfterMerge), Prompt: "通知团队"},
	}}

	w.submitProjectFlows()

	if len(pr.Steps) != 2 {
		t.Fatalf("steps = %+v, want the two typed ones", pr.Steps)
	}
	if pr.Steps[0].Prompt != "发布" || pr.Steps[0].At != string(flowAfterPR) {
		t.Fatalf("first step = %+v", pr.Steps[0])
	}
	if pr.Steps[1].Prompt != "通知团队" || pr.Steps[1].At != string(flowAfterMerge) {
		t.Fatalf("second step = %+v", pr.Steps[1])
	}
	if pr.Flows != nil {
		t.Fatalf("the old per-stage map was kept: %+v", pr.Flows)
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
	if task := w.flows[0]; task.paneID != "pane" || task.prompt() != "发布" || !task.archiveAfter {
		t.Fatalf("task = %+v", task)
	}
	if w.queueFlow(p, flowAfterPR, false) {
		t.Fatal("a boundary without a prompt was queued")
	}
}

// A project runs its stage's own prompt first, then the steps the user put
// at that boundary, in the order they are listed. Steps at another boundary
// and steps left empty do not run there.
func TestFlowPromptsRunTheStagesOwnPromptFirst(t *testing.T) {
	pr := &project{
		Flows: projectFlows{string(flowAfterMerge): "发布"},
		Steps: []flowStep{
			{At: string(flowAfterWorktree), Prompt: "跑测试"},
			{At: string(flowAfterMerge), Prompt: "写更新日志"},
			{At: string(flowAfterMerge), Prompt: "   "},
			{At: string(flowAfterMerge), Prompt: " 通知团队 "},
		},
	}
	got := pr.flowPrompts(flowAfterMerge)
	want := []string{"发布", "写更新日志", "通知团队"}
	if len(got) != len(want) {
		t.Fatalf("merge prompts = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merge prompts = %q, want %q", got, want)
		}
	}
	if got := pr.flowPrompts(flowAfterWorktree); len(got) != 1 || got[0] != "跑测试" {
		t.Fatalf("worktree prompts = %q, want the step alone", got)
	}
	if got := pr.flowPrompts(flowAfterPR); len(got) != 0 {
		t.Fatalf("PR prompts = %q, want none", got)
	}
}

// The queue carries every prompt of a boundary, so the flow types the stage's
// own prompt and then the steps added under it.
func TestQueueFlowCarriesTheStepsInOrder(t *testing.T) {
	p := &pane{ID: "pane"}
	pr := flowProject(p, flowAfterMerge, "发布")
	pr.Steps = []flowStep{
		{At: string(flowAfterMerge), Prompt: "写更新日志"},
		{At: string(flowAfterMerge), Prompt: "通知团队"},
	}
	w := &workspace{Projects: []*project{pr}}
	if !w.queueFlow(p, flowAfterMerge, false) {
		t.Fatal("the flow was not queued")
	}
	got := w.flows[0].prompts
	want := []string{"发布", "写更新日志", "通知团队"}
	if len(got) != len(want) {
		t.Fatalf("queued prompts = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queued prompts = %q, want %q", got, want)
		}
	}
}

// A step's handle moves it: onto another stage it joins the end of that
// boundary, and onto a step it lands just before it, so the list stays the
// order the prompts run in.
func TestMoveFlowStepKeepsTheListInRunningOrder(t *testing.T) {
	w := &workspace{}
	w.flowDraft.steps = []*flowStep{
		{At: string(flowAfterMerge), Prompt: "一"},
		{At: string(flowAfterMerge), Prompt: "二"},
		{At: string(flowAfterMerge), Prompt: "三"},
	}
	w.moveFlowStep(2, flowAfterWorktree, nil)
	if got := w.flowDraft.steps[0]; got.Prompt != "三" || got.At != string(flowAfterWorktree) {
		t.Fatalf("first step = %+v, want 三 at the worktree boundary", got)
	}
	if len(w.flowDraft.steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(w.flowDraft.steps))
	}
	// Dropped on 一, it lands before it rather than after the boundary's
	// own prompt only: the list is what runs.
	first := w.flowDraft.steps[1]
	w.moveFlowStep(0, flowAfterMerge, first)
	got := []string{}
	for _, step := range w.flowDraft.steps {
		got = append(got, step.At+":"+step.Prompt)
	}
	want := []string{"merge:三", "merge:一", "merge:二"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps = %q, want %q", got, want)
		}
	}
}

// A step's handle drags it to another boundary: let go on a stage's card, it
// joins the steps under that stage, which is where it will run.
func TestFlowDialogDragsAStepToAnotherBoundary(t *testing.T) {
	pr := &project{
		ID: "pr", Dir: t.TempDir(), Label: "gexi-flow-test",
		Sessions: []*session{{ID: "s", Root: leaf(&pane{ID: "p", Label: "zsh"})}},
		Steps:    []flowStep{{At: string(flowAfterMerge), Prompt: "通知团队"}},
	}
	w := &workspace{Projects: []*project{pr}, collapsed: map[string]bool{}}
	w.openProjectFlows(pr)
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			w.projectFlowDialog(c, tk)
		})
	}, 700, 620)
	tester.Frame()

	handle, ok := tester.Find("⋮⋮")
	if !ok {
		t.Fatalf("the step has no handle; texts = %q", tester.Texts())
	}
	target, ok := tester.Find(locales.FlowStageWorktree())
	if !ok {
		t.Fatal("the worktree stage is not drawn")
	}
	tester.Press(handle.X+handle.W/2, handle.Y+handle.H/2)
	tester.Move(target.X+target.W/2, target.Y+target.H/2)
	tester.Release(target.X+target.W/2, target.Y+target.H/2)
	tester.Frame()

	if len(w.flowDraft.steps) != 1 {
		t.Fatalf("steps = %d, want the dragged one", len(w.flowDraft.steps))
	}
	if got := w.flowDraft.steps[0].At; got != string(flowAfterWorktree) {
		t.Fatalf("the step sits at %q, want the worktree boundary", got)
	}
}

// The dialog's list is the pipeline: adding a step puts a card under the last
// boundary, and saving keeps it with the boundary it sits at.
func TestFlowDialogAddsAStepAndSavesIt(t *testing.T) {
	pr := &project{
		ID: "pr", Dir: t.TempDir(), Label: "gexi-flow-test",
		Sessions: []*session{{ID: "s", Root: leaf(&pane{ID: "p", Label: "zsh"})}},
	}
	w := &workspace{Projects: []*project{pr}, collapsed: map[string]bool{}}
	w.openProjectFlows(pr)
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Column(c).Fill().Background(tk.Background).Children(func() {
			w.projectFlowDialog(c, tk)
		})
	}, 700, 620)
	tester.Frame()
	if err := tester.Click(locales.FlowAddStep()); err != nil {
		t.Fatalf("add: %v", err)
	}
	tester.Frame()
	if len(w.flowDraft.steps) != 1 {
		t.Fatalf("steps = %d, want the added one", len(w.flowDraft.steps))
	}
	if got := w.flowDraft.steps[0].At; got != string(flowAfterMerge) {
		t.Fatalf("the step was added at %q, want the last boundary", got)
	}
	r, ok := tester.Find(locales.FlowStepPlaceholder())
	if !ok {
		t.Fatalf("the new card has no prompt field; texts = %q", tester.Texts())
	}
	tester.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tester.Frame()
	tester.Type("通知团队")
	tester.Frame()
	if err := tester.Click(locales.Save()); err != nil {
		t.Fatalf("save: %v", err)
	}
	tester.Frame()
	if len(pr.Steps) != 1 || pr.Steps[0].Prompt != "通知团队" ||
		pr.Steps[0].At != string(flowAfterMerge) {
		t.Fatalf("saved steps = %+v", pr.Steps)
	}
	if got := pr.flowPrompts(flowAfterMerge); len(got) != 1 || got[0] != "通知团队" {
		t.Fatalf("merge prompts = %q", got)
	}
}

// A flow with several prompts types one, waits for its turn to end, then
// types the next, so the steps read as messages rather than landing together.
func TestDrainFlowsTypesTheStepsOneTurnAtATime(t *testing.T) {
	p, conn := flowPane(t)
	pr := flowProject(p, flowAfterMerge, "发布")
	pr.Steps = []flowStep{{At: string(flowAfterMerge), Prompt: "通知团队"}}
	w := &workspace{Projects: []*project{pr}}
	if !w.queueFlow(p, flowAfterMerge, false) {
		t.Fatal("the flow was not queued")
	}
	w.drainFlows()
	waitForTyped(t, conn, "发布\r")

	// The first turn is still running: the second prompt waits for it.
	p.outAt = time.Now()
	p.activity = &agentActivity{State: activityWorking}
	w.drainFlows()
	if got := conn.typed(); got != "发布\r" {
		t.Fatalf("typed = %q, want only the first prompt", got)
	}

	// The turn ended: the task moves to its next prompt, which goes in on
	// the tick after, once the agent is idle.
	p.outAt = time.Now().Add(-2 * activityQuiet)
	p.activity = nil
	w.drainFlows()
	w.drainFlows()
	waitForTyped(t, conn, "发布\r通知团队\r")
	if len(w.flows) != 0 {
		t.Fatalf("flows = %d, want the task gone once the last prompt is typed", len(w.flows))
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

	// The fixed steps are anchors: a prompt is a card the user adds, and it
	// runs at the last anchor unless it is dragged elsewhere.
	if err := tester.Click(locales.FlowAddStep()); err != nil {
		t.Fatalf("add: %v", err)
	}
	tester.Frame()
	field, ok := tester.Find(locales.FlowStepPlaceholder())
	if !ok {
		t.Fatalf("the added card has no prompt field; texts = %q", tester.Texts())
	}
	tester.ClickAt(field.X+field.W/2, field.Y+field.H/2)
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
	if got := pr.flowPrompts(flowAfterMerge); len(got) != 1 || got[0] != "发布" {
		t.Fatalf("the saved merge prompts = %q, want 发布", got)
	}
	if got := pr.flowPrompts(flowAfterWorktree); len(got) != 0 {
		t.Fatalf("an untouched anchor got a prompt: %q", got)
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
