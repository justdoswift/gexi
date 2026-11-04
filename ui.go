package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The layout follows Rex: the window has no title bar of its own, the traffic
// lights sit over the top-left corner, and a session sidebar or the top-left
// pane header takes that first row. The sidebar toggle never moves.
//
// The chrome itself follows shadcn: one neutral palette, 1px borders, 6px
// radius, quiet hover states.

func (w *workspace) view(c *ui.Context) {
	tk := themeTokens(c.Theme())
	bar := c.TitleBar()
	// The window's own surface is the canvas the panes sit on, so a pane
	// that fades back fades toward that canvas, not toward its own colour.
	root := ui.Row(c).Fill().Background(w.backdrop(tk))

	target := float32(0)
	if !w.settings.SidebarCollapsed {
		target = sidebarWidth
	}
	width := root.Animate("sidebarWidth", target, sidebarMotion)

	root.Children(func() {
		w.sidebar(c, tk, width)
		// The sidebar and the space around the panes share one background,
		// so in comfortable a pane reads as a panel wrapped around its
		// content. The canvas keeps an extra inset from the top, right, and
		// bottom edges; the left one is the sidebar's own margin.
		inset := float32(0)
		if w.settings.PaneStyle == paneComfortable {
			inset = canvasInset
		}
		ui.Column(c).Fill().Background(w.backdrop(tk)).Padding(inset, inset, inset, 0).
			Children(func() {
				w.canvas(c, tk)
			})
		// The toggle stays put whether the sidebar is shown or not, so it
		// sits above the layout rather than inside either header.
		top := float32(11)
		ui.Row(c).Width(iconSize).Height(iconSize).Absolute().
			Left(bar.Left + 6).Top(top).Children(func() {
			w.sidebarToggle(c, tk)
		})
	})
}

// dragArea is a draggable stretch of a header that also zooms the window on
// a double click.
func (w *workspace) dragArea(c *ui.Context, tk tokens) ui.Element {
	return ui.Spacer(c).DragWindow()
}

func (w *workspace) backdrop(tk tokens) ui.Color {
	if w.settings.PaneStyle == paneComfortable {
		return tk.Muted
	}
	return tk.Background
}

func (w *workspace) canvas(c *ui.Context, tk tokens) {
	if tb := w.selected(); tb != nil {
		w.splitView(c, tk, tb.Root, true)
		return
	}
	ui.Column(c).Fill().Center().Children(func() {
		ui.Text(c, "No project").FontSize(13).TextColor(tk.MutedForeground)
	})
}

// sidebar lists projects, and under each one the sessions opened in that
// folder. A project is a place; a session is one piece of work there, with its
// own split tree of panes.
func (w *workspace) sidebar(c *ui.Context, tk tokens, width float32) {
	if width < 0.5 {
		return
	}
	bar := c.TitleBar()
	side := ui.Column(c).Width(width).FillHeight().Clip().Background(w.backdrop(tk))
	side.Opacity(side.Animate("focusDim", w.focusAmount(false), focusMotion)).
		Children(func() {
			ui.Row(c).Height(headerHeight).Padding(11, 8, 0, bar.Left+6+iconSize+8).
				Children(func() {
					ui.Row(c).FillWidth().Height(iconSize).AlignItems(ui.Center).Gap(2).Children(func() {
						w.dragArea(c, tk)
						iconButton(c, tk, "search", func() {
							w.searchOpen = !w.searchOpen
							if !w.searchOpen {
								w.searchQuery = ""
							}
							w.invalidate()
						})
						iconMenuButton(c, tk, "server", w.hostMenu)
						iconButton(c, tk, "plus", func() { w.pickProject() })
					})
				})
			if w.searchOpen {
				ui.Row(c).Padding(0, 8, 6, 8).Children(func() {
					ui.TextInput(c, &w.searchQuery).Placeholder("Search projects").
						FontSize(12).Height(26).FillWidth().AutoFocus()
				})
			}
			ui.Column(c).Fill().Padding(8).Gap(1).Children(func() {
				shown := false
				for _, pr := range w.Projects {
					if !w.projectVisible(pr) {
						continue
					}
					shown = true
					w.projectRow(c, tk, pr)
				}
				if !shown {
					ui.Text(c, "No projects").FontSize(12).
						TextColor(tk.MutedForeground).Padding(6, 8, 6, 8)
				}
				ui.Spacer(c)
			})
		})
}

// hostMenu lists the concrete hosts in ~/.ssh/config. The list is read as the
// menu opens, so a host added to the file shows up without a restart.
func (w *workspace) hostMenu(m *ui.Menu) {
	hosts := loadSSHHosts()
	if len(hosts) == 0 {
		m.Item("No hosts in ~/.ssh/config").Disabled(true)
		return
	}
	for _, h := range hosts {
		label := h.Alias
		if target := h.target(); target != label {
			label += "  " + target
		}
		if m.Item(label).Chosen() {
			w.connectHost(h)
		}
	}
}

