package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/loading"
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

// sidebarSlide is how much of the sidebar shows, from 0 to 1, with the motion
// running from where it was to where it is going.
type sidebarSlide struct {
	set   bool
	value float32
	from  float32
	to    float32
	at    time.Time
}

// sidebarFraction moves the sidebar to to, over the same motion the drawer
// slides with. The value is kept here rather than in a toolkit animation
// channel: a channel that is lost for a frame reports its target, which made
// the panes flash wide while the sidebar closed. Main thread.
func (w *workspace) sidebarFraction(c *ui.Context, to float32) float32 {
	s := &w.sidebarPos
	if !s.set {
		s.set, s.value, s.from, s.to, s.at = true, to, to, to, time.Now()
		return to
	}
	if to != s.to {
		s.from, s.at, s.to = s.value, time.Now(), to
	}
	p := float32(time.Since(s.at)) / float32(sidebarMotion)
	if p >= 1 {
		s.value = s.to
		return s.value
	}
	s.value = s.from + (s.to-s.from)*ui.EaseOut(p)
	c.After(8 * time.Millisecond)
	return s.value
}

// appearanceMotion is how long a change of appearance or material takes to
// dissolve the page the window was showing into the one it now has.
const appearanceMotion = 180 * time.Millisecond

// appearanceFade is the look last drawn and the dissolve running from it.
type appearanceFade struct {
	set   bool
	seen  tokens
	glass bool
	fade  bool
	from  ui.Color
	at    time.Time
	alpha float32
}

// trackAppearance notices a change of theme or of the glass material and
// starts the dissolve, from the page colour the eye was on rather than the one
// it is going to. Main thread.
func (w *workspace) trackAppearance(c *ui.Context, tk tokens) {
	a := &w.appear
	if !a.set {
		a.seen, a.glass, a.set = tk, glassChrome(), true
		return
	}
	if tk != a.seen || glassChrome() != a.glass {
		if !a.fade {
			a.from = a.seen.Background
			a.at = time.Now()
			a.fade = true
		}
		a.seen, a.glass = tk, glassChrome()
	}
	if !a.fade {
		return
	}
	p := float32(time.Since(a.at)) / float32(appearanceMotion)
	if p >= 1 {
		a.fade, a.alpha = false, 0
		return
	}
	// Fast at first, so the old page lifts away rather than lingering.
	a.alpha = (1 - p) * (1 - p)
	c.After(8 * time.Millisecond)
}

// appearanceScrim is the page the window was showing, laid over the one it
// now has while the dissolve runs. It takes no input: the window underneath
// is already the new one.
func (w *workspace) appearanceScrim(c *ui.Context) {
	a := &w.appear
	if !a.fade || a.alpha <= 0.01 {
		return
	}
	ui.Box(c).Absolute().Left(0).Top(0).WidthPercent(100).HeightPercent(100).
		Background(a.from.Alpha(a.alpha)).IgnoreInput(true)
}

