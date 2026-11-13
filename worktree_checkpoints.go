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
// the current stage are marked off, and the current one shimmers. It is on
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
	// rather than the chrome's flat border alone.
	shadow := alpha(ui.RGB(0, 0, 0), 0.14)
	if tk.Dark {
		shadow = alpha(ui.RGB(0, 0, 0), 0.42)
	}
	card := ui.Column(c).Width(checkpointsWidth).MaxWidthPercent(94).
		Absolute().Right(8).Top(headerHeight+6).
		Radius(12).Clip().
		Border(1, tk.Border).Background(tk.Background).
		Shadow(0, 6, 20, 0, shadow).
		IgnoreInput(true)
	// The card drops into its corner, faded, as the stage's cards and the
	// drawers arrive: a merge can start the moment the menu closes, and a
	// panel that pops there reads as a redraw rather than as work beginning.
	entered := ui.Local(card, "entered", func() bool { return false })
	pose := ui.Pose{}
	if !*entered {
		*entered = true
		pose = ui.Pose{Y: -6, Fade: 1}
		c.After(time.Millisecond)
	}
	card.AnimatePose(pose, ui.Animation{Duration: checkpointMotion, Ease: ui.EaseOut})
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
	row := ui.Row(c).Key("step-"+strconv.Itoa(int(step))).
		FillWidth().Height(checkpointRowHeight).Padding(0, 12).Gap(8).
		AlignItems(ui.Center)
	// A stage that has just been reached rises into its line, faded, so the
	// card grows a line instead of jumping one taller.
	entered := ui.Local(row, "entered", func() bool { return false })
	pose := ui.Pose{}
	if !*entered {
		*entered = true
		pose = ui.Pose{Y: 5, Fade: 1}
		c.After(time.Millisecond)
	}
	row.AnimatePose(pose, ui.Animation{Duration: checkpointMotion, Ease: ui.EaseOut})
	row.Children(func() {
		gutter := ui.Box(c).Width(12).Height(checkpointRowHeight)
		gutter.Children(func() {
			// The rail is drawn in halves, so the first and last
			// checkpoints end at their dot instead of running on. The
			// current stage is the last one drawn, so it ends there
			// until the operation adds the next.
			above := tk.Border
			if i <= at {
				above = tk.SystemAccent.Alpha(0.55)
			}
			below := tk.Border
			if i < at {
				below = tk.SystemAccent.Alpha(0.55)
			}
			if i > 0 {
				ui.Box(c).Width(1).Height(checkpointRowHeight / 2).
					Absolute().Left(5.5).Top(0).Background(above)
			}
			if i < at {
				ui.Box(c).Width(1).Height(checkpointRowHeight / 2).
					Absolute().Left(5.5).Top(checkpointRowHeight / 2).
					Background(below)
			}
			// The reached stages are filled dots, and the current one is
			// larger and in the full accent, as the component's head is.
			switch {
			case i < at:
				ui.Box(c).Width(6).Height(6).Radius(3).
					Absolute().Left(3).Top(8).
					Background(tk.SystemAccent.Alpha(0.6))
			case i == at:
				ui.Box(c).Width(8).Height(8).Radius(4).
					Absolute().Left(2).Top(7).
					Background(tk.SystemAccent)
			}
		})
		label := worktreeStepLabel(p, step)
		switch {
		case i < at:
			ui.Text(c, label).FontSize(11.5).TextColor(tk.MutedForeground).
				SingleLine().Shrink(1)
			ui.Spacer(c)
			check := ui.Text(c, "✓").FontSize(10).
				TextColor(tk.MutedForeground.Alpha(0.8))
			// The check lands with the stage it marks, rather than being
			// there the moment the shimmer stops.
			seen := ui.Local(check, "entered", func() bool { return false })
			checkPose := ui.Pose{}
			if !*seen {
				*seen = true
				checkPose = ui.Pose{Scale: 0.7, Fade: 1}
				c.After(time.Millisecond)
			}
			check.AnimatePose(checkPose,
				ui.Animation{Duration: checkpointMotion, Ease: ui.EaseOut})
		case i == at:
			shimmerLabel(c, tk, label, 11.5, 500).Shrink(1)
		}
	})
}