func (w *workspace) sidebarToggle(c *ui.Context, tk tokens) {
	iconButton(c, tk, "panel-left", func() {
		w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
		w.settings.save()
		w.invalidate()
	})
}

// projectIcon marks where a project runs: a folder on this machine, or a
// server when it is a remote host.
func projectIcon(pr *project) string {
	if pr.RemoteHost != "" {
		return "server"
	}
	return "folder"
}

// projectRow is a folder header: it folds its sessions in and out, and offers
// another session or closing the project on hover.
func (w *workspace) projectRow(c *ui.Context, tk tokens, pr *project) {
	open := !w.collapsed[pr.ID] || strings.TrimSpace(w.searchQuery) != ""
	row := ui.Row(c).Height(rowHeightPx).Padding(0, 8, 0, 8).Gap(6).Radius(chromeRadius)
	hovered := row.Hovered()
	color := tk.MutedForeground
	if pr.sessionsSelected(w) || hovered {
		color = tk.Foreground
	}
	if hovered {
		row = row.Background(tk.Active)
	}
	toggle := func() { w.toggleProject(pr) }
	row.OnClick(toggle).Children(func() {
		chevron := "chevron-right"
		if open {
			chevron = "chevron-down"
		}
		if src := iconSource(chevron, colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(12).Height(12).OnClick(toggle)
		}
		if src := iconSource(projectIcon(pr), colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(13).Height(13).OnClick(toggle)
		}
		ui.Text(c, pr.name()).FontSize(12).FontWeight(500).SingleLine().TextColor(color).OnClick(toggle)
		ui.Spacer(c).OnClick(toggle)
		if hovered {
			iconButtonSmall(c, tk, "plus", func() { _, _ = w.newSession(pr, nil, pr.Dir) })
			iconButtonSmall(c, tk, "x", func() { w.closeProject(pr) })
		}
	})
	if !open {
		return
	}
	for _, s := range pr.Sessions {
		w.sessionRow(c, tk, pr, s)
	}
}

// sessionRow is one piece of work in a project: an agent's mark or a terminal
// glyph, its name, the activity loader, and a close button on hover.
func (w *workspace) sessionRow(c *ui.Context, tk tokens, pr *project, s *session) {
	sel := s.ID == w.SelectedSession
	color := tk.MutedForeground
	// Indented past the project's chevron and folder.
	row := ui.Row(c).Height(rowHeightPx).Padding(0, 8, 0, 26).Gap(8).Radius(chromeRadius)
	hovered := row.Hovered()
	if sel || hovered {
		color = tk.Foreground
	}
	row = row.Background(fade(c, tk, row, sel, hovered))
	pick := func() { w.selectSession(s.ID) }
	row.OnClick(pick).Children(func() {
		agent := s.agent()
		if agent != nil {
			if src := agentLogoSource(agent.Kind, tk); src != nil {
				ui.Image(c, src).Width(13).Height(13).OnClick(pick)
			}
		} else if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(13).Height(13).OnClick(pick)
		}
		ui.Text(c, sessionLabel(s)).FontSize(12).FontWeight(500).SingleLine().TextColor(color).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
		// Loader first, close last: the × owns the row's right edge whenever
		// the pointer is over it, and the loader sits beside it.
		w.activityLoader(c, tk, sessionActivityState(s), pick)
		if hovered {
			iconButtonSmall(c, tk, "x", func() { w.closeSession(s) })
		}
	})
}

// sessionLabel is what the sidebar calls a session: its agent when one runs
// there, the terminal title otherwise.
func sessionLabel(s *session) string {
	if agent := s.agent(); agent != nil {
		return agent.Kind.name()
	}
	return s.title()
}

// sessionsSelected reports whether the selection lives under the project.
func (p *project) sessionsSelected(w *workspace) bool {
	for _, s := range p.Sessions {
		if s.ID == w.SelectedSession {
			return true
		}
	}
	return false
}

// projectVisible matches the search box against the folder and its sessions.
func (w *workspace) projectVisible(pr *project) bool {
	q := strings.ToLower(strings.TrimSpace(w.searchQuery))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(pr.name()), q) ||
		strings.Contains(strings.ToLower(pr.Dir), q) {
		return true
	}
	for _, s := range pr.Sessions {
		if strings.Contains(strings.ToLower(sessionLabel(s)), q) {
			return true
		}
	}
	return false
}