func (w *workspace) view(c *ui.Context) {
	w.applyInterfaceFont(c)
	if w.pendingToast != "" {
		if w.pendingToastAction != "" && w.pendingAction != nil {
			c.ToastAction(w.pendingToast, w.pendingToastAction, w.pendingAction)
		} else {
			c.Toast(w.pendingToast)
		}
		w.pendingToast, w.pendingToastAction, w.pendingAction = "", "", nil
	}
	tk := w.tokens(c)
	w.trackAppearance(c, tk)
	// The window's material shows where nothing is painted: the toolkit's
	// root would cover it with the theme's background, so it is cleared
	// first and the chrome's own sheet is what sits over the glass.
	if liquid(c) {
		c.Root().Background(ui.Transparent)
	}
	bar := c.TitleBar()
	// The sidebar and the canvas share one surface: the root paints the
	// theme's backdrop, and over the window's material it lets a little of
	// the glass through. Both sides then read as the same wrap around the
	// pane cards, instead of the sidebar's material against a painted canvas.
	//
	// A themed window takes the same glass as the neutral one. The material
	// stays underneath; the theme's colour is a tint over it, not a painted
	// sheet. In light comfortable the tint turns the sidebar's slightly
	// deeper paper into the page's colour toward the bottom, so the window
	// has a quiet vertical light even when the desktop behind it is plain.
	root := ui.Row(c).Fill()
	if liquid(c) {
		// The theme's paper, not a clear pane: the glass density setting
		// decides how much of the desktop shows through the sidebar, while
		// the terminal card below adds its own sheet so the text keeps the
		// theme's contrast.
		tint := float32(glassTintPercent(w.settings.GlassTint)) / 100
		top := tk.Background.Alpha(tint)
		if !tk.Dark && w.settings.PaneStyle == paneComfortable {
			// The light comfortable canvas starts one step deeper than the
			// page and rises to it, so the sidebar carries the window's
			// vertical light. A dark theme keeps its own canvas flat.
			top = tk.Muted.Alpha(tint)
		}
		root = root.Gradient(top, tk.Background.Alpha(tint), 180)
	} else {
		root = root.Background(w.backdrop(tk))
	}

	shown := float32(0)
	if !w.settings.SidebarCollapsed {
		shown = 1
	}
	width := w.sidebarFraction(c, shown) * sidebarWidth

	root.Children(func() {
		w.sidebar(c, tk, width)
		// The sidebar and the space around the panes share one background,
		// so in comfortable a pane reads as a panel wrapped around its
		// content. The canvas keeps an extra inset from the top, right, and
		// bottom edges; while the sidebar shows, its own margin is the left
		// one, and with the sidebar hidden the canvas takes the same margin
		// on that side too, so a pane never sits on the window's edge.
		inset := float32(0)
		if w.settings.PaneStyle == paneComfortable {
			inset = canvasInset
		}
		// The left margin is what the sidebar leaves behind as it slides
		// out, so one motion carries both and the pane never sits on the
		// window's edge.
		left := inset * (1 - width/sidebarWidth)
		// The canvas leaves its surface to the root, so the sidebar and the
		// pane cards sit on the same backdrop. A compact pane fills the
		// canvas with its terminal, and its text keeps the theme's colour
		// there rather than the glass.
		// The canvas keeps its identity when a sibling comes or goes: the
		// sidebar builds no element once it is narrower than a hair, and an
		// index-based identity would make the whole canvas a new element for
		// a frame — drawn as a ghost of itself, fading in over the old one.
		c.Key("canvas")
		canvas := ui.Column(c).Fill().Padding(inset, inset, inset, left)
		if w.settings.PaneStyle != paneComfortable {
			canvas = canvas.Background(w.backdrop(tk))
		}
		canvas.Children(func() {
			w.canvas(c, tk)
		})
		// The toggle sits beside the traffic lights and never moves, so
		// collapsing or showing the sidebar leaves it where the hand expects
		// it.
		ui.Row(c).Width(iconSize).Height(iconSize).Absolute().
			Left(bar.Left + 6).Top(16).Children(func() {
			w.sidebarToggle(c, tk)
		})
		// The drawer sits above the panes and below nothing: it is the last
		// thing drawn, so it slides over the canvas rather than resizing it.
		w.drawerView(c, tk)
		// The reply reader is the same kind of column, and only one of the
		// two is ever open.
		w.replyView(c, tk)
		// The dissolve of an appearance change covers everything drawn, so
		// a snap of the terminals comes with it.
		w.appearanceScrim(c)
	})
	if w.hostOpen {
		w.hostDialog(c, tk)
	}
	if w.searchOpen {
		w.searchDialog(c, tk)
	}
	if w.relayOpen {
		w.relayDialog(c, tk)
	}
	if w.commitOpen {
		w.commitDialog(c, tk)
	}
	if w.worktreeCreateOpen {
		w.worktreeCreateDialog(c, tk)
	}
	if w.renameOpen {
		w.renameBranchDialog(c, tk)
	}
	if w.sidebarRenameOpen {
		w.sidebarRenameDialog(c, tk)
	}
	if w.paneRenameOpen {
		w.paneRenameDialog(c, tk)
	}
	if w.closeTarget != nil {
		p := w.closeTarget
		dialogAlert(c, tk, &w.closeOpen, 420, locales.PaneCloseTitle(), w.closeMsg,
			dialogAction{label: locales.Cancel(), kind: dialogQuiet, run: func() {
				w.closeOpen = false
			}},
			dialogAction{label: locales.MenuClosePane(), kind: dialogDanger, run: func() {
				w.closeOpen = false
				if w.paneByID(p.ID) == p {
					w.requestClose(p)
				}
			}},
			dialogAction{label: locales.PaneCloseBackground(), kind: dialogPrimary, auto: true, run: func() {
				w.closeOpen = false
				if w.paneByID(p.ID) == p {
					w.detachPane(p)
				}
			}},
		)
		if !w.closeOpen {
			w.closeTarget = nil
		}
	}
	if w.closeProjectTarget != nil {
		w.closeProjectDialog(c, tk)
	}
	if w.leaveTarget != nil {
		p := w.leaveTarget
		dialogAlert(c, tk, &w.leaveOpen, 420, locales.WorktreeLeaveTitle(), w.leaveMsg,
			dialogAction{label: locales.Cancel(), kind: dialogQuiet, auto: true, run: func() {
				w.leaveOpen = false
			}},
			dialogAction{label: locales.WorktreeLeaveConfirm(), kind: dialogDanger, run: func() {
				w.leaveOpen = false
				w.leaveConfirmed(p)
			}},
		)
		if !w.leaveOpen {
			w.leaveTarget = nil
		}
	}
	if w.remoteLeaveTarget != nil {
		p := w.remoteLeaveTarget
		dialogAlert(c, tk, &w.remoteLeaveOpen, 420, locales.WorktreeLeaveTitle(), w.remoteLeaveMsg,
			dialogAction{label: locales.Cancel(), kind: dialogQuiet, auto: true, run: func() {
				w.remoteLeaveOpen = false
			}},
			dialogAction{label: locales.WorktreeLeaveConfirm(), kind: dialogDanger, run: func() {
				w.remoteLeaveOpen = false
				if p.remote != nil {
					p.remote.conn.paneWorktree(p, "leaveConfirm")
				}
			}},
		)
		if !w.remoteLeaveOpen {
			w.remoteLeaveTarget = nil
		}
	}
	if w.archiveTarget != nil {
		w.archiveWorktreeDialog(c, tk)
		if !w.archiveOpen {
			w.archiveTarget = nil
		}
	}
	if w.flowTarget != nil {
		w.projectFlowDialog(c, tk)
		if !w.flowOpen {
			w.flowTarget = nil
			w.flowDraft = flowDraft{}
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
		// A zoom changes the layout in one frame, so the stage shows small
		// on that frame and grows back to its size: the pane reads as
		// growing into the window rather than the grid jumping over.
		scaleTo := float32(1)
		if w.zoomSettling {
			w.zoomSettling = false
			w.zoomGen++
			scaleTo = 0.93
		}
		// A session of its own is another layout: the one replacing another
		// comes in while that one fades out, so the switch reads as a change
		// of view instead of a flash of the next tree.
		stage := ui.Column(c.Key(tb.ID)).Fill().Transition(ui.ElementTransition{
			Duration: 160 * time.Millisecond,
			Ease:     ui.EaseOut,
			Enter:    &ui.Motion{Opacity: 0, Scale: 0.99},
			Exit:     &ui.Motion{Opacity: 0},
		})
		// The scale is its own animation, keyed by the zoom, so each zoom
		// starts it afresh instead of gliding from where the last one ended.
		scale := stage.Animate(w.zoomGen, scaleTo, paneMotion)
		stage = stage.Scale(scale, scale)
		stage.Children(func() {
			if p := w.zoomedIn(tb); p != nil {
				w.paneView(c, tk, p, true)
				return
			}
			w.splitView(c, tk, tb.Root, true)
		})
		// The panes the zoom hides float over the stage's right edge, each
		// showing a live mirror of its terminal. They take no layout: the
		// stage keeps its size and the cards draw over its last columns.
		b := stage.LayoutBounds()
		w.stageW = b.W
		if w.zoomedIn(tb) != nil && w.stageRailVisible(tb) {
			w.stageOverlay(c, tk, tb, b.H)
		}
		return
	}
	// The empty workspace is a session's card with nothing in it: no
	// project, or a project with no session, still reads as a place waiting
	// for work rather than a blank window.
	outer := ui.Column(c).Fill()
	if w.settings.PaneStyle == paneComfortable {
		outer = outer.Padding(6)
	}
	outer.Children(func() {
		col := ui.Column(c).Fill()
		// An empty session is a pane card like the ones that hold terminals:
		// the theme's own sheet, a hairline, and the card's shadow.
		if w.settings.PaneStyle == paneComfortable {
			col = col.Radius(windowRadius).Clip()
			if liquid(c) {
				top, bottom := paneSurface(tk)
				col = col.Gradient(top, bottom, 180)
			} else {
				col = col.Background(tk.Background)
			}
			col = raised(col, tk)
		}
		headerBg := tk.Background
		if liquid(c) {
			headerBg = ui.Transparent
		}
		col.Children(func() {
			lit(ui.Row(c).Height(headerHeight).Background(headerBg).DragWindow(), tk)
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
	c.Key("sidebar")
	// The sidebar paints no background of its own: the root's surface runs
	// under it and under the canvas, so the two sides are one backdrop.
	// Collapsed, it takes no width and the canvas covers the surface.
	side := ui.Column(c).Width(width).FillHeight().Clip().Shrink(0)
	side.Children(func() {
		w.sidebarHeader(c, tk)
		// MinHeight(0) lets the body give way under the header: its own
		// content never has to fit without the scroll, which is what the
		// scroll is for.
		ui.Column(c).Fill().MinHeight(0).Padding(10, 6, 8, 8).Gap(2).Children(func() {
			// Projects and the idle fold scroll together, as one list:
			// a project with many sessions, or the fold opened over them,
			// would otherwise run past the window's bottom edge, where
			// the rows can no longer be reached. The footer stays under
			// the list, outside the scroll.
			list := ui.Scroll(c).Fill()
			list.Children(func() {
				col := ui.Column(c).FillWidth().Gap(2)
				// The list is at least as tall as its window, so the
				// spacer below the projects still holds the idle fold at
				// the bottom while everything fits.
				if w.sidebarListH > 0 {
					col = col.MinHeight(w.sidebarListH)
				}
				col.Children(func() {
					rows := w.sidebarProjectRows()
					if len(rows) == 0 {
						ui.Text(c, locales.NoProjects()).FontSize(12).
							TextColor(tk.MutedForeground).Padding(6, 8, 6, 8)
					} else {
						// The projects are one list, so one that comes, goes
						// or is reordered glides to its place and the rows
						// below close the gap, instead of jumping there.
						ids := make([]string, len(rows))
						for i, pr := range rows {
							ids[i] = pr.ID
						}
						w.sidebarList.Key = func(row int) any {
							if row < 0 || row >= len(ids) {
								return ""
							}
							return ids[row]
						}
						if w.sidebarList.MoveAnimation == nil {
							w.sidebarList.MoveAnimation = &ui.Animation{
								Duration: sidebarMotion,
								Ease:     ui.EaseOut,
							}
						}
						ui.List(c, &w.sidebarList, len(rows), func(i int) {
							w.projectRow(c, tk, rows[i])
						}).Gap(8)
					}
					ui.Spacer(c)
					// Idle sessions sit at the very bottom, under everything that
					// is running, so putting one away clears the list without
					// hiding the work.
					w.idleSessionSection(c, tk)
				})
			})
			w.sidebarListH = list.LayoutBounds().H
			// The footer holds the sidebar's two menus: remote Macs on the
			// left, settings on the right, under the list and clear of the
			// header's own controls.
			ui.Box(c).Height(1).FillWidth().Background(tk.Border)
			ui.Row(c).Height(iconSize).Gap(2).Padding(0, 2, 0, 2).Children(func() {
				iconMenuButton(c, tk, "monitor", w.remoteMenu, locales.RemoteMenuHint())
				ui.Spacer(c)
				iconButton(c, tk, "setting", func() { w.openSettings() },
					locales.ShortcutSettings()+" (⌘,)")
			})
		})
	})
}

// sidebarHeader is the sidebar's top row. The window drags from it, and the
// toggle at its far left is the only control there: search and new project
// sit at its right end, against the panes, where they are out of the hand's
// way while it reads the list.
func (w *workspace) sidebarHeader(c *ui.Context, tk tokens) {
	bar := c.TitleBar()
	// Six points taller than a pane's header: the sidebar starts at the
	// window's top, and its row shares the pane title's line, which a pane
	// card reaches through its own inset.
	ui.Row(c).Height(headerHeight+6).Padding(16, 8, 0, bar.Left+6+iconSize+8).
		DragWindow().Children(func() {
		ui.Row(c).FillWidth().Height(iconSize).AlignItems(ui.Center).Gap(6).Children(func() {
			w.dragArea(c, tk)
			iconButtonSized(c, tk, "search", iconSize, 16, iconOnMaterial, w.toggleSearch,
				locales.SearchProjects())
			iconButtonSized(c, tk, "plus", iconSize, 16, iconOnMaterial, func() { w.pickProject() },
				locales.ShortcutNewProject()+" (⌘⇧N)")
		})
	})
}

// toggleSearch opens the search over the window's middle, as a palette
// opens, or closes it and drops the filter, so the list comes back whole.
func (w *workspace) toggleSearch() {
	if w.searchOpen {
		w.closeSearch()
		return
	}
	w.searchOpen = true
	w.invalidate()
}

// closeSearch puts the box away and drops the query with it: a filter no
// field shows would outlive the search that made it.
func (w *workspace) closeSearch() {
	w.searchOpen = false
	w.searchQuery = ""
	w.invalidate()
}

// searchDialog is the search: one field over the middle of the window, and
// under it the sessions the query finds, each under its project. Choosing one
// opens it as its sidebar row would; Escape or a press outside the panel
// closes the box.
func (w *workspace) searchDialog(c *ui.Context, tk tokens) {
	panel := ui.DialogBase(c, &w.searchOpen, func(back, panel ui.Element) {
		// A light scrim: the list behind is what the query changes, so the
		// window stays readable under the box.
		shade := ui.RGBA(0, 0, 0, 0.10)
		if tk.Dark {
			shade = ui.RGBA(0, 0, 0, 0.32)
		}
		back.Background(shade).Transition(ui.ElementTransition{
			Duration:     dialogMotion,
			Ease:         ui.EaseOut,
			ExitDuration: dialogExitMotion,
			Enter:        &ui.Motion{},
			Exit:         &ui.Motion{},
		})
		// The box rises and fades in over the window, as the drawer and the
		// reply reader slide in. It leaves the same way, so closing the search
		// does not blink the window back in one frame.
		panel.Transition(ui.ElementTransition{
			Duration:     dialogMotion,
			Ease:         ui.EaseOut,
			ExitDuration: dialogExitMotion,
			Enter:        &ui.Motion{Y: -10, Scale: 0.98},
			Exit:         &ui.Motion{Y: -6, Scale: 0.99},
		})
		panel = raised(panel.Width(520).Clip().Radius(12).Background(tk.Background), tk)
		panel.Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				field := ui.Row(c).Height(52).FillWidth().AlignItems(ui.Center).Gap(8).
					PaddingStart(16).PaddingEnd(16).Cursor(ui.CursorText)
				var in ui.Element
				field.OnClick(func() { in.Focus() }).Children(func() {
					if src := iconSource("search", colorHex(tk.MutedForeground)); src != nil {
						ui.Image(c, src).Width(16).Height(16).IgnoreInput(true)
					}
					// The field has the keyboard as the box opens, so the
					// search begins with the typing that follows the click.
					in = ui.TextInputBase(c, &w.searchQuery).Label(locales.SearchProjects()).
						Placeholder(locales.SearchPlaceholder()).PlaceholderColor(tk.MutedForeground).
						FontSize(14).FontWeight(300).FillWidth().AutoFocus()
				})
				ui.Box(c).Height(1).FillWidth().Background(tk.Border)
				ui.Scroll(c).FillWidth().MaxHeight(360).Padding(6).Children(func() {
					w.searchResults(c, tk)
				})
			})
		})
	})
	if panel.Dismissed() {
		w.closeSearch()
	}
}

// searchResults lists what the query finds, the way the sidebar would: a
// project's name over the sessions it holds. A project the query names shows
// all of its sessions; one it does not shows only the sessions it matches.
func (w *workspace) searchResults(c *ui.Context, tk tokens) {
	q := strings.ToLower(strings.TrimSpace(w.searchQuery))
	found := false
	for _, pr := range w.allProjects() {
		hit := strings.Contains(strings.ToLower(pr.name()), q) ||
			strings.Contains(strings.ToLower(pr.Dir), q)
		var rows []*session
		for _, s := range pr.Sessions {
			if hit || strings.Contains(strings.ToLower(sessionLabel(s)), q) {
				rows = append(rows, s)
			}
		}
		if len(rows) == 0 {
			continue
		}
		found = true
		ui.Text(c, pr.name()).FontSize(11).FontWeight(400).SingleLine().
			TextColor(tk.MutedForeground).Padding(6, 8, 2, 8)
		for _, s := range rows {
			w.searchResultRow(c, tk, s)
		}
	}
	if !found {
		ui.Text(c, locales.SearchEmpty()).FontSize(13).TextColor(tk.MutedForeground).
			Padding(10, 8, 10, 8)
	}
}

// searchResultRow is one session the search found: the mark its sidebar row
// wears, and its name. A press opens it, as that row's press would.
func (w *workspace) searchResultRow(c *ui.Context, tk tokens, s *session) {
	row := ui.Row(c).Height(32).FillWidth().AlignItems(ui.Center).Gap(8).
		PaddingStart(8).PaddingEnd(8).Radius(chromeRadius).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row = row.Background(tk.Foreground.Alpha(0.045))
	}
	pick := func() {
		w.selectSession(s.ID)
		w.closeSearch()
	}
	row.OnClick(pick).Children(func() {
		if agent := s.agent(); agent != nil {
			if src := agentLogoSource(agent.Kind, tk); src != nil {
				ui.Image(c, src).Width(14).Height(14).OnClick(pick)
			}
		} else if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(14).Height(14).OnClick(pick)
		}
		ui.Text(c, sessionLabel(s)).FontSize(13).FontWeight(400).SingleLine().
			TextColor(tk.Foreground).OnClick(pick)
	})
}

