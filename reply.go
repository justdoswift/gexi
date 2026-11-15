package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"github.com/justdoswift/gexi/locales"
)

// The reply reader shows the last complete answer an agent handed the pane,
// typeset as markdown in a column of its own. It never reads the terminal:
// the hooks already carry the assistant's words, so the reader works without
// scraping a screen the agent is still drawing.
//
// Claude Code is the first agent this covers. Its Stop hook reports
// last_assistant_message, which eventcmd.go passes on as Message. Other agents
// keep their words in their own formats; they are not guessed at here.

const (
	replyWidthDefault float32 = 620
	replyWidthMin     float32 = 420
	replyWidthMax     float32 = 900
	// replyMaxBytes caps what a reader will lay out. A hook payload can hold
	// megabytes; a single reply that large would stall the window, and the
	// end of such an answer is rarely what the user is reading for.
	replyMaxBytes = 200 << 10
	// replyMonoFont is the terminal's family with the same fallbacks, so code
	// in a reply reads like code in the pane.
	replyMonoFont = terminalFont + ", PingFang SC, SF Mono, Menlo, monospace"
)

// paneReply is one assistant message, kept with the pane it belongs to.
type paneReply struct {
	Agent     agentKind
	Text      string
	At        time.Time
	Truncated bool
	// doc is the parsed markdown, made when the reader first shows this reply
	// and kept for the frames after it.
	doc    ast.Node
	source []byte
}

// document parses the reply's markdown once.
func (r *paneReply) document() (ast.Node, []byte) {
	if r == nil {
		return nil, nil
	}
	if r.doc == nil {
		r.source = []byte(r.Text)
		r.doc = replyMarkdown.Parser().Parse(text.NewReader(r.source))
	}
	return r.doc, r.source
}

var replyMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// setReply keeps the assistant text a hook reported. An empty message leaves
// the previous reply alone: a turn that only ran tools has no new words, and
// the last answer is still what the pane can show.
func (p *pane) setReply(agent agentKind, text string) {
	if p == nil || text == "" {
		return
	}
	truncated := false
	if len(text) > replyMaxBytes {
		text = text[:replyMaxBytes]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		truncated = true
	}
	p.reply = &paneReply{
		Agent:     agent,
		Text:      text,
		At:        time.Now(),
		Truncated: truncated,
	}
}

// replyState is the reader column, open over the canvas the way the files
// drawer is. pane and reply are the snapshot it shows, so a new turn ending
// behind it does not move the text under the reader's eyes.
type replyState struct {
	open    bool
	closing bool
	shown   bool
	width   float32
	pane    *pane
	reply   *paneReply
	// gen restarts the reader's scroll when a different reply opens.
	gen int
}

func (r *replyState) widthValue() float32 {
	if r.width <= 0 {
		return replyWidthDefault
	}
	return r.width
}

// replyChipLabel names the reader in the pane's controls. Markdown is what
// the column shows, and two letters keep the control as small as the icons
// beside it.
const replyChipLabel = "MD"

// replyChip is the pane header's way into the reader, beside the worktree
// button it belongs with. It follows the other controls: shown while the
// pane has the focus or the pointer is over it.
func (w *workspace) replyChip(c *ui.Context, tk tokens, p *pane) {
	hint := locales.PaneReplyHint(p.reply.Agent.name())
	el := ui.Row(c).Height(22).Padding(0, 7).Radius(chromeRadius).Center().
		Border(1, ui.Transparent).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label(hint).Tooltip(hint)
	if el.Hovered() {
		el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
	}
	el.OnClick(func() { w.toggleReply(p) }).Children(func() {
		ui.Text(c, replyChipLabel).FontSize(11).FontWeight(600).
			TextColor(tk.MutedForeground).IgnoreInput(true)
	})
}

