package main

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// checkpointsWidth is the card's width: a step's sentence fits without
	// crowding the terminal it floats over.
	checkpointsWidth float32 = 292
	// checkpointRowHeight is one stage's line. Every row is the same height
	// so the card does not jump as the operation moves from stage to stage.
	checkpointRowHeight float32 = 22
	// checkpointMotion is how long the card and each stage take to settle
	// into place. Short enough that a wait already under way never reads as
	// waiting on the motion itself.
	checkpointMotion = 180 * time.Millisecond
)

// worktreeCheckpoints draws a running pull request, merge, or conflict
// resolution as a line of checkpoints in the pane's upper right corner, over
// the terminal. Stages appear as the operation reaches them: the ones behind
// the current stage are marked off, and the current one is lit. It is on
// screen without a hover, so a wait that runs long says where it is at a
// glance; the header button keeps only the operation's name.
func (w *workspace) worktreeCheckpoints(c *ui.Context, tk tokens, p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy == "" || len(wt.busySteps) == 0 {
		return
	}
	title := worktreeBusyLabel(wt)
	if title == "" {
		return
	}
	at := -1
	for i, step := range wt.busySteps {
		if step == wt.busyAt {
			at = i
			break
		}
	}
	// The card is a floating surface, so it takes the component's soft lift
	// rather than the chrome's flat border alone. Its fill steps off the
	// pane's paper: a card of that same paper over a pane of that same paper
	// has nothing but its hairline to exist by, which reads as a grey outline
	// rather than as a card floating over the terminal.
	shadow := alpha(ui.RGB(0, 0, 0), 0.14)
	if tk.Dark {
		shadow = alpha(ui.RGB(0, 0, 0), 0.42)
	}
	card := ui.Column(c).Width(checkpointsWidth).MaxWidthPercent(94).
		Absolute().Right(8).Top(w.paneHeaderHeight()+6).
		Radius(12).Clip().
		Border(1, tk.Border).Background(floatingFill(tk)).
		Shadow(0, 6, 20, 0, shadow).
		IgnoreInput(true)
	// The card drops into its corner, faded, as the stage's cards and the
	// drawers arrive: a merge can start the moment the menu closes, and a
	// panel that pops there reads as a redraw rather than as work beginning.
	// It leaves the same way, so the end of the operation does not erase the
	// last stage before it can be read. Height follows the rows, so a stage
	// added mid-operation grows the card instead of jumping it a line taller.
	card.Transition(ui.ElementTransition{
		Duration:     checkpointMotion,
		Ease:         ui.EaseOut,
		ExitDuration: 140 * time.Millisecond,
		Enter:        &ui.Motion{Y: -6},
		Exit:         &ui.Motion{Y: -4},
		Height:       true,
	})
	card.Children(func() {
		checkpointsHead(c, tk, title, wt)
		ui.Column(c).FillWidth().Padding(0, 6, 8, 6).Children(func() {
			// Only the stages already reached are drawn, so the card
			// grows a line at a time with the operation.
			for i := 0; i <= at; i++ {
				checkpointRow(c, tk, p, wt.busySteps[i], i, at)
			}
		})
	})
}

// checkpointsHead names the operation and the branch it works on.
func checkpointsHead(c *ui.Context, tk tokens, title string, wt *paneWorktree) {
	ui.Row(c).FillWidth().Padding(10, 12, 3, 12).Gap(7).
		AlignItems(ui.Center).Children(func() {
		if src := iconSource("branch", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(13).Height(13)
		}
		ui.Text(c, title).FontSize(12).FontWeight(600).
			TextColor(tk.Foreground).SingleLine().Shrink(1)
		if wt.Branch != "" {
			ui.Text(c, wt.Branch+" → "+wt.Base).FontSize(11).
				TextColor(tk.MutedForeground).SingleLine().Shrink(1)
		}
		ui.Spacer(c)
	})
}

