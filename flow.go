package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A project's flows are prompts it runs at the steps of the worktree
// pipeline: after gexi makes a checkout, after it opens the pull request, and
// after it merges one. Each prompt is typed into the agent of the pane that
// did the work, so "发布" after a merge is one more step of the same
// conversation rather than a separate job.
//
// A flow waits for its turn before it is typed: right after a worktree is
// made the pane's agent is only starting, and a prompt sent mid-turn would
// land in the middle of other work. A merged checkout that archives itself
// waits for the flow's turn to end before the checkout goes.

type flowStage string

const (
	flowAfterWorktree flowStage = "worktree"
	flowAfterPR       flowStage = "pr"
	flowAfterMerge    flowStage = "merge"
)

// flowStages is the order the dialog shows, the step each boundary names,
// and the example its note offers.
var flowStages = []struct {
	stage       flowStage
	title       func() string
	placeholder func() string
}{
	{flowAfterWorktree, locales.FlowStageWorktree, locales.FlowWorktreePlaceholder},
	{flowAfterPR, locales.FlowStagePR, locales.FlowPRPlaceholder},
	{flowAfterMerge, locales.FlowStageMerge, locales.FlowMergePlaceholder},
}

// projectFlows is what a project runs at each stage, keyed by flowStage. An
// empty or missing entry means that stage has no flow.
type projectFlows map[string]string

// flowStep is a step the user added to a project's pipeline. It runs at the
// boundary At names, after that boundary's own prompt and before the next
// boundary's; steps at one boundary run in the order they are listed.
type flowStep struct {
	At     string `json:"at"`
	Prompt string `json:"prompt"`
}

// flowPrompt is the prompt the project runs at stage, trimmed, or "" when it
// has no flow there.
func (p *project) flowPrompt(stage flowStage) string {
	if p == nil || p.Flows == nil {
		return ""
	}
	return strings.TrimSpace(p.Flows[string(stage)])
}

// flowPrompts is everything that runs at stage, in order: the stage's own
// prompt, then the steps placed at it.
func (p *project) flowPrompts(stage flowStage) []string {
	if p == nil {
		return nil
	}
	var out []string
	if own := p.flowPrompt(stage); own != "" {
		out = append(out, own)
	}
	for _, step := range p.Steps {
		if step.At != string(stage) {
			continue
		}
		if prompt := strings.TrimSpace(step.Prompt); prompt != "" {
			out = append(out, prompt)
		}
	}
	return out
}

// flowDraft is the dialog's copy of a project's pipeline: the steps the user
// added, in the order they run. The steps point into the draft, so an input
// keeps its value as the list grows.
type flowDraft struct {
	steps []*flowStep
}

// counts is how many prompts are set, and how many the draft holds.
func (d *flowDraft) counts() (set, total int) {
	for _, step := range d.steps {
		if strings.TrimSpace(step.Prompt) != "" {
			set++
		}
	}
	return set, len(d.steps)
}

// flowStageLabel names a boundary in the user's language, for a notice that
// has no dialog to point at.
func flowStageLabel(stage flowStage) string {
	for _, s := range flowStages {
		if s.stage == stage {
			return s.title()
		}
	}
	return string(stage)
}

// openProjectFlows fills the dialog from the project's saved steps. A project
// from before the list kept one prompt per stage: those become the steps they
// describe, at the moment each one ran at.
func (w *workspace) openProjectFlows(p *project) {
	if p == nil || p.remote != nil {
		return
	}
	w.flowTarget = p
	w.flowDraft = flowDraft{}
	for _, s := range flowStages {
		if prompt := p.flowPrompt(s.stage); prompt != "" {
			w.flowDraft.steps = append(w.flowDraft.steps,
				&flowStep{At: string(s.stage), Prompt: prompt})
		}
	}
	for _, step := range p.Steps {
		step := step
		w.flowDraft.steps = append(w.flowDraft.steps, &step)
	}
	w.flowOpen = true
	w.invalidate()
}