func (w *workspace) sidebarToggle(c *ui.Context, tk tokens) {
	iconButton(c, tk, "panel-left", func() {
		w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
		w.settings.save()
		w.invalidate()
	}, locales.ShortcutToggleSidebar()+" (⌘B)")
}

// projectRow is a folder header: it folds its sessions in and out, and offers
// another session or closing the project on hover. The header reads as a
// label above its sessions, lighter and smaller than them, with no fill of
// its own: the session card is the only thing that takes a surface.
func (w *workspace) projectRow(c *ui.Context, tk tokens, pr *project) {
	open := !w.collapsed[pr.ID] || strings.TrimSpace(w.searchQuery) != ""
	row := ui.Row(c).Height(groupRowHeight).Padding(0, 8, 0, 8).Gap(5).Radius(chromeRadius).
		MarginX(2)
	hovered := row.Hovered()
	// The selected session's capsule marks where the work is; the project
	// label above it stays a quiet section heading, as Rex's groups do.
	color := groupLabelColor(tk)
	if hovered {
		color = tk.Foreground
	}
	toggle := func() { w.toggleProject(pr) }
	row.OnClick(toggle).ContextMenu(func(m *ui.Menu) { w.projectRowMenu(m, pr) }).Children(func() {
		chevron := "chevron-right"
		if open {
			chevron = "chevron-down"
		}
		if src := iconSource(chevron, colorHex(color)); src != nil {
			ui.Image(c, src).Width(12).Height(12).OnClick(toggle)
		}
		// A paired Mac wears the monitor its menu does, so it reads as a
		// host rather than another checkout.
		if pr.remote != nil {
			if src := iconSource("monitor", colorHex(color)); src != nil {
				ui.Image(c, src).Width(13).Height(13).OnClick(toggle)
			}
		}
		ui.Text(c, pr.name()).FontSize(11.5).FontWeight(400).SingleLine().
			TextColor(color).OnClick(toggle)
		ui.Spacer(c).OnClick(toggle)
		if pr.remote != nil && pr.remote.conn.err != "" {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(tk.Waiting).
				Tooltip(pr.remote.conn.err).OnClick(toggle)
		}
		// The + makes the session where it belongs: here for a local
		// project, on the host for a paired Mac. A local one offers the
		// agents under the same button. It stays in the row while it is
		// not hovered, so the menu it opened still has a button to report
		// its choice to after the pointer moves onto the menu.
		if pr.remote != nil {
			if hovered {
				iconButtonSmall(c, tk, "plus", func() { _, _ = w.newSession(pr, nil, pr.Dir) },
					locales.ShortcutNewSession()+" (⌘T)")
			}
		} else {
			plus := iconMenuButtonSmall(c, tk, "plus", func(m *ui.Menu) {
				w.newSessionMenu(m, pr)
			}, locales.ShortcutNewSession()+" (⌘T)")
			if !hovered {
				plus = plus.Opacity(0).IgnoreInput(true).AccessHidden(true)
			}
		}
		// The menu stays in the row while it is not hovered, for the same
		// reason the + does: the pointer moves onto the menu to choose an
		// item, and the button has to be there to report the choice.
		more := iconMenuButtonSmall(c, tk, "ellipsis", func(m *ui.Menu) {
			w.projectRowMenu(m, pr)
		}, locales.More())
		if !hovered {
			more = more.Opacity(0).IgnoreInput(true).AccessHidden(true)
		}
		if hovered {
			iconButtonSmall(c, tk, "x", func() { w.closeProject(pr) }, locales.CloseProject())
		}
	})
	if !open {
		return
	}
	// A host's projects are folders of their own under its row: sessions sit
	// under the folder they run in, and each folder's + makes a session
	// there, so the new shell starts in that project rather than in whichever
	// one the host happened to have selected. A host that sends no projects
	// keeps its sessions flat under its row, as before.
	if pr.remote != nil {
		for _, g := range pr.remote.conn.groups(pr.Sessions) {
			w.remoteProjectRow(c, tk, pr, g, 0)
		}
		return
	}
	for _, s := range pr.Sessions {
		w.sessionRow(c, tk, pr, s, 0)
	}
}