// checkpointRow is one stage: its dot on the rail, and its name in the
// operation's own words. The dot marks the stage reached and the rows behind
// it fade and take a check, so the card reads at a glance as well as in order.
func checkpointRow(c *ui.Context, tk tokens, p *pane, step worktreeStep,
	i, at int) {
	// A stage is keyed by itself rather than by its line: a new operation
	// that reuses the card animates its stages in, while one already drawn
	// keeps its place as the operation moves on to the next.
	row := ui.Row(c.Key("step-"+strconv.Itoa(int(step)))).
		FillWidth().Height(checkpointRowHeight).Padding(0, 12).Gap(8).
		AlignItems(ui.Center)
	// A stage that has just been reached rises into its line, faded, so the
	// card grows a line instead of jumping one taller.
	row.Transition(ui.ElementTransition{
		Duration: checkpointMotion,
		Ease:     ui.EaseOut,
		Enter:    &ui.Motion{Y: 5},
	})
	row.Children(func() {
		gutter := ui.Box(c).Width(12).Height(checkpointRowHeight)
		gutter.Children(func() {
			// The rail is drawn in halves, so the first and last
			// checkpoints end at their dot instead of running on. Both
			// halves stay in the tree and only change colour, so the line
			// grows into the stage reached instead of appearing with it.
			above := ui.Transparent
			if i > 0 {
				above = tk.SystemAccent.Alpha(0.55)
			}
			aboveBox := ui.Box(c).Width(1).Height(checkpointRowHeight / 2).
				Absolute().Left(5.5).Top(0)
			aboveBox.Background(aboveBox.AnimateColor("railAbove", above,
				ui.Animation{Duration: checkpointMotion, Ease: ui.EaseOut}))
			below := ui.Transparent
			if i < at {
				below = tk.SystemAccent.Alpha(0.55)
			}
			belowBox := ui.Box(c).Width(1).Height(checkpointRowHeight / 2).
				Absolute().Left(5.5).Top(checkpointRowHeight / 2)
			belowBox.Background(belowBox.AnimateColor("railBelow", below,
				ui.Animation{Duration: checkpointMotion, Ease: ui.EaseOut}))
			// The reached stages are filled dots, and the current one is
			// larger and in the full accent, as the component's head is.
			// They sit on the line's ink rather than its box: a line of
			// text keeps more room below the glyphs than above them, so a
			// dot centred on the box reads high beside them. The dot eases
			// between the two sizes as its stage passes, instead of one dot
			// replacing the other.
			size := float32(0)
			tone := ui.Transparent
			switch {
			case i < at:
				size, tone = 6, tk.SystemAccent.Alpha(0.6)
			case i == at:
				size, tone = 8, tk.SystemAccent
			}
			if size > 0 {
				dot := ui.Box(c).Absolute()
				size = dot.Animate("checkpointDotSize", size, checkpointMotion)
				dot.Width(size).Height(size).Radius(size / 2).
					Left(6 - size/2).Top(12.5 - size/2)
				dot.Background(dot.AnimateColor("checkpointDotColor", tone,
					ui.Animation{Duration: checkpointMotion, Ease: ui.EaseOut}))
			}
		})
		label := worktreeStepLabel(p, step)
		// The current stage's label and a passed one are two keys in the
		// same line: when the operation moves on, the current label fades
		// out as the check's label fades in, rather than the words
		// replacing one another mid-sentence.
		fade := ui.ElementTransition{
			Duration: 140 * time.Millisecond,
			Ease:     ui.EaseOut,
			Enter:    &ui.Motion{},
			Exit:     &ui.Motion{},
		}
		switch {
		case i < at:
			ui.Text(c.Key("passed"), label).FontSize(11.5).
				TextColor(tk.MutedForeground).SingleLine().Shrink(1).
				Transition(fade)
			ui.Spacer(c)
			check := ui.Text(c.Key("check"), "✓").FontSize(10).
				TextColor(tk.MutedForeground.Alpha(0.8))
			// The check lands with the stage it marks, rather than being
			// there the moment the stage moves on.
			check.Transition(ui.ElementTransition{
				Duration: checkpointMotion,
				Ease:     ui.EaseOut,
				Enter:    &ui.Motion{Scale: 0.7},
			})
		case i == at:
			ui.Text(c.Key("current"), label).FontSize(11.5).FontWeight(500).
				TextColor(tk.Foreground).SingleLine().Shrink(1).
				Transition(fade)
		}
	})
}