// submitProjectFlows keeps the steps that were typed and clears the rest. The
// project may have closed while the dialog was open, so it is found by id
// before the answer is applied.
func (w *workspace) submitProjectFlows() {
	p := w.flowTarget
	if p == nil || w.projectByID(p.ID) != p {
		w.closeProjectFlows()
		return
	}
	var steps []flowStep
	for _, step := range w.flowDraft.steps {
		if prompt := strings.TrimSpace(step.Prompt); prompt != "" {
			steps = append(steps, flowStep{At: step.At, Prompt: prompt})
		}
	}
	p.Steps = steps
	// The per-stage prompts are steps now; the old map is not written again.
	p.Flows = nil
	w.closeProjectFlows()
	w.scheduleSave()
}

func (w *workspace) closeProjectFlows() {
	w.flowOpen = false
	w.flowTarget = nil
	w.flowDraft = flowDraft{}
	w.invalidate()
}

// flowDragged is the value a step's handle drags: its place in the draft. The
// fixed stages are anchors, not cards, so only steps carry it.
type flowDragged struct{ Index int }

// projectFlowDialog edits one project's pipeline as a list: the fixed steps
// are anchors that name when their prompts run, and every prompt is a card
// the user added under one of them. The list order is the order things run
// in, and a card's handle drags it to another anchor.
func (w *workspace) projectFlowDialog(c *ui.Context, tk tokens) {
	p := w.flowTarget
	if p == nil {
		return
	}
	set, _ := w.flowDraft.counts()
	flowDialogFrame(c, tk, &w.flowOpen, func() {
		flowDialogHeader(c, tk, p, set)
		ui.Box(c).FillWidth().Height(1).Background(tk.Foreground.Alpha(0.08))
		ui.Scroll(c).FillWidth().
			Height(flowListHeight(len(flowStages), len(w.flowDraft.steps))).
			Children(func() {
				ui.Column(c).FillWidth().Padding(8, 12, 8, 12).Gap(6).Children(func() {
					for _, s := range flowStages {
						w.flowAnchor(c, tk, s.stage, s.title())
						for i, step := range w.flowDraft.steps {
							if step.At != string(s.stage) {
								continue
							}
							// The cards sit under their anchor, one step in.
							ui.Row(c).FillWidth().Padding(0, 0, 0, 12).
								Children(func() { w.flowStepCard(c, tk, i, step) })
						}
					}
				})
			})
		ui.Row(c).FillWidth().Padding(0, 12, 8, 12).Children(func() {
			w.flowAddStep(c, tk)
		})
		ui.Box(c).FillWidth().Height(1).Background(tk.Foreground.Alpha(0.08))
		ui.Row(c).FillWidth().Height(50).Padding(0, 18).Gap(6).
			AlignItems(ui.Center).Justify(ui.End).Children(func() {
			flowDialogButton(c, tk, locales.Cancel(), false, w.closeProjectFlows)
			flowDialogButton(c, tk, locales.Save(), true, w.submitProjectFlows)
		})
	})
}

// flowListHeight is the room the list needs: its anchors, its steps, the gaps
// between them, and the padding, up to what the dialog can show. The add
// button sits below the list, so it stays in reach however long the list gets.
func flowListHeight(anchors, steps int) float32 {
	h := 16 + float32(anchors)*24 + float32(steps)*32 +
		float32(max(anchors+steps-1, 0))*6
	return min(h, 380)
}

// flowAnchor is one fixed step of the pipeline. It holds no prompt of its
// own: it names the moment the cards under it run at, and a card let go on it
// joins that moment.
func (w *workspace) flowAnchor(c *ui.Context, tk tokens, stage flowStage, title string) {
	row := ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 2).Gap(7).
		AlignItems(ui.Center)
	over := false
	if _, ok := ui.DragOver[flowDragged](row); ok {
		over = true
	}
	if v, ok := ui.Drop[flowDragged](row); ok {
		w.moveFlowStep(v.Index, stage, nil)
	}
	if over {
		row = row.Background(dropTint).Radius(6)
	}
	row.Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Shrink(0).
			Background(tk.Foreground.Alpha(0.35))
		ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(tk.Foreground)
		ui.Spacer(c)
		ui.Text(c, locales.FlowFixed()).FontSize(9.5).TextColor(tk.MutedForeground)
	})
}

