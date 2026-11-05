package main

import (
	"encoding/json"
	"strings"
	"time"
)

// A plan is what the agent says it is working through, not what its screen
// happens to show. Claude Code reports it as a tool call, so the hooks in
// events.go hand the call's input to `gexi plan`; the pane keeps the list and
// the header draws its progress.

const (
	planPending = "pending"
	planActive  = "active"
	planDone    = "done"
)

type planStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type panePlan struct {
	Steps   []planStep
	Source  string
	Updated time.Time
}

// progress is the step the plan is on, 1-based, and the total. The first
// active step wins, as Codex's (1/3); with none active it falls back to how
// many are done, so a finished plan reads (3/3) rather than (0/3).
func (p *panePlan) progress() (current, total int) {
	if p == nil || len(p.Steps) == 0 {
		return 0, 0
	}
	done := 0
	for i, step := range p.Steps {
		switch step.Status {
		case planDone:
			done++
		case planActive:
			return i + 1, len(p.Steps)
		}
	}
	return done, len(p.Steps)
}

func (p *panePlan) doneCount() int {
	done := 0
	if p == nil {
		return done
	}
	for _, step := range p.Steps {
		if step.Status == planDone {
			done++
		}
	}
	return done
}

func (p *panePlan) allDone() bool {
	if p == nil || len(p.Steps) == 0 {
		return false
	}
	return p.doneCount() == len(p.Steps)
}

func (p *panePlan) hasActive() bool {
	if p == nil {
		return false
	}
	for _, step := range p.Steps {
		if step.Status == planActive {
			return true
		}
	}
	return false
}

// normalizePlanStatus maps the status words agents use onto the three the
// header understands.
func normalizePlanStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "done", "success", "succeeded":
		return planDone
	case "in_progress", "in-progress", "active", "running", "current", "started":
		return planActive
	default:
		return planPending
	}
}

// planUpdate is one `plan.update` request. Detail is the hook payload as
// Claude Code pipes it in.
type planUpdate struct {
	Agent  string
	Pane   string
	Detail json.RawMessage
}

// handlePlan records a plan on its pane. Main thread.
func (w *workspace) handlePlan(up planUpdate) (any, error) {
	p, err := w.resolvePane(up.Pane)
	if err != nil {
		return nil, err
	}

	steps, ok := planStepsFromHook(up.Detail)
	if !ok {
		// Not a plan tool, or a subagent's own plan: leave the pane alone.
		return map[string]any{"pane": p.ID, "ignored": true}, nil
	}
	if len(steps) == 0 {
		p.plan = nil
		p.planOpen = false
	} else {
		p.plan = &panePlan{Steps: steps, Source: up.Agent, Updated: time.Now()}
	}
	w.invalidate()
	return map[string]any{"pane": p.ID, "steps": len(steps)}, nil
}

// planStepsFromHook reads a Claude Code PreToolUse payload. Both the CLI's
// TodoWrite and the desktop's update_plan carry the whole list each time, so
// the newest call is the plan; TaskCreate/TaskUpdate are incremental and
// deliberately ignored. ok is false when the payload is not a plan at all,
// which is different from a plan that is now empty.
func planStepsFromHook(raw json.RawMessage) (steps []planStep, ok bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var payload struct {
		AgentID   string          `json:"agent_id"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	// A subagent works on its own list; showing it would replace the main
	// agent's plan with whatever a helper happened to be doing.
	if payload.AgentID != "" {
		return nil, false
	}
	if len(payload.ToolInput) == 0 {
		return nil, false
	}

	var input struct {
		Todos []struct {
			Content    string `json:"content"`
			ActiveForm string `json:"activeForm"`
			Status     string `json:"status"`
		} `json:"todos"`
		Items []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"items"`
		Plan []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if json.Unmarshal(payload.ToolInput, &input) != nil {
		return nil, false
	}

	add := func(text, status string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		steps = append(steps, planStep{Text: text, Status: normalizePlanStatus(status)})
	}
	switch {
	case input.Todos != nil:
		for _, todo := range input.Todos {
			text := todo.Content
			if strings.TrimSpace(text) == "" {
				text = todo.ActiveForm
			}
			add(text, todo.Status)
		}
		return steps, true
	case input.Items != nil:
		for _, item := range input.Items {
			add(item.Content, item.Status)
		}
		return steps, true
	case input.Plan != nil:
		for _, item := range input.Plan {
			add(item.Step, item.Status)
		}
		return steps, true
	}
	return nil, false
}

func planJSON(p *panePlan) any {
	if p == nil || len(p.Steps) == 0 {
		return nil
	}
	current, total := p.progress()
	return map[string]any{
		"steps":   p.Steps,
		"source":  p.Source,
		"current": current,
		"total":   total,
		"done":    p.doneCount(),
		"updated": p.Updated.Format(time.RFC3339),
	}
}
