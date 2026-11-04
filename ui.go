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

func (w *workspace) view(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Children(func() {
		if !w.settings.SidebarCollapsed {
			w.sidebar(c)
		}
		ui.Column(c).Fill().Children(func() {
			if tb := w.selected(); tb != nil {
				w.splitView(c, tb.Root, true)
			} else {
				ui.Column(c).Fill().Center().Children(func() {
					ui.Text(c, "No tabs").FontSize(13).TextColor(t.TextMuted)
				})
			}
		})
	})
}

func (w *workspace) sidebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Column(c).Width(250).FillHeight().Background(sidebarColor(t)).
		BorderColor(t.Border).BorderWidth(0, 1, 0, 0).Children(func() {
		ui.Row(c).Height(rowHeight(bar)).Padding(0, 8, 0, bar.Left).Gap(4).Children(func() {
			w.sidebarToggle(c)
			ui.Text(c, "Sessions").FontSize(11).TextColor(t.TextMuted).DragWindow()
			ui.Spacer(c).DragWindow()
			iconButton(c, t, "+", func() { _, _ = w.newTab(nil, "") })
		})
		ui.Column(c).Fill().Padding(8).Gap(2).Children(func() {
			for _, tb := range w.Tabs {
				w.tabRow(c, tb)
			}
			ui.Spacer(c)
		})
	})
}

func (w *workspace) sidebarToggle(c *ui.Context) {
	t := c.Theme()
	iconButton(c, t, "◧", func() {
		w.settings.SidebarCollapsed = !w.settings.SidebarCollapsed
		w.settings.save()
		w.invalidate()
	})
}

func (w *workspace) tabRow(c *ui.Context, tb *tab) {
	t := c.Theme()
	sel := tb.ID == w.SelectedTab
	row := ui.Row(c).Padding(6).Gap(8)
	if sel {
		row = row.Background(t.Selection)
	}
	row.OnClick(func() { w.selectTab(tb.ID) }).Children(func() {
		ui.Text(c, "›_").FontSize(10).TextColor(t.TextMuted)
		ui.Text(c, tb.title()).FontSize(12)
		ui.Spacer(c)
		ui.Textf(c, "%d", len(tb.Root.panes())).FontSize(10).TextColor(t.TextMuted)
	})
}

// splitView mirrors JustCli's recursive split tree. leading marks the child
// that touches the content's top-left corner: when the sidebar is collapsed,
// that one pane header makes room for the traffic lights and the toggle.
func (w *workspace) splitView(c *ui.Context, n *splitNode, leading bool) {
	if n == nil {
		return
	}
	if n.isLeaf() {
		w.paneView(c, n.pane, leading)
		return
	}
	if n.axis == axisHorizontal {
		ui.SplitPercent(c, &n.ratio,
			func() { w.splitView(c, n.first, leading) },
			func() { w.splitView(c, n.second, false) },
		).Fill()
	} else {
		ui.SplitVerticalPercent(c, &n.ratio,
			func() { w.splitView(c, n.first, leading) },
			func() { w.splitView(c, n.second, false) },
		).Fill()
	}
}

func (w *workspace) paneView(c *ui.Context, p *pane, leading bool) {
	t := c.Theme()
	bar := c.TitleBar()
	focused := p.ID == w.FocusedPane
	underChrome := leading && w.settings.SidebarCollapsed

	ui.Column(c).Fill().Children(func() {
		if w.settings.ShowHeaders {
			left := float32(8)
			if underChrome {
				left = bar.Left
			}
			ui.Row(c).Height(rowHeight(bar)).Padding(0, 6, 0, left).Gap(6).Children(func() {
				if underChrome {
					w.sidebarToggle(c)
				}
				ui.Text(c, "›_").FontSize(10).TextColor(t.TextMuted).DragWindow()
				title := p.title()
				if p.exited {
					title += " · exited"
				}
				if focused {
					ui.Text(c, title).FontSize(11).Bold().DragWindow()
				} else {
					ui.Text(c, title).FontSize(11).TextColor(t.TextMuted).DragWindow()
				}
				ui.Text(c, abbreviate(p.dir())).FontSize(10).TextColor(t.TextMuted).DragWindow()
				ui.Spacer(c).DragWindow()
				iconButton(c, t, "◫", func() { _, _ = w.splitPane(p, axisHorizontal, nil, p.dir()) })
				iconButton(c, t, "⬒", func() { _, _ = w.splitPane(p, axisVertical, nil, p.dir()) })
				iconButton(c, t, "×", func() { w.closePane(p) })
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

func sidebarColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGB(0x1e, 0x1e, 0x20)
	}
	return ui.RGB(0xf2, 0xf2, 0xf4)
}

// iconButton is a plain glyph that lights up on hover, instead of mygo's
// default button chrome, which is too heavy for a 32pt header.
func iconButton(c *ui.Context, t *ui.Theme, glyph string, action func()) {
	el := ui.Row(c).Width(24).Height(22).Center().Cursor(ui.CursorPointer)
	if el.Hovered() {
		el = el.Background(hoverColor(t))
	}
	el.OnClick(action).Children(func() {
		ui.Text(c, glyph).FontSize(11).TextColor(t.TextMuted)
	})
}

func hoverColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(0xff, 0xff, 0xff, 0.10)
	}
	return ui.RGBA(0, 0, 0, 0.06)
}

func rowHeight(bar ui.TitleBar) float32 {
	if bar.Height > 32 {
		return bar.Height
	}
	return 32
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