// openReply shows p's reply. It returns false when the pane has no reply to
// show, which is the normal state of a pane that never ran Claude Code.
func (w *workspace) openReply(p *pane) bool {
	if p == nil || p.remote != nil || p.reply == nil || strings.TrimSpace(p.reply.Text) == "" {
		return false
	}
	// The files drawer and the reader are both right-hand columns; showing
	// both would stack one over the other.
	if w.drawer.open || w.drawer.closing {
		w.closeDrawer()
	}
	r := &w.reply
	r.open = true
	r.closing = false
	r.shown = false
	r.pane = p
	r.reply = p.reply
	r.gen++
	w.invalidate()
	return true
}

// toggleReply opens the reader, or closes it when it already shows p. A pane
// with no reply says so instead of opening an empty column.
func (w *workspace) toggleReply(p *pane) {
	if w.reply.open && w.reply.pane == p {
		w.closeReply()
		return
	}
	if !w.openReply(p) {
		w.flash(locales.ReplyNone())
	}
}

// closeReply slides the reader out and forgets what it showed once the
// animation is over.
func (w *workspace) closeReply() {
	r := &w.reply
	if !r.open {
		return
	}
	r.open = false
	r.closing = true
	w.invalidate()
	time.AfterFunc(sidebarMotion, func() {
		w.onMain(func() {
			if w.reply.open {
				return
			}
			w.reply.closing = false
			w.reply.shown = false
			w.reply.pane = nil
			w.reply.reply = nil
			w.invalidate()
		})
	})
}

// replyView is the reader column itself. Like the drawer it is built only
// while it opens, shows, or slides out.
func (w *workspace) replyView(c *ui.Context, tk tokens) {
	r := &w.reply
	if !r.open && !r.closing {
		return
	}
	// The pane the snapshot belongs to can go away while the reader is open:
	// closing it, or leaving its session, makes the old answer stop being
	// what the window is about.
	if r.open && (r.pane == nil || w.paneByID(r.pane.ID) != r.pane ||
		w.sessionOf(r.pane) != w.selected()) {
		w.closeReply()
		if !r.closing {
			return
		}
	}
	el := ui.Column(c).Absolute().Top(0).Bottom(0).Right(0)
	width := el.Animate("replyWidth", r.widthValue(), sidebarMotion)
	// A reply is its own surface, as the drawer is: the theme's background and
	// a hairline, not glass over the terminal.
	el = el.Width(width).Background(tk.Background).
		BorderWidth(0, 0, 0, 1).BorderColor(tk.Border).Clip()
	if el.OverlayShortcut(0, ui.KeyEscape) {
		w.closeReply()
	}
	pose := ui.Pose{}
	if r.closing {
		pose = ui.Pose{X: 32, Fade: 1}
	} else if !r.shown {
		// The first frame starts it off the edge and transparent; the next
		// frame lets it slide and fade in, as the drawer does.
		r.shown = true
		pose = ui.Pose{X: 32, Fade: 1}
		c.After(time.Millisecond)
	}
	el = el.AnimatePose(pose, ui.Animation{Duration: sidebarMotion, Ease: ui.EaseOut})
	el.Children(func() {
		w.replyResizer(c, tk)
		w.replyHeader(c, tk)
		if r.reply != nil {
			w.replyBody(c, tk, r.reply)
		}
	})
}

// replyResizer is the reader's left edge, dragged to widen or narrow it for
// this session. It starts at the width the drawer uses for a file.
func (w *workspace) replyResizer(c *ui.Context, tk tokens) {
	edge := ui.Box(c).Absolute().Left(-3).Top(0).Width(6).FillHeight().
		Cursor(ui.CursorResizeEW)
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		next := w.reply.widthValue() - dx
		if next < replyWidthMin {
			next = replyWidthMin
		}
		if next > replyWidthMax {
			next = replyWidthMax
		}
		w.reply.width = next
		w.invalidate()
	}
}