// newSessionMenu is the project row's +: the terminal the button used to
// make straight away, then the agents. An agent starts in a worktree of its
// own, so choosing one both opens the checkout and enters the agent there.
func (w *workspace) newSessionMenu(m *ui.Menu, pr *project) {
	terminal := m.Item(locales.NewTerminal()).ID("terminal").
		Shortcut(ui.Cmd, ui.KeyT)
	if icon := terminalMenuIcon(); icon != nil {
		terminal = terminal.Icon(icon)
	}
	if terminal.Chosen() {
		_, _ = w.newSession(pr, nil, pr.Dir)
		return
	}
	m.Separator()
	for _, kind := range agentKinds() {
		item := m.Item(kind.name()).ID(string(kind)).
			Disabled(w.agentCreateBusy)
		if icon := agentMenuIcon(kind); icon != nil {
			item = item.Icon(icon)
		}
		if item.Chosen() {
			w.newAgentSession(pr, kind)
		}
	}
}

// remoteProjectRow is one of a host's folders under its row: its sessions,
// the + that makes another there, and the fold that hides them. A host that
// named no project draws its sessions with no folder row of their own.
func (w *workspace) remoteProjectRow(c *ui.Context, tk tokens, pr *project, g remoteProjectGroup, indent float32) {
	if g.project.ID == "" && g.project.Name == "" {
		for _, s := range g.sessions {
			w.sessionRow(c, tk, pr, s, indent)
		}
		return
	}
	key := remoteGroupKey(pr.ID, g.project.ID)
	open := !w.collapsed[key] || strings.TrimSpace(w.searchQuery) != ""
	row := ui.Row(c).Height(groupRowHeight).PaddingStart(8 + indent).PaddingEnd(8).Gap(5).
		Radius(chromeRadius).MarginX(2)
	hovered := row.Hovered()
	color := groupLabelColor(tk)
	if hovered {
		color = tk.Foreground
	}
	toggle := func() {
		if w.collapsed[key] {
			delete(w.collapsed, key)
		} else {
			w.collapsed[key] = true
		}
		w.invalidate()
	}
	row.OnClick(toggle).Children(func() {
		chevron := "chevron-right"
		if open {
			chevron = "chevron-down"
		}
		if src := iconSource(chevron, colorHex(color)); src != nil {
			ui.Image(c, src).Width(12).Height(12).OnClick(toggle)
		}
		if src := iconSource("folder", colorHex(color)); src != nil {
			ui.Image(c, src).Width(13).Height(13).IgnoreInput(true).OnClick(toggle)
		}
		ui.Text(c, g.project.Name).FontSize(11.5).FontWeight(400).SingleLine().
			TextColor(color).Tooltip(g.project.Dir).OnClick(toggle)
		ui.Spacer(c).OnClick(toggle)
		if hovered {
			iconButtonSmall(c, tk, "plus", func() {
				delete(w.collapsed, pr.ID)
				pr.remote.conn.newSession(g.project.ID)
			}, locales.ShortcutNewSession()+" (⌘T)")
		}
	})
	if !open {
		return
	}
	for _, s := range g.sessions {
		w.sessionRow(c, tk, pr, s, indent+12)
	}
}