// flowStepCard is one prompt the user added, running at the anchor above it.
// Its handle drags it to another place in the list, and its text is what the
// agent receives.
func (w *workspace) flowStepCard(c *ui.Context, tk tokens, i int, step *flowStep) {
	card := ui.Row(c).FillWidth().Height(32).Shrink(0).Radius(7).Padding(0, 6).
		Gap(6).AlignItems(ui.Center).Background(tk.Background).
		Border(1, tk.Foreground.Alpha(0.12))
	over := false
	if _, ok := ui.DragOver[flowDragged](card); ok {
		over = true
	}
	if v, ok := ui.Drop[flowDragged](card); ok {
		w.moveFlowStep(v.Index, flowStage(step.At), step)
	}
	if over {
		card = card.Background(dropTint).Border(1, dropAccent)
	}
	card.Children(func() {
		handle := ui.Row(c).Width(14).Height(18).Shrink(0).Center().
			Drag(flowDragged{Index: i}).Cursor(ui.CursorGrab).
			Tooltip(locales.FlowDragHint())
		if handle.Dragging() {
			handle = handle.Background(tk.Foreground.Alpha(0.10)).Radius(4)
		}
		handle.Children(func() {
			ui.Text(c, "⋮⋮").FontSize(10).TextColor(tk.MutedForeground).
				IgnoreInput(true)
		})
		ui.TextInputBase(c, &step.Prompt).Label(locales.FlowStepPlaceholder()).
			Placeholder(locales.FlowStepPlaceholder()).
			PlaceholderColor(tk.MutedForeground.Alpha(0.8)).FontSize(11.5).
			FillWidth()
		del := ui.Row(c).Width(18).Height(18).Radius(4).Center().Shrink(0).
			Cursor(ui.CursorPointer).Tooltip(locales.FlowRemoveStep())
		if del.Hovered() {
			del = del.Background(tk.Foreground.Alpha(0.06))
		}
		del.OnClick(func() { w.removeFlowStep(i) }).Children(func() {
			ui.Text(c, "×").FontSize(11).TextColor(tk.MutedForeground).
				IgnoreInput(true)
		})
	})
}

// flowAddStep adds a step at the last anchor, where the button sits; its
// handle moves it anywhere else.
func (w *workspace) flowAddStep(c *ui.Context, tk tokens) {
	ui.Row(c).FillWidth().Height(30).Shrink(0).Radius(8).Padding(0, 10).Gap(6).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Border(1, tk.Foreground.Alpha(0.14)).BorderStyle(ui.BorderDashed).
		OnClick(func() {
			last := flowStages[len(flowStages)-1].stage
			w.flowDraft.steps = append(w.flowDraft.steps,
				&flowStep{At: string(last)})
			w.invalidate()
		}).Children(func() {
		ui.Text(c, "+").FontSize(12).TextColor(tk.MutedForeground)
		ui.Text(c, locales.FlowAddStep()).FontSize(11.5).TextColor(tk.Foreground)
	})
}

// moveFlowStep puts the dragged step at stage, before the step it was let go
// on, or at the end of that anchor's steps when it was let go on the anchor
// itself. The list stays grouped by anchor, so what is drawn is the order the
// prompts run in.
func (w *workspace) moveFlowStep(from int, stage flowStage, before *flowStep) {
	d := &w.flowDraft
	if from < 0 || from >= len(d.steps) {
		return
	}
	moved := d.steps[from]
	if moved == before {
		return
	}
	rest := make([]*flowStep, 0, len(d.steps)-1)
	for i, step := range d.steps {
		if i != from {
			rest = append(rest, step)
		}
	}
	moved.At = string(stage)
	pos := len(rest)
	if before != nil {
		for i, step := range rest {
			if step == before {
				pos = i
				break
			}
		}
	} else {
		// The end of the anchor's block, before the next anchor's.
		last := flowStageIndex(stage)
		for i, step := range rest {
			if flowStageIndex(flowStage(step.At)) > last {
				pos = i
				break
			}
		}
	}
	out := make([]*flowStep, 0, len(rest)+1)
	out = append(out, rest[:pos]...)
	out = append(out, moved)
	out = append(out, rest[pos:]...)
	d.steps = out
	w.invalidate()
}

