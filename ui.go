package main

import (
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
	root := ui.Row(c).Fill().Background(tk.Background)

	target := float32(0)
	if !w.settings.SidebarCollapsed {
		target = sidebarWidth
	}
	width := root.Animate("sidebarWidth", target, sidebarMotion)

	root.Children(func() {
		w.sidebar(c, tk, width)
		// The sidebar and the space around the panes share one background,
		// so in comfortable a pane reads as a panel wrapped around its
		// content.
		ui.Column(c).Fill().Background(w.backdrop(tk)).Children(func() {
			w.canvas(c, tk)
		})
		// The toggle stays put whether the sidebar is shown or not, so it
		// sits above the layout rather than inside either header.
		top := (headerHeight - iconSize) / 2
		if bar.Height > 0 {
			top = (bar.Height - iconSize) / 2
		}
		if top < 0 {
			top = 0
		}
		ui.Row(c).Width(iconSize).Height(iconSize).Absolute().
			Left(bar.Left + 6).Top(top).Children(func() {
			w.sidebarToggle(c, tk)
		})
	})
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
		ui.Text(c, "No tabs").FontSize(13).TextColor(tk.MutedForeground)
	})
}

func (w *workspace) sidebar(c *ui.Context, tk tokens, width float32) {
	if width < 0.5 {
		return
	}
	bar := c.TitleBar()
	ui.Column(c).Width(width).FillHeight().Clip().Background(w.backdrop(tk)).
		Children(func() {
			ui.Row(c).Height(headerHeight).Padding(0, 8, headerHeight-chromeLine(bar), bar.Left+6+iconSize+8).
				Children(func() {
					ui.Row(c).FillWidth().Height(chromeLine(bar)).AlignItems(ui.Center).Gap(2).Children(func() {
						ui.Text(c, "Sessions").FontSize(11).FontWeight(500).
							TextColor(tk.MutedForeground).DragWindow()
						ui.Spacer(c).DragWindow()
						iconButton(c, tk, "plus", func() { _, _ = w.newTab(nil, "") })
					})
				})
			ui.Column(c).Fill().Padding(8).Gap(1).Children(func() {
				for _, tb := range w.Tabs {
					w.tabRow(c, tk, tb)
				}
				ui.Spacer(c)
			})
		})
}

func (w *workspace) sidebarToggle(c *ui.Context, tk tokens) {
	iconButton(c, tk, "panel-left", func() {
		w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
		w.settings.save()
		w.invalidate()
	})
}

func (w *workspace) tabRow(c *ui.Context, tk tokens, tb *tab) {
	sel := tb.ID == w.SelectedTab
	color := tk.MutedForeground
	row := ui.Row(c).Height(rowHeightPx).Padding(0, 8, 0, 8).Gap(8).Radius(chromeRadius)
	hovered := row.Hovered()
	switch {
	case sel:
		color = tk.Foreground
	case hovered:
		color = tk.Foreground
	}
	bg := fade(c, tk, row, sel, hovered)
	row = row.Background(bg)
	pick := func() { w.selectTab(tb.ID) }
	row.OnClick(pick).Children(func() {
		if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(13).Height(13).OnClick(pick)
		}
		ui.Text(c, tb.title()).FontSize(12).FontWeight(500).TextColor(color).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
		ui.Textf(c, "%d", len(tb.Root.panes())).FontSize(10).TextColor(tk.MutedForeground).OnClick(pick)
	})
}

// splitView mirrors JustCli's recursive split tree. leading marks the child
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
		outer = outer.Padding(6)
	}
	outer.Children(func() {
		col := ui.Column(c).Fill().Opacity(fadeAlpha(c, p))
		if card {
			col = col.Radius(8).Border(1, tk.Border).Background(tk.Background).Clip()
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
					if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
						ui.Image(c, src).Width(13).Height(13).DragWindow()
					}
					title := p.title()
					if p.exited {
						title += " · exited"
					}
					if focused {
						ui.Text(c, title).FontSize(12).FontWeight(500).
							TextColor(tk.Foreground).DragWindow()
					} else {
						ui.Text(c, title).FontSize(12).TextColor(tk.MutedForeground).DragWindow()
					}
					ui.Text(c, abbreviate(p.dir())).FontSize(11).
						TextColor(tk.MutedForeground).DragWindow()
					ui.Spacer(c).DragWindow()
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

// fade returns shadcn's 150ms ease-out transition for a row or button
// background: active when selected, muted when hovered, transparent otherwise.
func fade(c *ui.Context, tk tokens, el ui.Element, active, hovered bool) ui.Color {
	target := ui.RGBA(0, 0, 0, 0)
	switch {
	case active:
		target = tk.Active
	case hovered:
		target = tk.Muted
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
	if bar.Height > 0 {
		return bar.Height
	}
	return 28
}