// sessionRow is one piece of work in a project: an agent's mark or a terminal
// glyph, its name, a spinner in the mark's slot while its agent works, and a
// menu button on hover. The selected row is a card on the sidebar's surface, so
// the eye finds it without an accent colour. The button's place is kept
// while the pointer is away, so the name does not move as it comes and goes.
func (w *workspace) sessionRow(c *ui.Context, tk tokens, pr *project, s *session, indent float32) {
	sel := s.ID == w.SelectedSession
	color := tk.MutedForeground
	// Sessions step in from the project header: the header is the label, the
	// rows under it are its contents. A session under one of a host's
	// folders steps in once more.
	//
	// On glass a session is a capsule, as Rex's chosen row is: the white sheet
	// needs the softer end to read as a lifted piece of paper rather than a
	// small rectangular card. With glass off it keeps the square-ish 6pt card.
	radius := chromeRadius
	if glassChrome() {
		radius = rowHeightPx / 2
	}
	row := ui.Row(c).Height(rowHeightPx).PaddingStart(8+indent+16).PaddingEnd(8).Gap(8).
		Radius(radius).MarginX(2).Border(1, ui.Transparent)
	hovered := row.Hovered()
	if sel || hovered {
		color = tk.Foreground
	}
	// The hairline is reserved on every row, so selecting one does not shift
	// its contents by a pixel.
	switch {
	case sel:
		fill := tk.Raised
		if tk.Dark {
			fill = tk.Foreground.Alpha(0.10)
		}
		row = lit(lifted(row.Background(fill), tk), tk)
	case hovered:
		row = row.Background(tk.Foreground.Alpha(0.045))
	}
	pick := func() { w.selectSession(s.ID) }
	activity := sessionActivityState(s)
	markColor := tk.MutedForeground
	if sel {
		markColor = tk.Foreground
	}
	row.OnClick(pick).ContextMenu(func(m *ui.Menu) { w.sessionRowMenu(m, s) }).Children(func() {
		agent := s.agent()
		// Every mark takes the same slot, so selecting a row or an agent
		// changing its state never moves the label beside it.
		const markSize float32 = 18
		mark := func(child func()) {
			ui.Box(c).Width(markSize).Height(markSize).Center().OnClick(pick).Children(child)
		}
		switch {
		case activity == activityWorking:
			// A spinner where the session's mark sits, so a glance reads the
			// session as working.
			mark(func() {
				loading.Dual(c, loading.Options{Size: 15, Color: tk.Foreground}).IgnoreInput(true)
			})
		case agent != nil:
			if src := agentLogoSource(agent.Kind, tk); src != nil {
				mark(func() {
					ui.Image(c, src).Width(14).Height(14).IgnoreInput(true)
				})
			}
		default:
			if sel && glassChrome() {
				// Rex's chosen terminal: a small ink chip with a light glyph,
				// so the selected row reads before its label does.
				if src := iconSource("terminal", colorHex(tk.Background)); src != nil {
					ui.Box(c).Width(markSize).Height(markSize).Radius(4).Background(tk.Foreground).
						Center().OnClick(pick).Children(func() {
						ui.Image(c, src).Width(11).Height(11).IgnoreInput(true)
					})
				}
			} else if src := iconSource("terminal", colorHex(markColor)); src != nil {
				mark(func() {
					ui.Image(c, src).Width(14).Height(14).IgnoreInput(true)
				})
			}
		}
		label := sessionLabel(s)
		// The name reads still: the spinner in the mark's slot is what says
		// the session works, and a name that moves beside it says nothing
		// more while it asks for a frame every step.
		labelEl := ui.Text(c, label).FontSize(13).FontWeight(400).SingleLine()
		switch activity {
		case activityWorking, activityWaiting:
			labelEl = labelEl.TextColor(tk.Foreground)
		default:
			labelEl = labelEl.TextColor(color)
		}
		labelEl.OnClick(pick)
		ui.Spacer(c).OnClick(pick)
		// The row's menu, where the close button used to be: renaming
		// and the two ways a session goes are one press away, exactly
		// as the right-click offers them. It keeps its place while the
		// row is not hovered, so the menu it opened still has a button
		// to report its choice to after the pointer moves onto it.
		more := iconMenuButtonSmall(c, tk, "ellipsis", func(m *ui.Menu) {
			w.sessionRowMenu(m, s)
		}, locales.More())
		if !hovered {
			more = more.Opacity(0).IgnoreInput(true).AccessHidden(true)
		}
	})
}

