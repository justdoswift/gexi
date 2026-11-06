package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// The layout follows Rex: the window has no title bar of its own, the traffic
// lights sit over the top-left corner, and a session sidebar or the top-left
// pane header takes that first row. The sidebar toggle never moves.
//
// The chrome itself follows shadcn: one neutral palette, 1px borders, 6px
// radius, quiet hover states.

func (w *workspace) view(c *ui.Context) {
	if w.pendingToast != "" {
		if w.pendingToastAction != "" && w.pendingAction != nil {
			c.ToastAction(w.pendingToast, w.pendingToastAction, w.pendingAction)
		} else {
			c.Toast(w.pendingToast)
		}
		w.pendingToast, w.pendingToastAction, w.pendingAction = "", "", nil
	}
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
		// The toggle sits beside the traffic lights and never moves, so
		// collapsing or showing the sidebar leaves it where the hand expects
		// it.
		ui.Row(c).Width(iconSize).Height(iconSize).Absolute().
			Left(bar.Left + 6).Top(11).Children(func() {
			w.sidebarToggle(c, tk)
		})
		// The drawer sits above the panes and below nothing: it is the last
		// thing drawn, so it slides over the canvas rather than resizing it.
		w.drawerView(c, tk)
	})
	ui.Modal(c, &w.relayOpen, func() { w.relayDialog(c, tk) })
	ui.Modal(c, &w.importOpen, func() { w.importDialog(c, tk) })
	ui.Modal(c, &w.commitOpen, func() { w.commitDialog(c, tk) })
	if w.leaveTarget != nil {
		switch ui.AlertDialog(c, &w.leaveOpen, locales.WorktreeLeaveTitle(), w.leaveMsg,
			locales.WorktreeLeaveConfirm(), locales.Cancel()) {
		case 0:
			p := w.leaveTarget
			w.leaveTarget = nil
			w.leaveWorktree(p)
		case 1, ui.AlertDismissed:
			w.leaveTarget = nil
		}
	}
}

// dragArea is a draggable stretch of a header that also zooms the window on
// a double click. A Row centers its items, so a bare Spacer would be no
// taller than its empty content and take no press: stretching it fills the
// row's height.
func (w *workspace) dragArea(c *ui.Context, tk tokens) ui.Element {
	return ui.Box(c).Grow(1).AlignSelf(ui.Stretch).DragWindow()
}

func (w *workspace) backdrop(tk tokens) ui.Color {
	if w.settings.PaneStyle == paneComfortable {
		return tk.Muted
	}
	return tk.Background
}

func (w *workspace) canvas(c *ui.Context, tk tokens) {
	if tb := w.selected(); tb != nil {
		// A zoomed pane fills the session on its own: its tree is still
		// there, so putting the others back is only a matter of drawing it.
		// The layout changes in one frame, so the new one settles in with the
		// same fade and scale a pane opens with.
		// The stage grows into the new layout: no fade, which reads as a
		// flash over a grid that is resizing at the same moment, but a scale
		// big enough to see the pane settle into its new bounds.
		pose := ui.Pose{}
		if w.zoomSettling {
			w.zoomSettling = false
			pose = ui.Pose{Scale: 0.93}
			c.After(time.Millisecond)
		}
		stage := ui.Column(c).Fill()
		stage = stage.AnimatePose(pose, ui.Animation{Duration: paneMotion, Ease: ui.EaseOut})
		stage.Children(func() {
			if p := w.zoomedIn(tb); p != nil {
				w.paneView(c, tk, p, true)
				return
			}
			w.splitView(c, tk, tb.Root, true)
		})
		return
	}
	// The empty workspace is a session's card with nothing in it: no
	// project, or a project with no session, still reads as a place waiting
	// for work rather than a blank window.
	outer := ui.Column(c).Fill()
	if w.settings.PaneStyle == paneComfortable {
		outer = outer.Padding(3)
	}
	outer.Children(func() {
		col := ui.Column(c).Fill()
		if w.settings.PaneStyle == paneComfortable {
			col = col.Radius(windowRadius).Border(1, tk.Border).Background(tk.Background).Clip()
		}
		col.Children(func() {
			ui.Row(c).Height(headerHeight).Background(tk.Background).DragWindow()
			ui.Column(c).Fill().Center().Children(func() {
				ui.Text(c, locales.NoProject()).FontSize(13).TextColor(tk.MutedForeground)
			})
		})
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
	side.Children(func() {
		ui.Row(c).Height(headerHeight).Padding(11, 8, 0, bar.Left+6+iconSize+8).
			DragWindow().Children(func() {
			ui.Row(c).FillWidth().Height(iconSize).AlignItems(ui.Center).Children(func() {
				w.dragArea(c, tk)
			})
		})
		// The search field and the new-project button share the first row
		// under the header, so the title bar keeps only the drag area.
		ui.Row(c).Height(searchHeight).Padding(8, 8, 0, 8).Gap(6).Children(func() {
			w.projectSearch(c, tk)
			iconButtonSized(c, tk, "plus", 24, 19, func() { w.pickProject() },
				locales.ShortcutNewProject()+" (⌘⇧N)")
		})
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
				ui.Text(c, locales.NoProjects()).FontSize(12).
					TextColor(tk.MutedForeground).Padding(6, 8, 6, 8)
			}
			ui.Spacer(c)
			// The footer holds the sidebar's two menus: remote Macs on
			// the left, settings on the right, off the search row so the
			// field can run nearly as wide as the sessions.
			ui.Row(c).Height(iconSize).Gap(2).Children(func() {
				iconMenuButton(c, tk, "server", w.remoteMenu, locales.RemoteMenuHint())
				ui.Spacer(c)
				iconButton(c, tk, "setting", func() { w.openSettings() },
					locales.ShortcutSettings()+" (⌘,)")
			})
		})
	})
}