// activityLoader is the sidebar's grid loader, after loaders.wtf: a short
// row of dots that lights up in sequence while an agent works. Waiting shows
// the same dots holding still in amber, so the two states differ by motion
// and colour, not shape.
func (w *workspace) activityLoader(c *ui.Context, tk tokens, state string, pick func()) {
	// The slot keeps its width when the loader is off, so nothing in the row
	// shifts as agents start and stop.
	box := ui.Row(c).Width(loaderWidth).Height(8).Gap(3).AlignItems(ui.Center)
	box.OnClick(pick).Children(func() {
		if state == "" {
			return
		}
		color := tk.Working
		if state == activityWaiting {
			color = tk.Waiting
		}
		phase := 0.0
		if state == activityWorking {
			phase = loaderPhase(c)
		}
		for i := range 3 {
			alpha := float32(1)
			if state == activityWorking {
				alpha = dotAlpha(phase, i)
			}
			ui.Column(c).Width(5).Height(5).Radius(1.5).Background(color).
				Opacity(alpha).OnClick(pick)
		}
	})
}

// loaderPhase advances the wave and keeps frames coming while it runs.
func loaderPhase(c *ui.Context) float64 {
	const period = 900 * time.Millisecond
	c.After(33 * time.Millisecond)
	return float64(time.Now().UnixMilli()%period.Milliseconds()) / float64(period.Milliseconds())
}

// dotAlpha gives each dot the same wave a third of a cycle apart, so the row
// reads as one pulse travelling right.
func dotAlpha(phase float64, i int) float32 {
	s := math.Sin(2 * math.Pi * (phase - float64(i)/3))
	return float32(0.15 + 0.85*(0.5+0.5*s))
}

// loaderWidth is three 5pt dots and the 3pt gaps between them.
const loaderWidth float32 = 21

// sessionActivityState folds the panes' hook events into one sidebar indicator.
func sessionActivityState(s *session) string {
	state := ""
	if s.Root == nil {
		return state
	}
	for _, p := range s.Root.panes() {
		if p.activity == nil {
			continue
		}
		switch p.activity.State {
		case activityWaiting:
			return activityWaiting
		case activityWorking:
			state = activityWorking
		}
	}
	return state
}

// splitView mirrors Gexi's recursive split tree. leading marks the child
// that touches the content's top-left corner: when the sidebar is collapsed,
// that one pane header makes room for the traffic lights and the toggle.
func (w *workspace) splitView(c *ui.Context, tk tokens, n *splitNode, leading bool) {
	if n == nil {
		return
	}
	if n.isLeaf() {
		w.paneView(c, tk, n.pane, leading)
		return
	}
	// Comfortable panes are cards with no seam between them: the gap is the
	// divider. mygo's split always draws a line, so this lays the cards out
	// by percentage instead.
	if w.settings.PaneStyle == paneComfortable {
		if n.axis == axisHorizontal {
			ui.Row(c).Fill().Children(func() {
				ui.Row(c).WidthPercent(n.ratio).FillHeight().
					Children(func() { w.splitView(c, tk, n.first, leading) })
				ui.Row(c).WidthPercent(100 - n.ratio).FillHeight().
					Children(func() { w.splitView(c, tk, n.second, false) })
			})
		} else {
			ui.Column(c).Fill().Children(func() {
				ui.Column(c).HeightPercent(n.ratio).FillWidth().
					Children(func() { w.splitView(c, tk, n.first, leading) })
				ui.Column(c).HeightPercent(100 - n.ratio).FillWidth().
					Children(func() { w.splitView(c, tk, n.second, false) })
			})
		}
		return
	}
	if n.axis == axisHorizontal {
		ui.SplitPercent(c, &n.ratio,
			func() { w.splitView(c, tk, n.first, leading) },
			func() { w.splitView(c, tk, n.second, false) },
		).Fill()
	} else {
		ui.SplitVerticalPercent(c, &n.ratio,
			func() { w.splitView(c, tk, n.first, leading) },
			func() { w.splitView(c, tk, n.second, false) },
		).Fill()
	}
}

