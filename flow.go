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

// flowPrompt is the prompt the project runs at stage, trimmed, or "" when it
// has no flow there.
func (p *project) flowPrompt(stage flowStage) string {
	if p == nil || p.Flows == nil {
		return ""
	}
	return strings.TrimSpace(p.Flows[string(stage)])
}

// flowDraft is the dialog's copy of a project's flows: fields are edited
// here, and saved over the project only when the user confirms.
type flowDraft struct {
	afterWorktree string
	afterPR       string
	afterMerge    string
}

// field is the draft's input for one stage.
func (d *flowDraft) field(stage flowStage) *string {
	switch stage {
	case flowAfterWorktree:
		return &d.afterWorktree
	case flowAfterPR:
		return &d.afterPR
	case flowAfterMerge:
		return &d.afterMerge
	}
	return nil
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

// openProjectFlows fills the dialog from the project's saved flows.
func (w *workspace) openProjectFlows(p *project) {
	if p == nil || p.remote != nil {
		return
	}
	w.flowTarget = p
	w.flowDraft = flowDraft{}
	w.flowSelected = flowStages[0].stage
	for _, s := range flowStages {
		if field := w.flowDraft.field(s.stage); field != nil {
			*field = p.flowPrompt(s.stage)
		}
	}
	w.flowOpen = true
	w.invalidate()
}

// submitProjectFlows keeps the prompts that were typed and clears the rest.
// The project may have closed while the dialog was open, so it is found by
// id before the answer is applied.
func (w *workspace) submitProjectFlows() {
	p := w.flowTarget
	if p == nil || w.projectByID(p.ID) != p {
		w.closeProjectFlows()
		return
	}
	flows := projectFlows{}
	for _, s := range flowStages {
		field := w.flowDraft.field(s.stage)
		if field == nil {
			continue
		}
		if prompt := strings.TrimSpace(*field); prompt != "" {
			flows[string(s.stage)] = prompt
		}
	}
	if len(flows) == 0 {
		p.Flows = nil
	} else {
		p.Flows = flows
	}
	w.closeProjectFlows()
	w.scheduleSave()
}

func (w *workspace) closeProjectFlows() {
	w.flowOpen = false
	w.flowTarget = nil
	w.flowDraft = flowDraft{}
	w.flowSelected = ""
	w.invalidate()
}

// projectFlowDialog edits one project's prompts as the pipeline itself: a
// solid card of steps, each with a note. A set step wears a filled accent
// marker and its note a bar of the same ink; an empty one stays a dashed ring
// over a dashed well, so the whole workflow reads before a field is touched.
func (w *workspace) projectFlowDialog(c *ui.Context, tk tokens) {
	p := w.flowTarget
	if p == nil {
		return
	}
	set := 0
	for _, s := range flowStages {
		if field := w.flowDraft.field(s.stage); field != nil &&
			strings.TrimSpace(*field) != "" {
			set++
		}
	}
	selected := w.flowSelected
	if selected == "" {
		selected = flowStages[0].stage
	}
	flowDialogFrame(c, tk, &w.flowOpen, func() {
		flowDialogHeader(c, tk, p, set)
		ui.Box(c).FillWidth().Height(1).Background(tk.Foreground.Alpha(0.08))
		ui.Column(c).FillWidth().Padding(8, 12, 8, 12).Children(func() {
			for i, s := range flowStages {
				field := w.flowDraft.field(s.stage)
				if field == nil {
					continue
				}
				w.flowDialogStep(c, tk, s.stage, s.title(), i, len(flowStages), field,
					s.stage == selected)
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(tk.Foreground.Alpha(0.08))
		w.flowComposer(c, tk, selected)
		ui.Box(c).FillWidth().Height(1).Background(tk.Foreground.Alpha(0.08))
		ui.Row(c).FillWidth().Height(50).Padding(0, 18).Gap(6).
			AlignItems(ui.Center).Justify(ui.End).Children(func() {
			flowDialogButton(c, tk, locales.Cancel(), false, w.closeProjectFlows)
			flowDialogButton(c, tk, locales.Save(), true, w.submitProjectFlows)
		})
	})
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
		back.Background(shade)
		panel.Width(460).Clip().Gap(0).Padding(0).Radius(12).
			Background(tk.Background).
			Border(1, tk.Border).
			Shadow(0, 18, 44, 0, shadow).
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

// flowDialogHeader names the workflow, the project, and how many steps are
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
				ui.Text(c, locales.FlowSetCount(strconv.Itoa(set), strconv.Itoa(len(flowStages)))).
					FontSize(10.5).TextColor(tk.MutedForeground)
			})
		})
	})
}

// flowStepHeight is one step's line in the list.
const flowStepHeight float32 = 34

