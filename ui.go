package main

import (
	"os"
	"path/filepath"
	"strings"

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
	ui.Row(c).Fill().Background(tk.Background).Children(func() {
		if !w.settings.SidebarCollapsed {
			w.sidebar(c, tk)
		}
		ui.Column(c).Fill().Children(func() {
			if tb := w.selected(); tb != nil {
				w.splitView(c, tk, tb.Root, true)
			} else {
				ui.Column(c).Fill().Center().Children(func() {
					ui.Text(c, "No tabs").FontSize(13).TextColor(tk.MutedForeground)
				})
			}
		})
	})
}

func (w *workspace) sidebar(c *ui.Context, tk tokens) {
	bar := c.TitleBar()
	ui.Column(c).Width(240).FillHeight().Background(tk.Sidebar).
		BorderWidth(0, 1, 0, 0).BorderColor(tk.Border).Children(func() {
		ui.Row(c).Height(headerHeight).Padding(0, 8, 0, bar.Left).Gap(2).Children(func() {
			w.sidebarToggle(c, tk)
			ui.Text(c, "Sessions").FontSize(11).FontWeight(500).
				TextColor(tk.MutedForeground).DragWindow()
			ui.Spacer(c).DragWindow()
			iconButton(c, tk, "plus", func() { _, _ = w.newTab(nil, "") })
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
	if sel {
		row = row.Background(tk.Active)
		color = tk.Foreground
	} else if row.Hovered() {
		row = row.Background(tk.Muted)
	}
	row.OnClick(func() { w.selectTab(tb.ID) }).Children(func() {
		if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(13).Height(13)
		}
		ui.Text(c, tb.title()).FontSize(12).FontWeight(500).TextColor(color)
		ui.Spacer(c)
		ui.Textf(c, "%d", len(tb.Root.panes())).FontSize(10).TextColor(tk.MutedForeground)
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

	ui.Column(c).Fill().Children(func() {
		if w.settings.ShowHeaders {
			left := float32(10)
			if underChrome {
				left = bar.Left
			}
			ui.Row(c).Height(headerHeight).Padding(0, 6, 0, left).Gap(4).
				Background(tk.Background).BorderWidth(0, 0, 1, 0).
				BorderColor(tk.Border).Children(func() {
				if underChrome {
					w.sidebarToggle(c, tk)
				}
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
				iconButton(c, tk, "x", func() { w.closePane(p) })
			})
		}

		el := terminal.View(c, p.term).Fill()
		if el.Focused() && w.FocusedPane != p.ID {
			w.FocusedPane = p.ID
		}
		if w.focusReq == p.ID {
			el = el.Focus()
			w.focusReq = ""
		}
	})
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