// replyHeader names the agent and the reply, with copy and close on the right.
func (w *workspace) replyHeader(c *ui.Context, tk tokens) {
	r := &w.reply
	ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Padding(0, 8, 0, 6).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Shrink(1).Children(func() {
			if r.reply != nil {
				if src := agentLogoSource(r.reply.Agent, tk); src != nil {
					ui.Image(c, src).Width(13).Height(13)
				}
				title := locales.ReplyTitle(r.reply.Agent.name())
				ui.Text(c, title).FontSize(12.5).FontWeight(500).
					SingleLine().Shrink(1).TextColor(tk.Foreground)
				ui.Text(c, r.reply.At.Format("15:04")).FontSize(10.5).
					SingleLine().TextColor(tk.MutedForeground)
			}
		})
		ui.Spacer(c)
		if r.reply != nil {
			w.replyCopyButton(c, tk, r.reply.Text)
		}
		iconButtonInFrame(c, tk, "x", func() { w.closeReply() }, locales.ReplyClose())
	})
}

// replyCopyButton puts the reply's markdown on the clipboard, which is more
// useful than copying its typeset words.
func (w *workspace) replyCopyButton(c *ui.Context, tk tokens, text string) {
	el := ui.Row(c).Height(22).Padding(0, 8).Radius(5).Gap(6).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).
		Label(locales.ReplyCopy())
	if el.Hovered() {
		el = el.Background(tk.Control.Alpha(0.2))
	}
	el.OnClick(func() {
		mygo.Clipboard.WriteText(text)
		w.flash(locales.ReplyCopied())
	}).Children(func() {
		ui.Text(c, locales.ReplyCopy()).FontSize(11).FontWeight(500).
			TextColor(tk.MutedForeground)
	})
}

// replyBody is the scrolling column of typeset markdown. The text is capped
// to a readable measure even when the reader is dragged wide.
func (w *workspace) replyBody(c *ui.Context, tk tokens, r *paneReply) {
	doc, source := r.document()
	ui.Scroll(c).Fill().Key(w.reply.gen).Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(680).Padding(24, 28, 40, 28).
				Selectable().Children(func() {
				if doc == nil {
					ui.Text(c, locales.ReplyEmpty()).FontSize(13).
						TextColor(tk.MutedForeground)
					return
				}
				replyBlocks(c, tk, source, doc, replyStyle{
					size: 13, color: tk.Foreground,
				})
				if r.Truncated {
					ui.Text(c, locales.ReplyTruncated()).FontSize(11).
						Margin(12, 0, 0, 0).TextColor(tk.MutedForeground)
				}
			})
		})
	})
}

// replyStyle is how inline text reads inside one block. Zero fields inherit
// from the RichText around them.
type replyStyle struct {
	size      float32
	weight    int
	italic    bool
	color     ui.Color
	underline bool
	strike    bool
	code      bool
	codeBg    ui.Color
}

// replyBlocks lays out the block children of n in order.
func replyBlocks(c *ui.Context, tk tokens, source []byte, n ast.Node, base replyStyle) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		replyBlock(c, tk, source, child, base)
	}
}

func replyBlock(c *ui.Context, tk tokens, source []byte, n ast.Node, base replyStyle) {
	switch n := n.(type) {
	case *ast.Heading:
		replyHeading(c, tk, source, n)
	case *ast.Paragraph:
		replyParagraph(c, tk, source, n, base)
	case *ast.TextBlock:
		replyParagraph(c, tk, source, n, base)
	case *ast.List:
		replyList(c, tk, source, n, base)
	case *ast.Blockquote:
		replyBlockquote(c, tk, source, n, base)
	case *ast.FencedCodeBlock:
		replyCode(c, tk, source, n, string(n.Language(source)))
	case *ast.CodeBlock:
		replyCode(c, tk, source, n, "")
	case *ast.ThematicBreak:
		ui.Box(c).FillWidth().Height(1).Background(tk.Border).Margin(12, 0, 2, 0)
	case *ast.HTMLBlock:
		replyRawHTML(c, tk, source, n)
	case *gast.Table:
		replyTable(c, tk, source, n)
	default:
		if n.HasChildren() {
			replyBlocks(c, tk, source, n, base)
		}
	}
}