// flowDialogStep draws one step on the rail, with its prompt at the right
// where the steering card keeps its durations. A press selects the step for
// the note below.
func (w *workspace) flowDialogStep(c *ui.Context, tk tokens, stage flowStage,
	title string, i, total int, field *string, selected bool) {
	set := strings.TrimSpace(*field) != ""
	ui.Row(c).FillWidth().Height(flowStepHeight).Children(func() {
		ui.Box(c).Width(28).Height(flowStepHeight).Children(func() {
			line := tk.Foreground.Alpha(0.18)
			if i > 0 {
				ui.Box(c).Width(1).Height(17).Absolute().Left(13.5).Top(0).
					Background(line)
			}
			if i < total-1 {
				ui.Box(c).Width(1).Height(17).Absolute().Left(13.5).Top(17).
					Background(line)
			}
			marker := ui.Box(c).Width(16).Height(16).Radius(8).
				Absolute().Left(6).Top(9)
			if set {
				marker.Background(tk.SystemAccent).Center().Children(func() {
					ui.Text(c, "✓").FontSize(9).FontWeight(600).
						TextColor(ui.RGB(255, 255, 255))
				})
			} else {
				marker.Border(1, tk.Foreground.Alpha(0.30)).BorderStyle(ui.BorderDashed)
			}
		})
		content := ui.Column(c).Grow(1).Height(flowStepHeight).Padding(0, 7).
			Justify(ui.Center).Cursor(ui.CursorPointer)
		content.OnClick(func() {
			w.flowSelected = stage
			w.invalidate()
		}).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				titleColor := tk.MutedForeground
				weight := 500
				if set || selected {
					titleColor = tk.Foreground
				}
				if selected {
					weight = 600
				}
				ui.Text(c, title).FontSize(12.5).FontWeight(weight).TextColor(titleColor)
				ui.Spacer(c)
				right := locales.FlowUnset()
				if set {
					right = strings.TrimSpace(*field)
				}
				label := ui.Text(c, right).FontSize(10.5).TextColor(tk.MutedForeground).
					SingleLine().Shrink(1).MaxWidth(190)
				if set {
					label.Tooltip(right)
				}
			})
		})
	})
}

// flowComposer edits the selected step's prompt: one note at a time, as the
// steering card's queue takes one note for the next step.
func (w *workspace) flowComposer(c *ui.Context, tk tokens, stage flowStage) {
	field := w.flowDraft.field(stage)
	if field == nil {
		return
	}
	set := strings.TrimSpace(*field) != ""
	ui.Column(c).FillWidth().Padding(12, 20, 14, 20).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.FlowNoteLabel(flowStageLabel(stage))).FontSize(9.5).
				FontWeight(500).LetterSpacing(0.5).TextColor(tk.MutedForeground)
			ui.Spacer(c)
			if set {
				ui.Text(c, locales.FlowSet()).FontSize(10).TextColor(tk.MutedForeground)
			}
		})
		in := flowNoteField(c, tk, field, flowPlaceholder(stage), set, func() {
			*field = ""
			w.invalidate()
		}).Margin(7, 0, 0, 0)
		in.AutoFocus()
		in.OnSubmit(func() { w.submitProjectFlows() })
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

// flowNoteField is the composer's note: a quiet well while empty, and a white
// note with an accent bar once it holds a prompt, as a steering note does.
func flowNoteField(c *ui.Context, tk tokens, value *string, placeholder string,
	set bool, clear func()) ui.Element {
	row := ui.Row(c).FillWidth().Height(32).Radius(7).Padding(0, 8).Gap(6).
		AlignItems(ui.Center).Cursor(ui.CursorText)
	if set {
		row = row.Background(tk.Background).
			Border(1, tk.Foreground.Alpha(0.14))
	} else {
		row = row.Background(tk.Foreground.Alpha(0.02)).
			Border(1, tk.Foreground.Alpha(0.16)).
			BorderStyle(ui.BorderDashed)
	}
	var in ui.Element
	row.OnClick(func() { in.Focus() }).Children(func() {
		if set {
			ui.Box(c).Width(2).Height(14).Radius(1).Shrink(0).
				Background(tk.SystemAccent)
		}
		in = ui.TextInputBase(c, value).Label(placeholder).Placeholder(placeholder).
			PlaceholderColor(tk.MutedForeground.Alpha(0.8)).FontSize(11.5).
			FontWeight(400).FillWidth()
		if set {
			clearButton := ui.Row(c).Width(18).Height(18).Radius(4).
				Center().Shrink(0).Cursor(ui.CursorPointer)
			if clearButton.Hovered() {
				clearButton = clearButton.Background(tk.Foreground.Alpha(0.06))
			}
			clearButton.OnClick(clear).Children(func() {
				ui.Text(c, "×").FontSize(11).TextColor(tk.MutedForeground).
					IgnoreInput(true)
			})
		}
	})
	if in.Focused() {
		row = row.BorderColor(tk.MutedForeground)
	}
	return row
}

// flowTask is one prompt waiting for, or running in, a pane. Everything is
// touched on the main thread: queueFlow from the stage that just finished,
// and drainFlows from the agent monitor's tick.
type flowTask struct {
	paneID  string
	stage   flowStage
	prompt  string
	queued  time.Time
	sent    time.Time
	started bool
	// archiveAfter clears a merged checkout away once the flow's turn ends.
	// It carries the Settings › Worktrees choice across the wait, so the
	// flow is not cut off by the checkout disappearing under it.
	archiveAfter bool
}

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

// queueFlow queues the project's prompt for stage, and reports whether there
// was one. archiveAfter asks the merged checkout to wait for the flow before
// it clears itself away.
func (w *workspace) queueFlow(p *pane, stage flowStage, archiveAfter bool) bool {
	if p == nil || p.remote != nil {
		return false
	}
	pr := w.projectOf(w.sessionOf(p))
	prompt := pr.flowPrompt(stage)
	if prompt == "" {
		return false
	}
	w.flows = append(w.flows, flowTask{
		paneID:       p.ID,
		stage:        stage,
		prompt:       prompt,
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
			p.injectedPrompt = task.prompt
			p.send(task.prompt + "\r")
			task.sent = time.Now()
			// A prompt with nothing waiting on it is done once it is typed; a
			// merged checkout keeps its task until the flow's turn ends.
			if !task.archiveAfter {
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