// removeFlowStep drops one step the user added.
func (w *workspace) removeFlowStep(i int) {
	if i < 0 || i >= len(w.flowDraft.steps) {
		return
	}
	w.flowDraft.steps = append(w.flowDraft.steps[:i], w.flowDraft.steps[i+1:]...)
	w.invalidate()
}

// flowStageIndex is where a stage sits in the pipeline, or -1 for a name no
// stage carries.
func flowStageIndex(stage flowStage) int {
	for i, s := range flowStages {
		if s.stage == stage {
			return i
		}
	}
	return -1
}

// flowDialogFrame is the flow card's own frame: a solid surface with a thin
// edge and a soft lift, rather than the glass the app's other dialogs wear.
// The card is the whole point here, so it reads as one object on the window.
func flowDialogFrame(c *ui.Context, tk tokens, open *bool, build func()) {
	shade := ui.RGBA(0, 0, 0, 0.24)
	if !tk.Dark {
		shade = ui.RGBA(0, 0, 0, 0.12)
	}
	shadow := alpha(ui.RGB(0, 0, 0), 0.35)
	if !tk.Dark {
		shadow = alpha(ui.RGB(0, 0, 0), 0.12)
	}
	ui.DialogBase(c, open, func(back, panel ui.Element) {
		back.Background(shade).Transition(ui.ElementTransition{
			Duration:     dialogMotion,
			Ease:         ui.EaseOut,
			ExitDuration: dialogExitMotion,
			Enter:        &ui.Motion{},
			Exit:         &ui.Motion{},
		})
		panel.Width(460).Clip().Gap(0).Padding(0).Radius(12).
			Background(tk.Background).
			Border(1, tk.Border).
			Shadow(0, 18, 44, 0, shadow).
			Transition(ui.ElementTransition{
				Duration:     dialogMotion,
				Ease:         ui.EaseOut,
				ExitDuration: dialogExitMotion,
				Enter:        &ui.Motion{Y: 6, Scale: 0.97},
				Exit:         &ui.Motion{Y: 4, Scale: 0.985},
				Colors:       true,
				Shadows:      true,
			}).
			Children(build)
	})
}

// flowDialogButton is the card's small footer action: flat and short, so the
// buttons sit under the note rather than competing with it.
func flowDialogButton(c *ui.Context, tk tokens, label string, primary bool, action func()) {
	b := ui.Button(c, "").Label(label).Height(26).Padding(0, 12).Radius(13).
		Cursor(ui.CursorPointer).OnClick(action).Children(func() {
		ui.Text(c, label).FontSize(11.5).FontWeight(500)
	})
	if primary {
		b = b.Background(tk.Foreground).TextColor(tk.Background)
	} else {
		b = b.Background(tk.Foreground.Alpha(0.06)).
			TextColor(tk.Foreground).Border(1, tk.Foreground.Alpha(0.12))
	}
}

// flowDialogHeader names the workflow, the project, and how many prompts are
// set, as the agent steering card names its run and its place in it.
func flowDialogHeader(c *ui.Context, tk tokens, p *project, set int) {
	ui.Column(c).FillWidth().Padding(16, 20, 12, 20).Children(func() {
		ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "✦").FontSize(14).TextColor(tk.SystemAccent).Margin(0, 0, 1, 0)
			ui.Column(c).Grow(1).Gap(1).Children(func() {
				ui.Text(c, locales.FlowTitle()).FontSize(14.5).FontWeight(600).
					TextColor(tk.Foreground)
				ui.Text(c, p.name()+" · "+locales.FlowStepCount(strconv.Itoa(len(flowStages)))).
					FontSize(11).TextColor(tk.MutedForeground)
			})
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				dot := tk.MutedForeground.Alpha(0.4)
				if set > 0 {
					dot = tk.SystemAccent
				}
				ui.Box(c).Width(5).Height(5).Radius(2.5).Background(dot)
				ui.Text(c, locales.FlowPromptCount(strconv.Itoa(set))).
					FontSize(10.5).TextColor(tk.MutedForeground)
			})
		})
	})
}

// flowPlaceholder is the example the selected step's note offers.
func flowPlaceholder(stage flowStage) string {
	for _, s := range flowStages {
		if s.stage == stage {
			return s.placeholder()
		}
	}
	return ""
}