// replyHeading is a heading at one of the reader's three sizes, with room
// above it so sections separate without a rule.
func replyHeading(c *ui.Context, tk tokens, source []byte, n *ast.Heading) {
	size, weight, top := float32(13.5), 600, float32(10)
	switch n.Level {
	case 1:
		size, top = 21, 6
	case 2:
		size, top = 17, 16
	case 3:
		size, top = 14.5, 13
	}
	el := ui.RichText(c).FontSize(size).FontWeight(weight).
		TextColor(tk.Foreground).LineHeight(1.35).Margin(top, 0, 2, 0)
	el.Children(func() {
		replyInlineChildren(c, tk, source, n, replyStyle{
			size: size, weight: weight, color: tk.Foreground,
		})
	})
}

// replyParagraph is running text: a paragraph, or the tight item of a list.
func replyParagraph(c *ui.Context, tk tokens, source []byte, n ast.Node, base replyStyle) {
	if !n.HasChildren() {
		return
	}
	style := base
	if style.size == 0 {
		style.size = 13
	}
	if style.color.A == 0 {
		style.color = tk.Foreground
	}
	el := ui.RichText(c).FontSize(style.size).TextColor(style.color).
		LineHeight(1.55)
	if style.weight > 0 {
		el.FontWeight(style.weight)
	}
	el.Children(func() {
		replyInlineChildren(c, tk, source, n, style)
	})
}

// listTask reports whether an item opens with a GFM task box, and whether
// that box is checked. The box is the item's marker instead of its bullet.
func listTask(li *ast.ListItem) (checked, ok bool) {
	block := li.FirstChild()
	if block == nil {
		return false, false
	}
	box, ok := block.FirstChild().(*gast.TaskCheckBox)
	if !ok {
		return false, false
	}
	return box.IsChecked, true
}

// replyList indents each item under its marker. A nested list is a block in
// the item's own column, so its markers step in with the content.
func replyList(c *ui.Context, tk tokens, source []byte, n *ast.List, base replyStyle) {
	ordered := n.IsOrdered()
	number := n.Start
	list := ui.Column(c).Gap(4).Margin(2, 0, 4, 0)
	list.Children(func() {
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			li, ok := item.(*ast.ListItem)
			if !ok {
				continue
			}
			row := ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(8)
			row.Children(func() {
				marker := ui.Column(c).Width(18).Shrink(0).AlignItems(ui.End)
				marker.Children(func() {
					if checked, task := listTask(li); task {
						// A task's box is its marker; the checkbox itself
						// is not drawn again in the text.
						label, color := "○", tk.MutedForeground
						if checked {
							label, color = "✓", tk.Working
						}
						ui.Text(c, label).FontSize(13).TextColor(color)
						return
					}
					label := "•"
					if ordered {
						label = fmt.Sprintf("%d.", number)
						number++
					}
					ui.Text(c, label).FontSize(13).TextColor(tk.MutedForeground)
				})
				content := ui.Column(c).Grow(1).Shrink(1).Gap(4)
				content.Children(func() {
					replyBlocks(c, tk, source, li, base)
				})
			})
		}
	})
}

// replyBlockquote sets its contents off with a vertical rule and the quieter
// text colour, as a quotation reads on the web.
func replyBlockquote(c *ui.Context, tk tokens, source []byte, n *ast.Blockquote, base replyStyle) {
	quote := base
	quote.color = tk.MutedForeground
	ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(12).Margin(4, 0, 6, 0).
		Children(func() {
			ui.Box(c).Width(2).Background(tk.Control).AlignSelf(ui.Stretch)
			ui.Column(c).Grow(1).Shrink(1).Gap(8).Children(func() {
				replyBlocks(c, tk, source, n, quote)
			})
		})
}