// projectSearch is the field that filters the project list. It is a pill of
// the list's own size and tint, so it reads as the first row of the list.
func (w *workspace) projectSearch(c *ui.Context, tk tokens) {
	field := ui.Row(c).Height(searchHeight).FillWidth().AlignItems(ui.Center).Gap(6).
		PaddingStart(9).PaddingEnd(8).Radius(chromeRadius).
		Background(tk.Active).Border(1, ui.RGBA(0, 0, 0, 0)).Cursor(ui.CursorText)
	var in ui.Element
	field.OnClick(func() { in.Focus() }).Children(func() {
		if src := iconSource("search", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(16).Height(16).IgnoreInput(true)
		}
		in = ui.TextInputBase(c, &w.searchQuery).Label(locales.SearchProjects()).
			Placeholder(locales.SearchPlaceholder()).PlaceholderColor(tk.MutedForeground).
			FontSize(11).FillWidth()
	})
	// The pill lights its border while the query has the focus; the input
	// itself draws no chrome, so this is the only focus cue it needs.
	if in.Focused() {
		field.BorderColor(tk.MutedForeground)
	}
}

// remoteMenu is the whole Remote row: whether this Mac can be reached, the
// Macs it already reaches, and the one-line import. A host list is read as
// the menu opens, so ~/.ssh/config edits show up without a restart.
func (w *workspace) remoteMenu(m *ui.Menu) {
	if w.settings.RemoteListen {
		state := "tunnel reconnecting"
		if w.tunnelUp {
			state = "reachable"
		}
		m.Item(fmt.Sprintf("%s · %s", w.settings.RemoteName, state)).Disabled(true)
		if m.Item("Copy connection code").Chosen() {
			if code, err := w.remoteCodeValue(); err == nil {
				writeClipboard(code)
			}
		}
		if m.Item("Turn off remote access").Chosen() {
			w.disableRemote()
			w.invalidate()
		}
	} else if m.Item("Allow this Mac to be reached…").Chosen() {
		w.openRelayDialog()
	}
	m.Separator()

	for _, h := range w.settings.ImportedHosts {
		if m.Item(h.Name + "  " + h.Relay).Chosen() {
			w.connectRemote(h)
		}
	}
	for _, h := range loadSSHHosts() {
		label := h.Alias
		if target := h.target(); target != label {
			label += "  " + target
		}
		if m.Item(label).Chosen() {
			w.connectHost(h)
		}
	}
	if len(w.settings.ImportedHosts) == 0 && len(loadSSHHosts()) == 0 {
		m.Item("No other Mac yet").Disabled(true)
	}
	m.Separator()
	if m.Item("Connect to another Mac…").Chosen() {
		w.openImportDialog()
	}
}