// sessionLabel is what the sidebar calls a session: the name the user gave
// it, else its branch, its agent when one runs there, or the terminal title.
// The manual name comes first, so an agent renaming the branch or the title
// cannot change a name the user set.
func sessionLabel(s *session) string {
	if s.Label != "" {
		return s.Label
	}
	// A session on another Mac is called what its host calls it, with the
	// host's activity mark dropped: the row already wears the agent's logo.
	if s.remote != nil && s.remote.title != "" {
		return cleanTitle(s.remote.title)
	}
	// The first pane is the work the row stands for, so its name names the
	// session too: one named by hand, or made from its first message.
	if s.Root != nil {
		ps := s.Root.panes()
		if len(ps) > 0 && ps[0].Label != "" {
			return ps[0].Label
		}
		for _, p := range ps {
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

// sidebarProjectRows is what the sidebar lists: the projects open here, then
// the paired Macs, whose sessions are sessions of the same code, filtered by
// the search box.
func (w *workspace) sidebarProjectRows() []*project {
	rows := make([]*project, 0, len(w.Projects)+len(w.remoteProjects))
	for _, pr := range w.Projects {
		if w.projectVisible(pr) {
			rows = append(rows, pr)
		}
	}
	for _, pr := range w.remoteProjects {
		if w.projectVisible(pr) {
			rows = append(rows, pr)
		}
	}
	return rows
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

// sessionActivityState folds the panes' hook events and terminal title marks
// into one sidebar indicator. A running subagent counts as work even when its
// pane's turn has ended: a background one is exactly the case where the prompt
// has returned but the session is still busy.
func sessionActivityState(s *session) string {
	working := false
	if s.Root == nil {
		return ""
	}
	for _, p := range s.Root.panes() {
		if p.conn != nil {
			if n := p.conn.outputTotal(); n != p.outSeen {
				p.outSeen, p.outAt = n, time.Now()
			}
		}
		if p.outAt.IsZero() {
			p.outAt = time.Now()
		}
		if p.activity != nil && p.activity.State == activityWaiting {
			return activityWaiting
		}
		if p.working() {
			working = true
		}
	}
	if working {
		return activityWorking
	}
	return ""
}

// activityQuiet is how long a pane marked at work may say nothing at all
// before the mark is taken as stale: a hook that never came, or one the
// other Mac told us about and never corrected.
const activityQuiet = 90 * time.Second

// working reports whether an agent is at work in the pane: a turn its hooks
// have not ended, a subagent still running, or a title whose spinner still
// moves. A pane opened or switched mid-turn can miss agent.start; its title
// still carries the animated spinner while it works, so the session loader
// does not stop under a running terminal. The sidebar's mark and the pane's
// beam both read it.
func (p *pane) working() bool {
	working := p.subagentCount() > 0 || p.titleWorking() ||
		p.activity != nil && p.activity.State == activityWorking && !p.activityLapsed()
	if !working {
		p.beamStart = time.Time{}
		return false
	}
	if p.beamStart.IsZero() {
		p.beamStart = time.Now()
	}
	return true
}

// activityLapsed reports whether a working mark has gone stale: no output,
// no animating title, and no subagent still running.
func (p *pane) activityLapsed() bool {
	if p == nil || p.subagentCount() > 0 {
		return false
	}
	if p.titleWorking() {
		return false
	}
	// A local agent still in the foreground owns its turn. Thinking, a long
	// build or a slow tool call writes nothing to the terminal for minutes,
	// and the quiet rule used to drop the loader in the middle of that
	// work. The process scan clears the agent when it exits, and the rule
	// applies again from there.
	if p.remote == nil && p.agent != nil {
		return false
	}
	return time.Since(p.outAt) > activityQuiet
}

// subagentPillWidth measures the running-subagent chip before it is drawn, so
// the header can give it room beside the branch pill.
func (w *workspace) subagentPillWidth(c *ui.Context, p *pane) float32 {
	n := p.subagentCount()
	if n <= 0 {
		return 0
	}
	tw, _ := c.MeasureText(0, ui.Span{Text: fmt.Sprint(n), Size: 10})
	return 12 + 6 + 4 + tw
}

// subagentPill is the pane header's running-subagent count, after the branch
// pill: the dot is the activity colour, so a child still running after its
// turn ended reads as work even when the pane itself is idle.
func (w *workspace) subagentPill(c *ui.Context, tk tokens, p *pane) {
	n := p.subagentCount()
	if n <= 0 {
		return
	}
	el := ui.Row(c).Height(22).Radius(headerRadius).Padding(0, 6).Gap(4).
		AlignItems(ui.Center)
	dot := tk.Working
	if p.activity == nil || p.activity.State != activityWorking {
		// The turn is over but a child still runs: amber says the work
		// continued past the prompt that started it.
		dot = tk.Waiting
	}
	el = badge(el, tk, el.Hovered())
	el.Tooltip(subagentHint(p)).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Background(dot)
		ui.Text(c, fmt.Sprint(n)).FontSize(10).FontWeight(500).
			TextColor(tk.MutedForeground)
	})
}

// subagentHint names what the chip counts and whether any of it outlived the
// turn, which is the only "background" the header can see.
func subagentHint(p *pane) string {
	kinds := []string{}
	seen := map[string]bool{}
	for _, a := range p.subagentsJSON() {
		kind, _ := a["type"].(string)
		if kind != "" && !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	hint := locales.PaneSubagents(fmt.Sprint(p.subagentCount()))
	if len(kinds) > 0 {
		hint += " · " + strings.Join(kinds, ", ")
	}
	if p.activity == nil || p.activity.State != activityWorking {
		hint += " · " + locales.PaneSubagentsBackground()
	}
	return hint
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
		outer = outer.Padding(6)
	}
	// A pane takes another pane dropped on it: the side it lands on splits
	// toward that side, the middle trades the two. The hint is read before
	// the drop, which clears the drag, so both read the pointer directly.
	// A pane on another Mac is not moved by this window's drags: its layout
	// belongs to the host.
	moved, over := ui.DragOver[paneMoved](outer)
	place := paneDropNone
	if over && p.remote == nil {
		place = dropPlaceOf(outer, moved.Pane, p)
	}
	if v, dropped := ui.Drop[paneMoved](outer); dropped && p.remote == nil {
		if at := dropPlaceOf(outer, v.Pane, p); at != paneDropNone {
			w.movePane(v.Pane, p, at)
			place = paneDropNone
		}
	}
	outer.Children(func() {
		// The card itself is the glass: its own sheet carries the pane's
		// surface, so a fade or an unfocused pane shows the same paper, not
		// an opaque backing box around it.
		col := ui.Column(c).Fill()
		// A pane at full strength is the one being typed into or the one the
		// pointer is over; the rest step back so the split reads as one work
		// surface with depth, not as equal peers.
		dim := col.Animate("focusDim", w.focusAmount(focused || col.Hovered()), focusMotion)
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
		} else if p.stageEnter {
			// A card clicked forward grows from its card's size and place
			// into the stage; the pane it replaces shrinks back into the
			// rail the same way.
			p.stageEnter = false
			pose = ui.Pose{Scale: 0.22, X: w.stageCardOffset(), Fade: 1}
			c.After(time.Millisecond)
		}
		col = col.AnimatePose(pose, ui.Animation{Duration: paneMotion, Ease: ui.EaseOut})
		if card {
			col = col.Radius(windowRadius).Clip()
			if liquid(c) {
				top, bottom := paneSurface(tk)
				col = col.Gradient(top, bottom, 180)
			} else {
				col = col.Background(tk.Background)
			}
			col = raised(col, tk)
		}
		dragging := false
		blend := ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut}
		// The header is the card's own surface, so it reads as the same
		// colour as the terminal under it; a lifted pane fills it with the
		// canvas, so the slot it leaves reads as empty.
		panelBg := tk.Background
		headerBg := tk.Background
		if liquid(c) {
			// The header is the card's own sheet; leaving it undrawn lets
			// the card's gradient run through the title as well.
			panelBg = ui.Transparent
			headerBg = ui.Transparent
		}
		col.Children(func() {
			left := panePadX
			if underChrome {
				left = bar.Left + 6 + iconSize + 8
			}
			// Headers are always there: hiding them would leave the collapsed
			// sidebar toggle over bare terminal content.
			// No rule under the header: the pane card itself is the frame.
			headerH := w.paneHeaderHeight()
			header := lit(ui.Row(c).Height(headerH).Padding(headerH-headerHeight, 6, 0, left).
				Background(headerBg).ClipY().DragWindow(), tk)
			// The pane's name is renamed from anywhere on its header bar,
			// not only the few points the title itself covers.
			if p.remote == nil {
				header = header.ContextMenu(func(m *ui.Menu) { w.paneHeaderMenu(m, p) })
			}
			// Split and close belong to the pane the hand is on: they appear
			// wherever the pointer is over the pane, its terminal included,
			// or when the pane has the focus, so an idle layout is only the
			// terminal and its title.
			showActions := focused || col.Hovered()

			header.Children(func() {
				line := ui.Row(c).FillWidth().Height(chromeLine(bar)).AlignItems(ui.Center).Gap(4)
				// The header keeps the title and the pane's buttons; the branch
				// pill and the path fit around them by what each one measures,
				// so a narrowing pane gives them up in that order and a wide
				// one shows them all. The line's width is the one it had in the
				// frame before, which is exact until the pane or its split is
				// resized and settles a frame later.
				width := line.Bounds().W
				// The title picks the pane up once there is a second pane
				// to trade with. Alone in the window it keeps dragging the
				// window, as a title bar does, since there is nowhere to
				// put it.
				movable := len(w.visiblePanes()) > 1 && p.remote == nil
				// A sliver of a pane spends nothing on padding it does not
				// need: a couple of points buy its title back.
				stripPad := float32(6)
				if width > 0 && width < 200 {
					stripPad = 3
				}
				// A detected agent wears its own mark and name, in place
				// of the terminal title with the agent's spinner glyph.
				// The directory stays: it is the one thing the brand
				// does not say, which checkout the agent works in.
				agent := p.agent
				title := p.title()
				if agent != nil && p.Label == "" {
					title = agent.Kind.name()
				}
				if p.exited {
					title += " · exited"
				}
				// The title matters more than its mark once the pane
				// is a sliver: the name stays, the glyph goes.
				showMark := width == 0 || width >= 160
				markW := float32(0)
				if showMark {
					markW = 16
					if agent != nil {
						markW = 13
					}
					markW += 4 // the gap before the title
				}
				titleW, _ := c.MeasureText(0, ui.Span{Text: title, Size: 12, Weight: 500})
				actionsW := p.headerActionsW
				if actionsW == 0 {
					// The first frame has no row to read yet; guess at what
					// the buttons take from the width they have.
					actionsW = 120
					if width > 0 && width < 380 {
						actionsW = 56
					}
				}
				// The strip's padding and the line's gaps: one between the
				// strip, the path chip, the pill, the subagent count, the drag
				// area and the actions.
				kept := 2*stripPad + markW + titleW + actionsW + 4*4
				info, hasPill := w.branchPillInfo(p)
				pillW := float32(0)
				if hasPill {
					pillW = branchPillWidth(c, info)
				}
				showPill := hasPill && (width == 0 || kept+pillW <= width)
				pillSpace := float32(0)
				if showPill {
					pillSpace = pillW + 4
				}
				subW := w.subagentPillWidth(c, p)
				showSub := subW > 0 && (width == 0 || kept+pillSpace+subW <= width)
				subSpace := float32(0)
				if showSub {
					subSpace = subW + 4
				}
				// The path names the checkout with its last segment alone, and
				// the pointer brings the whole path: the header says where the
				// work is, not the road to it.
				pathRoom := width - kept - pillSpace - subSpace - 4
				if capRoom := width * 0.4; pathRoom > capRoom {
					pathRoom = capRoom
				}
				dir := p.dir()
				_, managed := worktreePaneName(dir)
				name := checkoutName(dir)
				// A checkout whose folder carries its branch's name says the
				// same thing twice. The branch pill then stands for both, and
				// its hover names the path it folded in.
				merged := showPill && name == info.branch
				path := ""
				if !merged {
					room := pathRoom - 10 // the chip's padding a side
					tw, _ := c.MeasureText(0, ui.Span{Text: name, Size: 11})
					if width == 0 || (room > 0 && tw <= room) {
						path = name
					}
				}
				line.Children(func() {
					// The hover hairline is reserved while it is away, so the
					// title does not move as the pointer comes and goes.
					strip := ui.Row(c).Height(24).AlignItems(ui.Center).Shrink(1).
						Gap(4).Padding(0, stripPad).Radius(chromeRadius).
						Border(1, ui.Transparent)
					if movable {
						strip = strip.Drag(paneMoved{Pane: p}).Cursor(ui.CursorGrab).
							Tooltip(locales.PaneDragHint())
						dragging = strip.Dragging()
					} else {
						strip = strip.DragWindow()
					}
					strip = hoverWash(c, strip, tk)
					strip.Children(func() {
						if agent != nil {
							if src := agentLogoSource(agent.Kind, tk); src != nil && showMark {
								ui.Image(c, src).Width(13).Height(13)
							}
						} else if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil && showMark {
							ui.Image(c, src).Width(16).Height(16)
						}
						// The title gives way only when the close button
						// would otherwise be clipped.
						titleShrink := float32(0)
						if width > 0 && width < 200 {
							titleShrink = 1
						}
						if focused {
							ui.Text(c, title).FontSize(12).SingleLine().
								Shrink(titleShrink).TextColor(tk.Foreground)
						} else {
							ui.Text(c, title).FontSize(12).SingleLine().
								Shrink(titleShrink).TextColor(tk.MutedForeground)
						}
					})
					// The path is a chip of its own: the title names the agent,
					// and the checkout must not read as part of that name. A
					// gexi worktree is tinted so it reads as a place; any other
					// checkout keeps the quiet control surface.
					if path != "" {
						chip := ui.Row(c).Height(18).Radius(4).Padding(0, 5).
							AlignItems(ui.Center).Shrink(1)
						chip = badge(chip, tk, chip.Hovered())
						// The checkout's name stays quiet; a gexi
						// worktree takes the system accent, so it reads
						// as a place rather than another path.
						color := tk.MutedForeground
						if managed {
							color = tk.SystemAccent
						}
						// The header keeps the checkout's name; the pointer
						// brings the full path it stands for.
						chip.Tooltip(dir).Children(func() {
							ui.Text(c, path).FontSize(11).SingleLine().
								Shrink(1).TextColor(color)
						})
					}
					// The branch pill follows the title: it names the branch
					// the project is on, counts the working tree's changes,
					// and opens their diff. It sits inside the header band
					// rather than filling it, so hovering reads as a chip and
					// not as a second bar. The drawer still opens from the
					// menu when the header has no room for it. Merged with the
					// path, it carries the checkout's full path in its hint.
					if showPill {
						hint := ""
						if merged {
							hint = locales.DrawerOpenChanges() + " · " + dir
						}
						w.branchPill(c, tk, p, hint)
					}
					// Running subagents sit beside the branch they work on:
					// the panel says where the work is, and how much of it
					// is still going.
					if showSub {
						w.subagentPill(c, tk, p)
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
						// The reply sits beside the checkout it is about,
						// and appears with the other controls so an idle
						// header stays quiet.
						if p.remote == nil && len(p.conversation) > 0 {
							w.replyChip(c, tk, p)
						}
						// The worktree button is the pane's checkout: a local
						// one here, and a host's pressed through it, since
						// the checkout and its pull request are the host's.
						if p.remote == nil {
							w.paneWorktreeButton(c, tk, p)
						} else {
							w.remoteWorktreeButton(c, tk, p)
						}
						// A pane too narrow for all three keeps only the way
						// out: splitting and zooming are in the View menu, and
						// a clipped button is worse than an absent one.
						if width == 0 || width >= 380 {
							iconButtonOnCard(c, tk, "columns-2", func() { _, _ = w.splitPane(p, axisHorizontal, nil, "") },
								locales.MenuSplitRight()+" (⌘D)")
							iconButtonOnCard(c, tk, "rows-2", func() { _, _ = w.splitPane(p, axisVertical, nil, "") },
								locales.MenuSplitDown()+" (⌘⇧D)")
							// A pane with nowhere to go has nothing to zoom to.
							if len(w.visiblePanes()) > 1 || w.isZoomed(p) {
								name, hint := "expand", locales.PaneZoom()+" ("+zoomShortcut+")"
								if w.isZoomed(p) {
									name, hint = "collapse", locales.PaneUnzoom()+" ("+zoomShortcut+")"
								}
								iconButtonOnCard(c, tk, name, func() { w.toggleZoom(p) }, hint)
							}
						}
						iconButtonOnCard(c, tk, "x", func() { w.requestCloseByUser(p) },
							locales.MenuClosePane()+" (⌘W)")
					})
					// The row's width follows the buttons it holds, so the
					// next frame sets the path and the pill beside it.
					if actionsW := actions.Bounds().W; actionsW > 0 {
						p.headerActionsW = actionsW
					}
				})
			})
			// A raised pane takes the canvas colour, so the slot reads as
			// empty behind the chip the pointer carries.
			if dragging {
				panelBg = w.backdrop(tk)
			}
			header.Background(header.AnimateColor("paneLiftBg", panelBg, blend))

			var el ui.Element
			if p.remote != nil {
				// A pane on another Mac shows that Mac's screen at that
				// Mac's grid: scaled down to fit when the card is smaller,
				// centered when it is bigger, so every part of the screen
				// stays visible at a readable size. The grid this card fits
				// goes with the watch, and the other Mac takes it while its
				// own window is not in use.
				area := ui.Box(c).Fill().Clip()
				area.Children(func() {
					aw, ah := area.Bounds().W, area.Bounds().H
					if aw > 0 && ah > 0 {
						p.remote.measureCell()
						cols, rows := p.remote.wantGrid(aw, ah)
						p.remote.wantCols.Store(int32(cols))
						p.remote.wantRows.Store(int32(rows))
					}
					natW, natH := mirrorSize(p)
					// That Mac's screen whole in this card: scaled down when
					// its grid is wider or taller than the card, which it is
					// while this window owns the grid, rather than cut off.
					scale := float32(1)
					if natW > 0 && natH > 0 && aw > 0 && ah > 0 {
						scale = min(1, aw/natW, ah/natH)
					}
					left, top := (aw-natW*scale)/2, (ah-natH*scale)/2
					el = terminal.View(c, p.term).Width(natW).Height(natH).
						Absolute().Left(left).Top(top).
						TransformOrigin(0, 0).Scale(scale, scale)
				})
				el = area
			} else {
				// The pane keeps its own card's grid: a watching Mac fits
				// the screen it gets into its card, rather than the host
				// resizing a pane someone here is looking at.
				el = terminal.View(c, p.term).Fill()
				if b := el.LayoutBounds(); b.W > 0 && b.H > 0 {
					p.viewW, p.viewH = b.W, b.H
					if cols, rows := p.term.Size(); cols > 0 && rows > 0 {
						if cw := (b.W - 2*panePadX) / float32(cols); cw > 0 {
							p.cellW = cw
							w.paneCellW = cw
						}
						if ch := (b.H - 2*panePadY) / float32(rows); ch > 0 {
							p.cellH = ch
							w.paneCellH = ch
						}
					}
				}
			}
			if el.Focused() && w.FocusedPane != p.ID {
				w.FocusedPane = p.ID
				if p.remote != nil {
					p.remote.conn.focusPane(p.remote.id)
				}
			}
			if w.focusReq == p.ID {
				el = el.Focus()
				w.focusReq = ""
			}
			// The completion sweep bends and lights the terminal itself;
			// the header above it takes no part.
			if fx := p.haloEffect(c, tk); fx != nil {
				el = el.Effect(fx)
			}
			// A running pull request or merge says its stages here, in
			// the corner, rather than in a hint that needs a hover.
			if p.remote == nil {
				w.worktreeCheckpoints(c, tk, p)
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
		// A working pane's light rides the bottom edge of its card. It paints
		// after the terminal so the glow sits over the last rows, as the
		// library's does, instead of under the terminal's opaque background.
		if p.working() {
			still := c.Preferences().ReduceMotion
			start := p.beamStart
			col = col.DrawOver(func(pt *ui.Painter, r ui.Rect) {
				// The beam paints in frames that only paint, so no frame
				// that builds the view comes round to notice the work is
				// over. Ask again here, every frame: a turn that ended
				// without its hook, or a title that stopped spinning, then
				// takes the light with it instead of leaving it running.
				if !p.working() {
					return
				}
				paintPaneBeam(pt, r, tk, card, pt.Now(), still, start)
			})
		}
	})
}

// focusDim is how far the other panes recede once a tab is split, so one pane
// holds the eye. The pointer restores the pane it is over, so looking at
// another pane does not mean focusing it. The sidebar keeps its own strength:
// it is navigation, not a peer of the panes.
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

// focusAmount is a part of a pane's opacity: full when the pane holds focus
// or the pointer is over it, dimmed otherwise in a split.
func (w *workspace) focusAmount(full bool) float32 {
	if full || !w.dimming() {
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

// checkoutName is what the header calls a checkout: the short name gexi gave
// its worktree, or the last segment of any other path.
func checkoutName(dir string) string {
	if name, ok := worktreePaneName(dir); ok {
		return name
	}
	return filepath.Base(dir)
}

// chromeLine is the height of the window controls' row: titles and buttons
// sit on that line, even though the header band is taller.
func chromeLine(bar ui.TitleBar) float32 {
	return 32
}

// paneHeaderHeight is the band a pane's title takes. A compact pane starts at
// the window's top edge, where a comfortable card is set down by the canvas
// and its own inset, so the band carries that room itself.
func (w *workspace) paneHeaderHeight() float32 {
	if w.settings.PaneStyle == paneComfortable {
		return headerHeight
	}
	return headerHeight + compactHeaderTop
}