// flowTask is one prompt waiting for, or running in, a pane. Everything is
// touched on the main thread: queueFlow from the stage that just finished,
// and drainFlows from the agent monitor's tick.
type flowTask struct {
	paneID string
	stage  flowStage
	// prompts run in order at the stage; at is the one being sent now.
	prompts []string
	at      int
	queued  time.Time
	sent    time.Time
	started bool
	// archiveAfter clears a merged checkout away once the flow's turn ends.
	// It carries the Settings › Worktrees choice across the wait, so the
	// flow is not cut off by the checkout disappearing under it.
	archiveAfter bool
}

// prompt is the one the task is sending now.
func (t *flowTask) prompt() string { return t.prompts[t.at] }

const (
	// flowAgentWait bounds how long a flow waits for the pane's agent to be
	// recognized. A pane whose agent never comes back would otherwise hold
	// the prompt for the rest of the session.
	flowAgentWait = 60 * time.Second
	// flowStartWait bounds how long a sent prompt may take to start its
	// turn. A merged checkout is left open when it never does, because an
	// agent that is working without a visible turn must not be archived
	// under.
	flowStartWait = 60 * time.Second
)

// queueFlow queues what the project runs at stage, in order, and reports
// whether there was anything. archiveAfter asks the merged checkout to wait
// for the flow before it clears itself away.
func (w *workspace) queueFlow(p *pane, stage flowStage, archiveAfter bool) bool {
	if p == nil || p.remote != nil {
		return false
	}
	pr := w.projectOf(w.sessionOf(p))
	prompts := pr.flowPrompts(stage)
	if len(prompts) == 0 {
		return false
	}
	w.flows = append(w.flows, flowTask{
		paneID:       p.ID,
		stage:        stage,
		prompts:      prompts,
		queued:       time.Now(),
		archiveAfter: archiveAfter,
	})
	return true
}

// drainFlows sends each queued prompt once its pane has an agent and the
// agent is idle, then archives a merged checkout whose flow has finished.
// Main thread, from the agent monitor's tick.
func (w *workspace) drainFlows() {
	for i := 0; i < len(w.flows); {
		task := &w.flows[i]
		p := w.paneByID(task.paneID)
		if p == nil || p.remote != nil {
			w.dropFlow(i)
			continue
		}
		if task.sent.IsZero() {
			if p.agent == nil {
				if time.Since(task.queued) > flowAgentWait {
					w.flash(locales.FlowSkippedNoAgent(flowStageLabel(task.stage)))
					if task.archiveAfter && p.Worktree != nil && p.Worktree.State == worktreeMerged {
						w.dropFlow(i)
						w.archiveMergedAndReturn(p)
						continue
					}
					w.dropFlow(i)
					continue
				}
				i++
				continue
			}
			// A prompt typed mid-turn would land in someone else's work;
			// wait for the agent to be ready for it.
			if p.working() {
				i++
				continue
			}
			p.injectedPrompt = task.prompt()
			p.send(task.prompt() + "\r")
			task.sent = time.Now()
			// The last prompt of a flow with nothing waiting on it is done
			// once it is typed; one with more prompts, or a merged checkout
			// to archive, keeps its task until the turn ends.
			if task.at == len(task.prompts)-1 && !task.archiveAfter {
				w.dropFlow(i)
				continue
			}
			i++
			continue
		}
		if p.working() {
			task.started = true
			i++
			continue
		}
		if !task.started {
			if time.Since(task.sent) < flowStartWait {
				i++
				continue
			}
			// The turn never showed. Leave the merged checkout in place:
			// an agent can be working without its turn being visible, and
			// the user can archive when it is done.
			w.dropFlow(i)
			continue
		}
		if task.at < len(task.prompts)-1 {
			// The turn is over: the next prompt goes in once the agent is
			// idle again, so a flow reads as one message after another.
			task.at++
			task.sent = time.Time{}
			task.started = false
			i++
			continue
		}
		w.dropFlow(i)
		if p.Worktree != nil && p.Worktree.State == worktreeMerged {
			w.archiveMergedAndReturn(p)
		}
	}
}

// dropFlow removes the task at index i without disturbing the order of the
// ones after it.
func (w *workspace) dropFlow(i int) {
	w.flows = append(w.flows[:i], w.flows[i+1:]...)
}