// replyCode is a fenced or indented code block: its language, when it has
// one, over a monospaced surface that keeps blank lines and leading spaces.
func replyCode(c *ui.Context, tk tokens, source []byte, n ast.Node, language string) {
	value := ""
	if lines := n.Lines(); lines != nil && lines.Len() > 0 {
		value = string(lines.Value(source))
	} else if text, ok := n.(*ast.FencedCodeBlock); ok {
		value = string(text.Text(source))
	} else if text, ok := n.(*ast.CodeBlock); ok {
		value = string(text.Text(source))
	}
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return
	}
	ui.Column(c).FillWidth().Radius(6).Background(tk.Muted).
		Border(1, tk.Border).Padding(10, 12).Margin(4, 0, 6, 0).
		Children(func() {
			if lang := strings.TrimSpace(language); lang != "" {
				ui.Text(c, lang).FontSize(10.5).TextColor(tk.MutedForeground).
					Margin(0, 0, 6, 0)
			}
			for _, line := range strings.Split(value, "\n") {
				if line == "" {
					// An empty Text measures nothing; a space keeps the
					// line's height, and the gap inside the code.
					line = " "
				}
				ui.Text(c, line).Font(replyMonoFont).FontSize(12).
					TextColor(tk.Foreground).FixedLineHeight(18)
			}
		})
}

// replyTable draws a GFM table as a static grid: equal columns, a tinted
// header, and hairlines between cells. It does not scroll on its own; the
// reader's column is the one scroll.
func replyTable(c *ui.Context, tk tokens, source []byte, n *gast.Table) {
	var rows [][]*gast.TableCell
	add := func(row ast.Node) {
		var cells []*gast.TableCell
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			if tc, ok := cell.(*gast.TableCell); ok {
				cells = append(cells, tc)
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.(type) {
		case *gast.TableHeader, *gast.TableRow:
			add(child)
		}
	}
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	aligns := n.Alignments
	ui.Column(c).FillWidth().Radius(6).Border(1, tk.Border).Clip().
		Margin(4, 0, 6, 0).Children(func() {
		for i, row := range rows {
			header := i == 0
			line := ui.Row(c).FillWidth().AlignItems(ui.Stretch)
			if header {
				line = line.Background(tk.Muted)
			}
			if i < len(rows)-1 {
				line = line.BorderWidth(0, 0, 1, 0).BorderColor(tk.Border)
			}
			line.Children(func() {
				for j := 0; j < cols; j++ {
					cell := ui.Column(c).Grow(1).Shrink(1).Padding(7, 10)
					if j < cols-1 {
						cell = cell.BorderWidth(0, 1, 0, 0).BorderColor(tk.Border)
					}
					var tc *gast.TableCell
					if j < len(row) {
						tc = row[j]
					}
					cell.Children(func() {
						if tc == nil {
							return
						}
						size, weight := float32(12.5), 0
						if header {
							weight = 600
						}
						el := ui.RichText(c).FontSize(size).LineHeight(1.45).
							TextColor(tk.Foreground)
						if weight > 0 {
							el.FontWeight(weight)
						}
						switch {
						case tc.Alignment == gast.AlignCenter || (tc.Alignment == gast.AlignNone && j < len(aligns) && aligns[j] == gast.AlignCenter):
							el.TextAlign(ui.Center)
						case tc.Alignment == gast.AlignRight || (tc.Alignment == gast.AlignNone && j < len(aligns) && aligns[j] == gast.AlignRight):
							el.TextAlign(ui.End)
						}
						el.Children(func() {
							replyInlineChildren(c, tk, source, tc, replyStyle{
								size: size, weight: weight, color: tk.Foreground,
							})
						})
					})
				}
			})
		}
	})
}

// replyRawHTML keeps raw markup visible as text. The reader has no HTML
// engine, and a line of markup is more honest than dropping it.
func replyRawHTML(c *ui.Context, tk tokens, source []byte, n *ast.HTMLBlock) {
	var b strings.Builder
	if lines := n.Lines(); lines != nil {
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			b.Write(seg.Value(source))
		}
	}
	value := strings.TrimSpace(b.String())
	if value == "" {
		return
	}
	ui.Text(c, value).Font(replyMonoFont).FontSize(11.5).
		TextColor(tk.MutedForeground).FixedLineHeight(17).Margin(2, 0, 4, 0)
}

