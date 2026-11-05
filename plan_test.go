package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPlanFromTodoWrite(t *testing.T) {
	raw := []byte(`{
		"hook_event_name": "PreToolUse",
		"tool_name": "TodoWrite",
		"tool_input": {"todos": [
			{"content": "Read the code", "status": "completed"},
			{"content": "Write the plan", "status": "in_progress"},
			{"content": "Ship it", "status": "pending"}
		]}
	}`)
	steps, ok := planStepsFromHook(raw)
	if !ok {
		t.Fatal("TodoWrite payload was not recognized")
	}
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	if steps[1].Status != planActive || steps[2].Status != planPending {
		t.Fatalf("statuses are %v", steps)
	}

	plan := &panePlan{Steps: steps}
	if current, total := plan.progress(); current != 2 || total != 3 {
		t.Fatalf("progress is %d/%d, want 2/3", current, total)
	}
	if plan.allDone() {
		t.Fatal("a plan with a pending step reads as done")
	}
}

func TestPlanFromUpdatePlan(t *testing.T) {
	raw := []byte(`{
		"tool_name": "mcp__ccd_turn__update_plan",
		"tool_input": {"items": [
			{"content": "One", "status": "completed"},
			{"content": "Two", "status": "completed"}
		]}
	}`)
	steps, ok := planStepsFromHook(raw)
	if !ok || len(steps) != 2 {
		t.Fatalf("got %d steps, ok=%v", len(steps), ok)
	}
	plan := &panePlan{Steps: steps}
	if current, total := plan.progress(); current != 2 || total != 2 {
		t.Fatalf("progress is %d/%d, want 2/2", current, total)
	}
	if !plan.allDone() {
		t.Fatal("a finished plan does not read as done")
	}
}

func TestPlanFromHookIgnoresOtherTools(t *testing.T) {
	if _, ok := planStepsFromHook([]byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)); ok {
		t.Fatal("Bash was read as a plan")
	}
	if _, ok := planStepsFromHook(nil); ok {
		t.Fatal("an empty payload was read as a plan")
	}
}

func TestPlanFromHookIgnoresSubagent(t *testing.T) {
	raw := []byte(`{
		"agent_id": "helper",
		"tool_name": "TodoWrite",
		"tool_input": {"todos": [{"content": "A subagent's list", "status": "pending"}]}
	}`)
	if _, ok := planStepsFromHook(raw); ok {
		t.Fatal("a subagent's plan replaced the pane's")
	}
}

func TestPlanFromHookEmptyClears(t *testing.T) {
	raw := []byte(`{"tool_name":"TodoWrite","tool_input":{"todos":[]}}`)
	steps, ok := planStepsFromHook(raw)
	if !ok {
		t.Fatal("an empty TodoWrite was not recognized")
	}
	if len(steps) != 0 {
		t.Fatalf("got %d steps, want none", len(steps))
	}
}

func TestPlanProgressWithoutActiveStep(t *testing.T) {
	plan := &panePlan{Steps: []planStep{
		{Text: "One", Status: planPending},
		{Text: "Two", Status: planPending},
	}}
	if current, total := plan.progress(); current != 0 || total != 2 {
		t.Fatalf("progress is %d/%d, want 0/2", current, total)
	}
}

func TestHandlePlanRecordsAndClears(t *testing.T) {
	p := &pane{ID: "pane-1"}
	s := &session{ID: "session-1", Root: leaf(p)}
	w := &workspace{
		Projects:        []*project{{ID: "project-1", Sessions: []*session{s}}},
		SelectedSession: s.ID,
		FocusedPane:     p.ID,
	}

	detail := []byte(`{"tool_name":"TodoWrite","tool_input":{"todos":[{"content":"One","status":"in_progress"}]}}`)
	if _, err := w.handlePlan(planUpdate{Pane: p.ID, Agent: "claude", Detail: detail}); err != nil {
		t.Fatalf("handlePlan: %v", err)
	}
	if p.plan == nil || len(p.plan.Steps) != 1 || p.plan.Source != "claude" {
		t.Fatalf("plan is %+v", p.plan)
	}

	// A tool that is not a plan leaves the last one alone.
	if _, err := w.handlePlan(planUpdate{Pane: p.ID, Detail: []byte(`{"tool_name":"Bash"}`)}); err != nil {
		t.Fatalf("handlePlan: %v", err)
	}
	if p.plan == nil {
		t.Fatal("a non-plan tool cleared the plan")
	}

	// An empty list is the agent saying the plan is gone.
	empty := []byte(`{"tool_name":"TodoWrite","tool_input":{"todos":[]}}`)
	if _, err := w.handlePlan(planUpdate{Pane: p.ID, Detail: empty}); err != nil {
		t.Fatalf("handlePlan: %v", err)
	}
	if p.plan != nil {
		t.Fatalf("an empty plan left %+v", p.plan)
	}
}

func TestPlanBadgeShowsProgress(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	p := &pane{ID: "pane-1", plan: &panePlan{Steps: []planStep{
		{Text: "One", Status: planDone},
		{Text: "Two", Status: planActive},
		{Text: "Three", Status: planPending},
	}}}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Row(c).Fill().Children(func() { w.planBadge(c, tk, p) })
	}, 300, 80)
	tester.Frame()
	if _, ok := tester.Find("2/3"); !ok {
		t.Fatal("the badge does not show 2/3")
	}
}

func TestPlanBadgeHoverPreviewsSteps(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	p := &pane{ID: "pane-1", plan: &panePlan{Steps: []planStep{
		{Text: "Read the code", Status: planDone},
		{Text: "Write the plan", Status: planActive},
		{Text: "Ship it", Status: planPending},
	}}}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Row(c).Fill().Children(func() { w.planBadge(c, tk, p) })
	}, 300, 140)
	tester.Frame()
	rect, ok := tester.Find("2/3")
	if !ok {
		t.Fatal("the badge is not in the view")
	}
	tester.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tester.Advance(700 * time.Millisecond)
	tester.Frame()
	if !tester.HasText("Ship it") {
		t.Fatal("hovering the badge did not preview the steps")
	}
}

func TestPlanPanelsRender(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	plan := &panePlan{Steps: []planStep{
		{Text: "Read the code", Status: planDone},
		{Text: "Write the plan and check it against the header", Status: planActive},
		{Text: "Ship it", Status: planPending},
	}}
	for _, pinned := range []bool{false, true} {
		pinned := pinned
		tester := ui.NewTester(func(c *ui.Context) {
			tk := themeTokens(c.Theme())
			w.planPanel(c, tk, plan, ui.Column(c), pinned)
		}, 360, 320)
		tester.Frame()
	}
}