// relayDialog is the one-time setup for a Mac that should be reachable.
func (w *workspace) relayDialog(c *ui.Context, tk tokens) {
	ui.Column(c).Width(400).Padding(20).Gap(8).Children(func() {
		ui.Text(c, locales.RelayTitle()).FontSize(14).FontWeight(600).TextColor(tk.Foreground)
		ui.Text(c, locales.RelayBody()).
			FontSize(11).TextColor(tk.MutedForeground)
		ui.Text(c, locales.RelayHost()).FontSize(11).TextColor(tk.MutedForeground).Padding(6, 0, 0, 0)
		ui.TextInput(c, &w.relayInput).Placeholder(locales.RelayHostPlaceholder()).Height(28).FontSize(12)
		ui.Text(c, locales.RelayName()).FontSize(11).TextColor(tk.MutedForeground).Padding(6, 0, 0, 0)
		ui.TextInput(c, &w.relayName).Placeholder(locales.RelayNamePlaceholder()).Height(28).FontSize(12)
		if w.relayErr != "" {
			ui.Text(c, w.relayErr).FontSize(11).TextColor(tk.Waiting)
		}
		ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			ui.Button(c, locales.Cancel()).OnClick(func() {
				w.relayOpen = false
				w.invalidate()
			})
			label := locales.TurnOn()
			if w.relayBusy {
				label = locales.Checking()
			}
			ui.Button(c, label).OnClick(func() { w.submitRelay() })
		})
	})
}

// importDialog takes the code from the other Mac. It starts from the
// clipboard, so the usual path is open, connect.
func (w *workspace) importDialog(c *ui.Context, tk tokens) {
	ui.Column(c).Width(440).Padding(20).Gap(8).Children(func() {
		ui.Text(c, locales.ImportTitle()).FontSize(14).FontWeight(600).TextColor(tk.Foreground)
		ui.Text(c, locales.ImportBody()).
			FontSize(11).TextColor(tk.MutedForeground)
		ui.TextInput(c, &w.importText).Height(28).FontSize(12).Placeholder("gexi1.…")
		if w.importErr != "" {
			ui.Text(c, w.importErr).FontSize(11).TextColor(tk.Waiting)
		}
		ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			ui.Button(c, locales.Cancel()).OnClick(func() {
				w.importOpen = false
				w.invalidate()
			})
			ui.Button(c, locales.Connect()).OnClick(func() { w.submitImport() })
		})
	})
}

func (w *workspace) sidebarToggle(c *ui.Context, tk tokens) {
	iconButton(c, tk, "panel-left", func() {
		w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
		w.settings.save()
		w.invalidate()
	}, locales.ShortcutToggleSidebar()+" (⌘B)")
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
			ui.Image(c, src).Width(15).Height(15).OnClick(toggle)
		}
		if src := iconSource(projectIcon(pr), colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(16).Height(16).OnClick(toggle)
		}
		ui.Text(c, pr.name()).FontSize(12).FontWeight(500).SingleLine().TextColor(color).OnClick(toggle)
		ui.Spacer(c).OnClick(toggle)
		if hovered {
			iconButtonSmall(c, tk, "plus", func() { _, _ = w.newSession(pr, nil, pr.Dir) },
				locales.ShortcutNewSession()+" (⌘T)")
			iconButtonSmall(c, tk, "x", func() { w.closeProject(pr) }, locales.CloseProject())
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
	// Flush with the project row: the session card reads as its own row, not
	// as something hanging under an indent.
	row := ui.Row(c).Height(rowHeightPx).Padding(0, 8, 0, 8).Gap(8).Radius(chromeRadius)
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
			ui.Image(c, src).Width(16).Height(16).OnClick(pick)
		}
		sessionLabelElement(c, tk, s, color, pick)
		ui.Spacer(c).OnClick(pick)
		if hovered {
			iconButtonSmall(c, tk, "x", func() { w.closeSession(s) }, locales.CloseSession())
		}
	})
}