// replyInlineChildren emits the inline content of a block into the RichText
// that is being built.
func replyInlineChildren(c *ui.Context, tk tokens, source []byte, n ast.Node, style replyStyle) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		replyInline(c, tk, source, child, style)
	}
}

func replyInline(c *ui.Context, tk tokens, source []byte, n ast.Node, style replyStyle) {
	switch n := n.(type) {
	case *ast.Text:
		value := string(n.Segment.Value(source))
		if n.SoftLineBreak() {
			value += " "
		}
		if n.HardLineBreak() {
			value += "\n"
		}
		replyInlineText(c, value, style)
	case *ast.String:
		replyInlineText(c, string(n.Value), style)
	case *ast.CodeSpan:
		code := style
		code.code = true
		code.codeBg = tk.Control.Alpha(0.14)
		code.size = 12.5
		code.color = tk.Foreground
		replyInlineText(c, string(n.Text(source)), code)
	case *ast.Emphasis:
		next := style
		if n.Level >= 2 {
			next.weight = 600
		} else {
			next.italic = true
		}
		replyInlineChildren(c, tk, source, n, next)
	case *ast.Link:
		link := ui.Link(c, "", string(n.Destination)).
			Tooltip(string(n.Destination))
		link.Children(func() {
			next := style
			next.underline = true
			next.color = ui.Color{}
			replyInlineChildren(c, tk, source, n, next)
		})
	case *ast.AutoLink:
		url := string(n.URL(source))
		label := string(n.Label(source))
		if label == "" {
			label = url
		}
		link := ui.Link(c, "", url).Tooltip(url)
		link.Children(func() {
			next := style
			next.underline = true
			next.color = ui.Color{}
			replyInlineText(c, label, next)
		})
	case *ast.Image:
		// No image engine: keep the alt text and the URL, which is what the
		// text was carrying anyway.
		alt := style
		alt.italic = true
		replyInlineChildren(c, tk, source, n, alt)
		if dest := string(n.Destination); dest != "" {
			replyInlineText(c, " (", style)
			link := ui.Link(c, "", dest).Tooltip(dest)
			link.Children(func() {
				next := style
				next.underline = true
				next.color = ui.Color{}
				replyInlineText(c, dest, next)
			})
			replyInlineText(c, ")", style)
		}
	case *gast.TaskCheckBox:
		// The list item's marker carries the box; drawing it again would
		// read as two checkboxes.
		return
	case *ast.RawHTML:
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			replyInlineText(c, string(seg.Value(source)), style)
		}
	default:
		if n.HasChildren() {
			replyInlineChildren(c, tk, source, n, style)
		}
	}
}

func replyInlineText(c *ui.Context, value string, style replyStyle) {
	if value == "" {
		return
	}
	el := ui.Text(c, value)
	if style.size > 0 {
		el.FontSize(style.size)
	}
	if style.weight > 0 {
		el.FontWeight(style.weight)
	}
	if style.italic {
		el.Italic()
	}
	if style.color.A > 0 {
		el.TextColor(style.color)
	}
	if style.underline {
		el.Underline()
	}
	if style.strike {
		el.Strikethrough()
	}
	if style.code {
		el.Font(replyMonoFont)
		if style.codeBg.A > 0 {
			el.TextBackground(style.codeBg)
		}
	}
}
