package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The stage rail is zoom's other half: when one pane fills the session, the
// panes it hides wait on the right as cards, as Stage Manager keeps the
// windows behind the one in front. A card is not a live view of its
// terminal — mygo has no per-pane bitmap capture, and a terminal redrawn at
// card size would be illegible — so it holds the pane's mark, its name and
// state, and the last lines it showed when the rail last looked.
const (
	stageRailWidth float32 = 188
	// stageRailMinStage keeps the stage wide enough to work in before the
	// rail may take its edge.
	stageRailMinStage float32 = 720
	// stageCardWidth is the rail less its 8pt left and 12pt right padding.
	stageCardWidth  float32 = stageRailWidth - 20
	stageCardHeight float32 = 108
	stageRailGap    float32 = 8
	stagePeekLines          = 4
	stagePeekCols           = 27
)

// stageRailPanes are the panes the rail shows: every pane of the session
// except the one on stage, in the split tree's order, so a card keeps its
// place as the stage moves.
func (w *workspace) stageRailPanes(s *session) []*pane {
	if s == nil || s.Root == nil || w.zoomedPane == "" {
		return nil
	}
	stage := s.Root.paneByID(w.zoomedPane)
	if stage == nil {
		return nil
	}
	var out []*pane
	for _, p := range s.Root.panes() {
		if p.ID != stage.ID {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stageRailVisible reports whether the rail belongs on screen: a pane is
// zoomed, it hides another pane, and the drawer is not covering that edge.
func (w *workspace) stageRailVisible(s *session) bool {
	if w.drawer.open || w.drawer.closing {
		return false
	}
	return len(w.stageRailPanes(s)) > 0
}

// stagePane brings p to the stage: it fills the session, takes the focus, and
// the pane it replaces joins the rail.
func (w *workspace) stagePane(p *pane) {
	if p == nil || p.ID == w.zoomedPane {
		return
	}
	if s := w.sessionOf(p); s != nil && s.Root != nil {
		if cur := s.Root.paneByID(w.zoomedPane); cur != nil {
			cur.captureStagePeek()
		}
	}
	w.zoomedPane = p.ID
	w.FocusedPane = p.ID
	w.focusReq = p.ID
	w.invalidate()
}

// captureStagePeeks fills the rail's previews when a pane zooms: the panes it
// hides were on screen a frame ago, so their cards start with what they showed.
func (w *workspace) captureStagePeeks(stage *pane) {
	s := w.sessionOf(stage)
	if s == nil || s.Root == nil {
		return
	}
	for _, p := range s.Root.panes() {
		if p.ID != stage.ID {
			p.captureStagePeek()
		}
	}
}

// captureStagePeek remembers the tail of what the pane shows, for its card.
func (p *pane) captureStagePeek() {
	if p.term == nil {
		return
	}
	p.stagePeek = stagePeekLinesOf(p.text())
}

// stagePeekLinesOf keeps a terminal's text as a card shows it: the last
// lines, trailing blank ones dropped, each cut to the card's width from the
// right, where the visible end of a wrapped line sits.
func stagePeekLinesOf(text string) []string {
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > stagePeekLines {
		lines = lines[len(lines)-stagePeekLines:]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \r")
		line = strings.ReplaceAll(line, "\t", "  ")
		runes := []rune(line)
		if len(runes) > stagePeekCols {
			runes = runes[len(runes)-stagePeekCols:]
		}
		out = append(out, string(runes))
	}
	return out
}

// stageRail draws the off-stage panes of s, top aligned on the right edge.
func (w *workspace) stageRail(c *ui.Context, tk tokens, s *session) {
	panes := w.stageRailPanes(s)
	if len(panes) == 0 {
		return
	}
	// The rail slides in once per zoom; moving between stages keeps it still,
	// so the hand does not chase the cards it just clicked.
	pose := ui.Pose{}
	if !w.stageRailShown {
		w.stageRailShown = true
		pose = ui.Pose{X: 12, Fade: 1}
		c.After(time.Millisecond)
	}
	rail := ui.Column(c).Width(stageRailWidth).FillHeight().Clip().
		AnimatePose(pose, ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut})
	rail.Children(func() {
		ui.Scroll(c).Fill().Padding(8, 12, 8, 8).Gap(stageRailGap).Children(func() {
			for _, p := range panes {
				w.stageCard(c, tk, p)
			}
		})
	})
}

// stageCard is one off-stage pane: its mark and name, the folder it works in,
// and the last lines it showed.
func (w *workspace) stageCard(c *ui.Context, tk tokens, p *pane) {
	card := ui.Column(c).Width(stageCardWidth).Height(stageCardHeight).Padding(10).Gap(2).
		Radius(8).Border(1, tk.Border).Clip().Cursor(ui.CursorPointer).
		Tooltip(p.dir())
	hovered := card.Hovered()
	// In comfortable the canvas is muted and a card lifts off it; in compact
	// the canvas is the content surface, so the card steps down instead.
	bg := tk.Background
	if w.settings.PaneStyle != paneComfortable {
		bg = tk.Muted
	}
	if hovered {
		bg = tk.Active
	}
	card = card.Background(card.AnimateColor("bg", bg,
		ui.Animation{Duration: 100 * time.Millisecond, Ease: ui.EaseOut}))
	pick := func() { w.stagePane(p) }
	card.OnClick(pick)
	card.Children(func() {
		ui.Row(c).Height(16).Gap(6).AlignItems(ui.Center).Children(func() {
			if agent := p.agent; agent != nil {
				if src := agentLogoSource(agent.Kind, tk); src != nil {
					ui.Image(c, src).Width(13).Height(13).OnClick(pick)
				}
			} else if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
				ui.Image(c, src).Width(14).Height(14).OnClick(pick)
			}
			stageCardTitle(c, tk, p).OnClick(pick)
		})
		ui.Text(c, abbreviate(p.dir())).FontSize(10.5).SingleLine().
			TextColor(tk.MutedForeground).OnClick(pick)
		// The card's screen: the tail the pane last showed, in the terminal's
		// own face, clipped rather than wrapped like a terminal.
		ui.Column(c).Grow(1).Gap(1).Clip().OnClick(pick).Children(func() {
			for _, line := range p.stagePeek {
				ui.Text(c, line).Font(terminalFont).FontSize(9).SingleLine().
					TextColor(tk.MutedForeground).IgnoreInput(true)
			}
		})
	})
}

// stageCardTitle is the pane's name with the state the agent reports: the
// sidebar's shimmer while it works, full ink while it waits, quiet when idle.
func stageCardTitle(c *ui.Context, tk tokens, p *pane) ui.Element {
	label := p.title()
	if n := p.subagentCount(); n > 0 {
		label = fmt.Sprintf("%s · %d", label, n)
	}
	state := ""
	if p.activity != nil {
		state = p.activity.State
	}
	switch state {
	case activityWorking:
		return ui.RichText(c, shimmerSpans(label, shimmerPhase(c), tk.Foreground)...).
			FontSize(12).FontWeight(500).SingleLine()
	case activityWaiting:
		return ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine().
			TextColor(tk.Foreground)
	}
	return ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine().
		TextColor(tk.MutedForeground)
}
