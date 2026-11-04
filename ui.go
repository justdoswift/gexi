package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// view is the whole window: a sidebar of tabs and the selected tab's tree.
// mygo calls it for every frame, so it only reads the model.
func (w *workspace) view(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(230).FillHeight().Padding(10).Gap(2).
			BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
			ui.Text(c, "JustCli · Go POC").FontSize(12).Bold().TextColor(t.TextMuted)
			ui.Textf(c, "%s", socketPath()).FontSize(9).TextColor(t.TextMuted)
			if ui.Button(c, "+ New Tab").Clicked() {
				w.newTab(nil, "")
			}
			for _, tb := range w.Tabs {
				w.tabRow(c, tb)
			}
			ui.Spacer(c)
			ui.Textf(c, "%d tab(s) · %d panes", len(w.Tabs), len(w.allPanes())).
				FontSize(10).TextColor(t.TextMuted)
		})

		ui.Column(c).Fill().Children(func() {
			w.toolbar(c)
			if tb := w.selected(); tb != nil {
				w.splitView(c, tb.Root)
			} else {
				ui.Column(c).Fill().Center().Children(func() {
					ui.Text(c, "No tabs").FontSize(13).TextColor(t.TextMuted)
				})
			}
		})
	})
}

func (w *workspace) tabRow(c *ui.Context, tb *tab) {
	t := c.Theme()
	sel := tb.ID == w.SelectedTab
	row := ui.Row(c).Padding(7).Gap(8)
	if sel {
		row = row.Background(t.Selection)
	}
	row.OnClick(func() { w.selectTab(tb.ID) }).Children(func() {
		ui.Text(c, tb.title()).FontSize(12)
		ui.Spacer(c)
		ui.Textf(c, "%d", len(tb.Root.panes())).FontSize(10).TextColor(t.TextMuted)
	})
}

func (w *workspace) toolbar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(34).Padding(8).Gap(10).
		BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		if tb := w.selected(); tb != nil {
			ui.Text(c, tb.title()).FontSize(11.5).Bold()
		}
		ui.Spacer(c)
		if ui.Button(c, "New Tab").Clicked() {
			w.newTab(nil, "")
		}
	})
}

// splitView mirrors JustCli's recursive split tree. mygo's SplitPercent
// gives the same draggable divider.
func (w *workspace) splitView(c *ui.Context, n *splitNode) {
	if n == nil {
		return
	}
	if n.isLeaf() {
		w.paneView(c, n.pane)
		return
	}
	if n.axis == axisHorizontal {
		ui.SplitPercent(c, &n.ratio,
			func() { w.splitView(c, n.first) },
			func() { w.splitView(c, n.second) },
		).Fill()
	} else {
		ui.SplitVerticalPercent(c, &n.ratio,
			func() { w.splitView(c, n.first) },
			func() { w.splitView(c, n.second) },
		).Fill()
	}
}

func (w *workspace) paneView(c *ui.Context, p *pane) {
	t := c.Theme()
	focused := p.ID == w.FocusedPane
	ui.Column(c).Fill().Children(func() {
		title := p.title()
		if p.exited {
			title += " · exited"
		}
		if w.settings.ShowHeaders {
			ui.Row(c).Height(28).Padding(8).Gap(8).
				BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
				if focused {
					ui.Text(c, title).FontSize(11).Bold()
				} else {
					ui.Text(c, title).FontSize(11).TextColor(t.TextMuted)
				}
				ui.Text(c, abbreviate(p.dir())).FontSize(10).TextColor(t.TextMuted)
				ui.Spacer(c)
				if ui.Button(c, "◫").Width(26).Clicked() {
					_, _ = w.splitPane(p, axisHorizontal, nil, p.dir())
				}
				if ui.Button(c, "⬒").Width(26).Clicked() {
					_, _ = w.splitPane(p, axisVertical, nil, p.dir())
				}
				if ui.Button(c, "×").Width(26).Clicked() {
					w.closePane(p)
				}
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

var _ = fmt.Sprintf