// sessionLabel is what the sidebar calls a session: its agent when one runs
// there, the terminal title otherwise.
func sessionLabel(s *session) string {
	if s.Root != nil {
		for _, p := range s.Root.panes() {
			if p.Worktree != nil && p.Worktree.Branch != "" {
				return p.Worktree.Branch
			}
		}
	}
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

// splitView mirrors gexi's recursive split tree. leading marks the child
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

// zoomShortcut is the accelerator the menu and the pane's zoom button show,
// written as macOS writes it on a Mac keyboard.
const zoomShortcut = "⇧⌘↩"

func (w *workspace) paneView(c *ui.Context, tk tokens, p *pane, leading bool) {
	bar := c.TitleBar()
	focused := p.ID == w.FocusedPane
	underChrome := leading && w.settings.SidebarCollapsed

	card := w.settings.PaneStyle == paneComfortable
	outer := ui.Column(c).Fill()
	if card {
		outer = outer.Padding(3)
	}
	// A pane takes another pane dropped on it: the side it lands on splits
	// toward that side, the middle trades the two. The hint is read before
	// the drop, which clears the drag, so both read the pointer directly.
	moved, over := ui.DragOver[paneMoved](outer)
	place := paneDropNone
	if over {
		place = dropPlaceOf(outer, moved.Pane, p)
	}
	if v, dropped := ui.Drop[paneMoved](outer); dropped {
		if at := dropPlaceOf(outer, v.Pane, p); at != paneDropNone {
			w.movePane(v.Pane, p, at)
			place = paneDropNone
		}
	}
	outer.Children(func() {
		col := ui.Column(c).Fill()
		dim := col.Animate("focusDim", w.focusAmount(focused), focusMotion)
		// The card fades and grows in as it opens, and shrinks out before it
		// closes: a fade alone is easy to miss on a white card over a light
		// canvas. Poses run in frames that only paint, without building the
		// view, which keeps a terminal redrawing its grid from stuttering.
		pose := ui.Pose{}
		if p.closing {
			pose = ui.Pose{Fade: 1, Scale: 0.98}
		} else if !p.shown {
			p.shown = true
			pose = ui.Pose{Fade: 1, Scale: 0.97}
			c.After(time.Millisecond)
		}
		col = col.AnimatePose(pose, ui.Animation{Duration: paneMotion, Ease: ui.EaseOut})
		if card {
			col = col.Radius(windowRadius).Border(1, tk.Border).Background(tk.Background).Clip()
		}
		dragging := false
		blend := ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut}
		panelBg := tk.Background
		col.Children(func() {
			left := float32(10)
			if underChrome {
				left = bar.Left + 6 + iconSize + 8
			}
			// Headers are always there: hiding them would leave the collapsed
			// sidebar toggle over bare terminal content.
			// No rule under the header: the pane card itself is the frame.
			header := ui.Row(c).Height(headerHeight).Padding(0, 6, 0, left).
				Background(tk.Background).ClipY().DragWindow()
			// Split and close belong to the pane the hand is on: they appear
			// wherever the pointer is over the pane, its terminal included,
			// or when the pane has the focus, so an idle layout is only the
			// terminal and its title.
			showActions := focused || col.Hovered()

			header.Children(func() {
				line := ui.Row(c).FillWidth().Height(chromeLine(bar)).AlignItems(ui.Center).Gap(4)
				// The header drops what a narrow pane cannot show before it
				// lets anything overlap: the folder first, then the branch
				// pill. The width is the one it had in the frame before this,
				// which is exact until the pane or its split is resized and
				// settles one frame later.
				width := line.Bounds().W
				line.Children(func() {
					// The title picks the pane up once there is a second pane
					// to trade with. Alone in the window it keeps dragging the
					// window, as a title bar does, since there is nowhere to
					// put it.
					// The pill sits inside the header band rather than filling
					// it, so hovering reads as a chip and not as a second bar.
					movable := len(w.visiblePanes()) > 1
					// The title gives way before the branch pill does: its
					// texts end in an ellipsis rather than wrapping, and the
					// pill keeps its width.
					// A sliver of a pane spends nothing on padding it does
					// not need: a couple of points buy its title back.
					stripPad := float32(6)
					if width > 0 && width < 200 {
						stripPad = 3
					}
					strip := ui.Row(c).Height(24).AlignItems(ui.Center).Shrink(1).
						Gap(4).Padding(0, stripPad).Radius(chromeRadius)
					if movable {
						strip = strip.Drag(paneMoved{Pane: p}).Cursor(ui.CursorGrab).
							Tooltip(locales.PaneDragHint())
						dragging = strip.Dragging()
					} else {
						strip = strip.DragWindow()
					}
					if strip.Hovered() {
						strip = strip.Background(tk.Active)
					}
					strip.Children(func() {
						// A detected agent wears its own mark and name. The
						// terminal title carries the agent's spinner glyph, and
						// the directory after it is noise beside the brand.
						agent := p.agent
						// The title matters more than its mark once the pane
						// is a sliver: the name stays, the glyph goes.
						showMark := width == 0 || width >= 160
						if agent != nil {
							if src := agentLogoSource(agent.Kind, tk); src != nil && showMark {
								ui.Image(c, src).Width(13).Height(13)
							}
						} else if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil && showMark {
							ui.Image(c, src).Width(16).Height(16)
						}
						title := p.title()
						if agent != nil {
							title = agent.Kind.name()
						}
						if p.exited {
							title += " · exited"
						}
						// The title gives way only when the close button
						// would otherwise be clipped.
						titleShrink := float32(0)
						if width > 0 && width < 200 {
							titleShrink = 1
						}
						if focused {
							ui.Text(c, title).FontSize(12).FontWeight(500).SingleLine().
								Shrink(titleShrink).TextColor(tk.Foreground)
						} else {
							ui.Text(c, title).FontSize(12).SingleLine().
								Shrink(titleShrink).TextColor(tk.MutedForeground)
						}
						if agent == nil && (width == 0 || width >= 540) {
							ui.Text(c, abbreviate(p.dir())).FontSize(11).SingleLine().
								Shrink(1).TextColor(tk.MutedForeground)
						}
					})
					// The branch pill follows the title: it names the branch
					// the project is on, counts the working tree's changes,
					// and opens their diff. A pane too narrow for it keeps
					// only its title; the drawer still opens from the menu.
					if width == 0 || width >= 320 {
						w.branchPill(c, tk, p)
					}
					w.dragArea(c, tk)
					// The buttons keep their room and fade, so the row does
					// not jump as the pointer comes and goes; they take no
					// click while invisible.
					alpha := float32(0)
					if showActions {
						alpha = 1
					}
					actions := ui.Row(c).Gap(2).Height(chromeLine(bar)).AlignItems(ui.Center).Shrink(0)
					actions = actions.IgnoreInput(!showActions).
						Opacity(actions.AnimateTo("actions", alpha, ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseOut}))
					actions.Children(func() {
						w.paneWorktreeButton(c, tk, p)
						// A pane too narrow for all three keeps only the way
						// out: splitting and zooming are in the View menu, and
						// a clipped button is worse than an absent one.
						if width == 0 || width >= 380 {
							iconButton(c, tk, "columns-2", func() { _, _ = w.splitPane(p, axisHorizontal, nil, p.dir()) },
								locales.MenuSplitRight()+" (⌘D)")
							iconButton(c, tk, "rows-2", func() { _, _ = w.splitPane(p, axisVertical, nil, p.dir()) },
								locales.MenuSplitDown()+" (⌘⇧D)")
							// A pane with nowhere to go has nothing to zoom to.
							if len(w.visiblePanes()) > 1 || w.zoomedPane == p.ID {
								name, hint := "expand", locales.PaneZoom()+" ("+zoomShortcut+")"
								if w.zoomedPane == p.ID {
									name, hint = "collapse", locales.PaneUnzoom()+" ("+zoomShortcut+")"
								}
								iconButton(c, tk, name, func() { w.toggleZoom(p) }, hint)
							}
						}
						iconButton(c, tk, "x", func() { w.requestClose(p) },
							locales.MenuClosePane()+" (⌘W)")
					})
				})
			})
			// A raised pane takes the canvas colour, so the slot reads as
			// empty behind the chip the pointer carries.
			if dragging {
				panelBg = w.backdrop(tk)
			}
			header.Background(header.AnimateColor("paneLiftBg", panelBg, blend))

			el := terminal.View(c, p.term).Fill()
			if el.Focused() && w.FocusedPane != p.ID {
				w.FocusedPane = p.ID
			}
			if w.focusReq == p.ID {
				el = el.Focus()
				w.focusReq = ""
			}
			if place != paneDropNone {
				paneDropHint(c, place, card)
			}
		})
		// Picking the pane up sets it back a touch and fills its surface with
		// the canvas colour, so the slot it would leave reads as empty. The
		// change eases in, and the title chip stays what the pointer carries.
		liftTarget := float32(0)
		if dragging {
			liftTarget = 1
		}
		lift := col.AnimateTo("paneLift", liftTarget, blend)
		if lift > 0 {
			s := 1 - 0.025*lift
			col = col.Scale(s, s)
		}
		col = col.Background(col.AnimateColor("paneLiftBg", panelBg, blend))
		col = col.Opacity(dim)
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

// focusDim is how far the other panes recede once a tab is split, so the pane
// being typed into is the only one that reads at full strength. The sidebar
// keeps its own strength: it is navigation, not a peer of the panes.
const focusDim float32 = 0.55

// dimming reports whether the selected tab holds focus in one of several
// panes, which is when the other panes step back.
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

// focusAmount is a part of a pane's opacity: full unless another pane holds
// focus in a split.
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