func (w *workspace) paneView(c *ui.Context, tk tokens, p *pane, leading bool) {
	bar := c.TitleBar()
	focused := p.ID == w.FocusedPane
	underChrome := leading && w.settings.SidebarCollapsed

	card := w.settings.PaneStyle == paneComfortable
	outer := ui.Column(c).Fill()
	if card {
		outer = outer.Padding(3)
	}
	outer.Children(func() {
		col := ui.Column(c).Fill()
		col = col.Opacity(col.Animate("focusDim", w.focusAmount(focused), focusMotion) * fadeAlpha(c, p))
		if card {
			col = col.Radius(windowRadius).Border(1, tk.Border).Background(tk.Background).Clip()
		}
		col.Children(func() {
			left := float32(10)
			if underChrome {
				left = bar.Left + 6 + iconSize + 8
			}
			// Headers are always there: hiding them would leave the collapsed
			// sidebar toggle over bare terminal content.
			// No rule under the header: the pane card itself is the frame.
			header := ui.Row(c).Height(headerHeight).Padding(0, 6, 0, left).
				Background(tk.Background).ClipY()

			header.Children(func() {
				line := ui.Row(c).FillWidth().Height(chromeLine(bar)).AlignItems(ui.Center).Gap(4)
				line.Children(func() {
					// A detected agent wears its own mark and name. The
					// terminal title carries the agent's spinner glyph, and
					// the directory after it is noise beside the brand.
					agent := p.agent
					if agent != nil {
						if src := agentLogoSource(agent.Kind, tk); src != nil {
							ui.Image(c, src).Width(13).Height(13).DragWindow()
						}
					} else if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
						ui.Image(c, src).Width(13).Height(13).DragWindow()
					}
					title := p.title()
					if agent != nil {
						title = agent.Kind.name()
					}
					if p.exited {
						title += " · exited"
					}
					if focused {
						ui.Text(c, title).FontSize(12).FontWeight(500).
							TextColor(tk.Foreground).DragWindow()
					} else {
						ui.Text(c, title).FontSize(12).TextColor(tk.MutedForeground)
					}
					if agent == nil {
						ui.Text(c, abbreviate(p.dir())).FontSize(11).
							TextColor(tk.MutedForeground).DragWindow()
					}
					w.dragArea(c, tk)
					iconButton(c, tk, "columns-2", func() { _, _ = w.splitPane(p, axisHorizontal, nil, p.dir()) })
					iconButton(c, tk, "rows-2", func() { _, _ = w.splitPane(p, axisVertical, nil, p.dir()) })
					iconButton(c, tk, "x", func() { w.requestClose(p) })
				})
			})

			el := terminal.View(c, p.term).Fill()
			if el.Focused() && w.FocusedPane != p.ID {
				w.FocusedPane = p.ID
			}
			if w.focusReq == p.ID {
				el = el.Focus()
				w.focusReq = ""
			}
		})
	})
}

// fade returns shadcn's 150ms ease-out transition for a row background:
// filled when selected or hovered (the sidebar's own backdrop is Muted, so a
// Muted hover would disappear), transparent otherwise.
func fade(c *ui.Context, tk tokens, el ui.Element, active, hovered bool) ui.Color {
	target := ui.RGBA(0, 0, 0, 0)
	switch {
	case active:
		target = tk.Active
	case hovered:
		// The sidebar sits on tk.Muted, so a Muted hover would be invisible.
		target = tk.Active
	}
	return el.AnimateColor("bg", target, ui.Animation{Duration: 150 * time.Millisecond})
}

// fadeAlpha fades a pane in when it opens and out before it closes, keeping
// frames coming while it moves.
func fadeAlpha(c *ui.Context, p *pane) float32 {
	d := paneMotion
	if p.closing {
		t := float32(time.Since(p.closingAt)) / float32(d)
		if t >= 1 {
			return 0
		}
		c.After(16 * time.Millisecond)
		return 1 - t
	}
	if !p.openedAt.IsZero() {
		t := float32(time.Since(p.openedAt)) / float32(d)
		if t < 1 {
			c.After(16 * time.Millisecond)
			return t
		}
		p.openedAt = time.Time{}
	}
	return 1
}

// focusDim is how far everything but the focused pane recedes once a tab is
// split: other panes and the sidebar fall back so the pane being typed into
// is the only thing that reads at full strength.
const focusDim float32 = 0.55

// dimming reports whether the selected tab holds focus in one of several
// panes, which is when the rest of the window steps back.
func (w *workspace) dimming() bool {
	tb := w.selected()
	if tb == nil {
		return false
	}
	panes := tb.Root.panes()
	if len(panes) < 2 {
		return false
	}
	for _, p := range panes {
		if p.ID == w.FocusedPane {
			return true
		}
	}
	return false
}

// focusAmount is a part of the window's opacity: full unless another pane
// holds focus.
func (w *workspace) focusAmount(focused bool) float32 {
	if focused || !w.dimming() {
		return 1
	}
	return focusDim
}

func abbreviate(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home) {
		path = "~" + strings.TrimPrefix(path, home)
	}
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) > 4 {
		return "…/" + strings.Join(parts[len(parts)-3:], "/")
	}
	return path
}

// chromeLine is the height of the window controls' row: titles and buttons
// sit on that line, even though the header band is taller.
func chromeLine(bar ui.TitleBar) float32 {
	return 32
}
