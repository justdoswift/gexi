package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
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

// The reply reader shows the conversation an agent and the user had in the
// pane — what the user said and what the agent answered — typeset as markdown
// in a column of its own. Tool calls, their output, and the agent's working
// notes stay out of it: the pane's terminal already shows all of that.
//
// It never reads the terminal: Claude Code's UserPromptSubmit hook carries the
// prompt and its Stop hook reports last_assistant_message, which eventcmd.go
// passes on as Prompt and Message; Amp's plugin reports both ends the same way.
// Other agents keep their words in their own formats; they are not guessed at
// here. The log lives in memory, so a pane that resumes a conversation older
// than the window starts the reader at the words said since it opened.

const (
	replyWidthDefault float32 = 620
	replyWidthMin     float32 = 420
	// replyWidthMax lets the reader take almost the whole window: the text
	// keeps its measure, so a wide column is air around the words, not a
	// longer line to read.
	replyWidthMax float32 = 1400
	// replyMaxBytes caps one message the reader will lay out. A hook payload
	// can hold megabytes; a single message that large would stall the window,
	// and the end of such a message is rarely what the user is reading for.
	replyMaxBytes = 200 << 10
	// conversationMaxBytes caps the log as a whole. A session left running is
	// more prompts and answers than anyone scrolls back through, so the
	// oldest turns go first.
	conversationMaxBytes = 400 << 10
	// replyMonoFont is the terminal's family with the same fallbacks, so code
	// in a reply reads like code in the pane.
	replyMonoFont = terminalFont + ", PingFang SC, SF Mono, Menlo, monospace"
)

// paneMessage is one side of a conversation, kept with the pane it belongs to.
// Session is the conversation that said it, so a reconnect of the same one
// does not throw the log away.
type paneMessage struct {
	// FromUser marks the user's own words rather than the agent's answer.
	FromUser  bool
	Agent     agentKind
	Session   string
	Text      string
	At        time.Time
	Truncated bool
	// doc is the parsed markdown, made when the reader first shows this
	// message and kept for the frames after it.
	doc    ast.Node
	source []byte
}

// document parses the message's markdown once.
func (m *paneMessage) document() (ast.Node, []byte) {
	if m == nil {
		return nil, nil
	}
	if m.doc == nil {
		m.source = []byte(m.Text)
		m.doc = replyMarkdown.Parser().Parse(text.NewReader(m.source))
	}
	return m.doc, m.source
}

var replyMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// agentTalks reports whether an agent reports both sides of its conversation,
// the prompt the user sent and the answer the turn ended with. The others
// report no words at all, and there is nothing to show for them.
func agentTalks(k agentKind) bool {
	return k == agentClaude || k == agentAmp
}

// addUserMessage keeps what the user said to start a turn.
func (p *pane) addUserMessage(agent agentKind, session, text string) {
	p.addMessage(paneMessage{FromUser: true, Agent: agent, Session: session, Text: text})
}

// addReplyMessage keeps the answer a turn ended with.
func (p *pane) addReplyMessage(agent agentKind, session, text string) {
	p.addMessage(paneMessage{Agent: agent, Session: session, Text: text})
}

// addMessage appends one side of the conversation. A message with no words
// adds nothing: a turn that only ran tools has no new words, and the
// conversation before it is still what the pane can show. An agent other than
// the one the log holds is a new conversation, so its first words start one.
func (p *pane) addMessage(m paneMessage) {
	if p == nil || strings.TrimSpace(m.Text) == "" {
		return
	}
	if m.FromUser && len(p.conversation) > 0 {
		// Two sources can report one prompt: Amp names a turn from the
		// thread's last message while its start hook reports the same
		// words, a hook installed twice runs twice, and a reloaded plugin
		// leaves its old copy reporting beside the new one. The reports
		// can land minutes apart and need not space their words the same
		// way. The user said it once: a prompt repeating the last one,
		// with no answer since, is that second report.
		if last := p.conversation[len(p.conversation)-1]; last.FromUser &&
			last.Agent == m.Agent &&
			(last.Session == "" || m.Session == "" || last.Session == m.Session) &&
			strings.TrimSpace(last.Text) == strings.TrimSpace(m.Text) {
			return
		}
	}
	if agent := p.conversationAgent(); agent != "" && agent != m.Agent {
		p.clearConversation()
	}
	m.Text, m.Truncated = capMessage(m.Text, replyMaxBytes)
	m.At = time.Now()
	p.conversation = append(p.conversation, m)
	p.trimConversation()
}

// conversationAgent names the agent whose words the log holds.
func (p *pane) conversationAgent() agentKind {
	if p == nil || len(p.conversation) == 0 {
		return ""
	}
	return p.conversation[len(p.conversation)-1].Agent
}

// clearConversation forgets the log, for a pane that starts another one.
func (p *pane) clearConversation() {
	p.conversation = nil
	p.conversationTrimmed = false
}

// conversationStale reports that the log belongs to another conversation of
// the same agent: a session that is known on both sides and differs, or a
// session this log never learned. Only the agent that owns the log can clear
// it, so a stray announcement from another one leaves it alone.
func (p *pane) conversationStale(agent agentKind, session string) bool {
	if len(p.conversation) == 0 {
		return false
	}
	last := p.conversation[len(p.conversation)-1]
	if last.Agent != agent {
		return false
	}
	return last.Session == "" || session == "" || last.Session != session
}

// capMessage cuts a message to max bytes on a rune boundary.
func capMessage(text string, max int) (string, bool) {
	if len(text) <= max {
		return text, false
	}
	text = text[:max]
	for len(text) > 0 && !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text, true
}

// trimConversation drops the oldest turns once the log is past its cap.
func (p *pane) trimConversation() {
	total := 0
	for _, m := range p.conversation {
		total += len(m.Text)
	}
	drop := 0
	for total > conversationMaxBytes && drop < len(p.conversation)-1 {
		total -= len(p.conversation[drop].Text)
		drop++
	}
	if drop == 0 {
		return
	}
	p.conversation = append([]paneMessage(nil), p.conversation[drop:]...)
	p.conversationTrimmed = true
}

// replyReaderSize is the size the conversation is read at: the terminal's
// own, so the reader and the pane it belongs to are set in the same type.
func (w *workspace) replyReaderSize() float32 {
	return w.terminalFontSize()
}

// replyReaderFont is the family the conversation is read in: the terminal's,
// so a reader's prose and the pane's output are the same letterforms.
func (w *workspace) replyReaderFont() string {
	return w.terminalTextFont().Family
}

// replyMeasureFor is how wide the conversation reads at size: about 49
// characters, short enough to follow a line back to its start.
func replyMeasureFor(size float32) float32 {
	measure := size * 49
	if measure > 980 {
		measure = 980
	}
	return measure
}

// replyState is the reader column, open over the canvas the way the files
// drawer is. msgs is the log as it stood when the reader opened, so a turn
// ending behind it does not move the text under the reader's eyes.
type replyState struct {
	open    bool
	closing bool
	shown   bool
	width   float32
	pane    *pane
	agent   agentKind
	msgs    []paneMessage
	trimmed bool
	// gen restarts the reader's scroll when a different conversation opens.
	gen int
	// toEnd asks the column to open on its newest turn, which is what the
	// user came to read; scroll is where it shows the text.
	toEnd  bool
	scroll ui.ScrollState
	// wide fills the window with the reader, for reading a long answer
	// without the panes beside it.
	wide bool
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
	hint := locales.PaneReplyHint(p.conversationAgent().name())
	el := ui.Row(c).Height(22).Padding(0, 7).Radius(chromeRadius).Center().
		Border(1, ui.Transparent).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label(hint).Tooltip(hint)
	el = badge(el, tk, el.Hovered())
	el.OnClick(func() { w.toggleReply(p) }).Children(func() {
		ui.Text(c, replyChipLabel).FontSize(11).FontWeight(600).
			TextColor(tk.MutedForeground).IgnoreInput(true)
	})
}

// openReply shows p's conversation. It returns false when the pane has no
// words to show, which is the normal state of a pane the user has not talked
// to, or whose agent reports none.
func (w *workspace) openReply(p *pane) bool {
	if p == nil || p.remote != nil || len(p.conversation) == 0 {
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
	r.agent = p.conversationAgent()
	// The messages are copied so the column keeps the log it opened with; a
	// markdown document already parsed travels with the copy.
	r.msgs = append([]paneMessage(nil), p.conversation...)
	r.trimmed = p.conversationTrimmed
	r.toEnd = true
	r.scroll = ui.ScrollState{}
	r.gen++
	w.invalidate()
	return true
}

// toggleReply opens the reader, or closes it when it already shows p. A pane
// with nothing said in it says so instead of opening an empty column.
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
			w.reply.agent = ""
			w.reply.msgs = nil
			w.reply.trimmed = false
			w.reply.toEnd = false
			w.reply.scroll = ui.ScrollState{}
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
	// closing it, or leaving its session, makes the old conversation stop
	// being what the window is about.
	if r.open && (r.pane == nil || w.paneByID(r.pane.ID) != r.pane ||
		w.sessionOf(r.pane) != w.selected()) {
		w.closeReply()
		if !r.closing {
			return
		}
	}
	c.Key("reply")
	el := ui.Column(c).Absolute().Top(0).Bottom(0).Right(0)
	// A reply is its own surface, as the drawer is: the theme's background and
	// a hairline, not glass over the terminal.
	if r.wide {
		el = el.WidthPercent(100)
	} else {
		el = el.Width(el.Animate("replyWidth", r.widthValue(), sidebarMotion))
	}
	el = sheet(el.Background(tk.Background).BorderWidth(0, 0, 0, 1).BorderColor(tk.Border), tk)
	el = el.Clip()
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
		w.replyBody(c, tk)
	})
}

// replyResizer is the reader's left edge, dragged to widen or narrow it for
// this session. It starts at the width the drawer uses for a file.
func (w *workspace) replyResizer(c *ui.Context, tk tokens) {
	edge := ui.Box(c).Absolute().Left(-3).Top(0).Width(6).FillHeight().
		Cursor(ui.CursorResizeEW)
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		// Dragging the edge is asking for a width again, not for the window.
		w.reply.wide = false
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

// replyHeader names the agent and whose conversation this is, with copy and
// close on the right.
func (w *workspace) replyHeader(c *ui.Context, tk tokens) {
	r := &w.reply
	// The logo sits at 9 so the title's 28 is the same rail the conversation
	// hangs from; the panel reads as one column, header included.
	ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Padding(0, 8, 0, 9).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Shrink(1).Children(func() {
			if len(r.msgs) > 0 {
				if src := agentLogoSource(r.agent, tk); src != nil {
					ui.Image(c, src).Width(13).Height(13)
				}
				title := locales.ReplyTitle(r.agent.name())
				ui.Text(c, title).FontSize(12).FontWeight(500).
					SingleLine().Shrink(1).TextColor(tk.Foreground)
				meta := locales.ReplyMeta(strconv.Itoa(len(r.msgs)))
				ui.Text(c, meta).FontSize(10.5).SingleLine().
					TextColor(tk.MutedForeground)
			}
		})
		ui.Spacer(c)
		if len(r.msgs) > 0 {
			w.replyCopyButton(c, tk, conversationMarkdown(r.msgs))
		}
		w.replyWideButton(c, tk)
		iconButtonInFrame(c, tk, "x", func() { w.closeReply() }, locales.ReplyClose())
	})
}

// replyWideButton fills the window with the reader, and gives the panes back
// when the answer is read.
func (w *workspace) replyWideButton(c *ui.Context, tk tokens) {
	r := &w.reply
	name, hint := "expand", locales.ReplyExpand()
	if r.wide {
		name, hint = "collapse", locales.ReplyCollapse()
	}
	iconButtonInFrame(c, tk, name, func() {
		r.wide = !r.wide
		w.invalidate()
	}, hint)
}

// conversationMarkdown is the whole conversation as one document, each side
// named, so a copy reads as a transcript rather than as loose prose.
func conversationMarkdown(msgs []paneMessage) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		name := m.Agent.name()
		if m.FromUser {
			name = locales.ReplyUser()
		}
		b.WriteString("**" + name + "**\n\n")
		b.WriteString(strings.TrimSpace(m.Text))
	}
	return b.String()
}

// replyCopyButton puts the conversation's markdown on the clipboard, which is
// more useful than copying its typeset words.
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

// replyBody is the scrolling column of typeset markdown: the user's turns and
// the agent's answers in the order they were said, in the terminal's own type
// and size. The text keeps its measure however wide the reader is dragged.
func (w *workspace) replyBody(c *ui.Context, tk tokens) {
	r := &w.reply
	size, family := w.replyReaderSize(), w.replyReaderFont()
	scroll := ui.Scroll(c).Fill().Key(r.gen).TrackScroll(&r.scroll)
	if r.toEnd && r.scroll.MaxY > 0 {
		// The column knows how tall the conversation is only after it has
		// laid it out, so the scroll to its end waits for that frame.
		r.toEnd = false
		r.scroll.Y = r.scroll.MaxY
	}
	scroll.Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(replyMeasureFor(size)).
				Padding(22, 28, 36, 28).Selectable().Children(func() {
				if len(r.msgs) == 0 {
					ui.Text(c, locales.ReplyEmpty()).Font(family).FontSize(size - 4).
						TextColor(tk.MutedForeground)
					return
				}
				if r.trimmed {
					ui.Text(c, locales.ReplyTrimmed()).Font(family).FontSize(size-4).
						Margin(0, 0, 16, 0).TextColor(tk.MutedForeground)
				}
				for i := range r.msgs {
					w.replyMessage(c, tk, &r.msgs[i], i == 0, size, family)
				}
			})
		})
	})
}

// replyMessage lays out one side of the conversation. What the user said hangs
// on a rule at the left under its own label; the answer hangs on the same rule
// at the right, so the two speakers are told apart at a glance.
func (w *workspace) replyMessage(c *ui.Context, tk tokens, m *paneMessage, first bool, size float32, family string) {
	doc, source := m.document()
	if !m.FromUser {
		if doc == nil {
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				replyBlocks(c, tk, source, doc, replyStyle{
					size: size, family: family, color: tk.Foreground,
				}, true)
				if m.Truncated {
					ui.Text(c, locales.ReplyTruncated()).Font(family).
						FontSize(size-4).Margin(8, 0, 0, 0).
						TextColor(tk.MutedForeground)
				}
			})
			ui.Box(c).Width(2).Radius(1).Background(tk.Control).
				AlignSelf(ui.Stretch)
		})
		return
	}
	top := float32(28)
	if first {
		top = 0
	}
	ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(12).
		Margin(top, 0, 10, 0).Children(func() {
		ui.Box(c).Width(2).Radius(1).Background(tk.Control).
			AlignSelf(ui.Stretch)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			label := locales.ReplyUser() + " · " + m.At.Format("15:04")
			ui.Text(c, label).Font(family).FontSize(size-4.5).FontWeight(500).
				TextColor(tk.MutedForeground).Margin(0, 0, 5, 0)
			if doc == nil {
				return
			}
			replyBlocks(c, tk, source, doc, replyStyle{
				size: size - 0.5, family: family, color: tk.Foreground,
			}, true)
			if m.Truncated {
				ui.Text(c, locales.ReplyTruncated()).Font(family).
					FontSize(size-4).Margin(6, 0, 0, 0).
					TextColor(tk.MutedForeground)
			}
		})
	})
}

// replyStyle is how inline text reads inside one block. Zero fields inherit
// from the RichText around them.
type replyStyle struct {
	size      float32
	family    string
	weight    int
	italic    bool
	color     ui.Color
	underline bool
	strike    bool
	code      bool
	codeBg    ui.Color
}

// replyBlocks lays out the block children of n in order. first marks the one
// that opens a message, which takes the room it needs from the question above
// rather than from the space between turns.
func replyBlocks(c *ui.Context, tk tokens, source []byte, n ast.Node, base replyStyle, first bool) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		replyBlock(c, tk, source, child, base, first)
		first = false
	}
}

func replyBlock(c *ui.Context, tk tokens, source []byte, n ast.Node, base replyStyle, first bool) {
	switch n := n.(type) {
	case *ast.Heading:
		replyHeading(c, tk, source, n, base, first)
	case *ast.Paragraph:
		replyParagraph(c, tk, source, n, base)
	case *ast.TextBlock:
		replyParagraph(c, tk, source, n, base)
	case *ast.List:
		replyList(c, tk, source, n, base)
	case *ast.Blockquote:
		replyBlockquote(c, tk, source, n, base)
	case *ast.FencedCodeBlock:
		replyCode(c, tk, source, n, string(n.Language(source)), base)
	case *ast.CodeBlock:
		replyCode(c, tk, source, n, "", base)
	case *ast.ThematicBreak:
		ui.Box(c).FillWidth().Height(1).Background(tk.Border).Margin(12, 0, 2, 0)
	case *ast.HTMLBlock:
		replyRawHTML(c, tk, source, n)
	case *gast.Table:
		replyTable(c, tk, source, n, base)
	default:
		if n.HasChildren() {
			replyBlocks(c, tk, source, n, base, false)
		}
	}
}

// replyHeading is a heading at one of the reader's three sizes, with room
// above it so sections separate without a rule.
func replyHeading(c *ui.Context, tk tokens, source []byte, n *ast.Heading, base replyStyle, first bool) {
	body := base.size
	if body == 0 {
		body = 13
	}
	size, weight, top := body, 600, float32(12)
	switch n.Level {
	case 1:
		size, top = body+5, 6
	case 2:
		size, top = body+3, 18
	case 3:
		size, top = body+1, 14
	}
	if first {
		// An answer's first heading keeps a hair of room, so it reads as the
		// answer's own title rather than as part of the question above it.
		top = 4
	}
	el := ui.RichText(c).FontSize(size).FontWeight(weight).
		TextColor(tk.Foreground).LineHeight(1.3).Margin(top, 0, 2, 0)
	if base.family != "" {
		el = el.Font(base.family)
	}
	el.Children(func() {
		replyInlineChildren(c, tk, source, n, replyStyle{
			size: size, family: base.family, weight: weight, color: tk.Foreground,
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
		LineHeight(1.6)
	if style.family != "" {
		el = el.Font(style.family)
	}
	if _, tight := n.Parent().(*ast.ListItem); !tight {
		el = el.Margin(0, 0, 8, 0)
	}
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
					replyBlocks(c, tk, source, li, base, false)
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
				replyBlocks(c, tk, source, n, quote, false)
			})
		})
}

// replyCode is a fenced or indented code block: its language, when it has
// one, over a monospaced surface that keeps blank lines and leading spaces.
func replyCode(c *ui.Context, tk tokens, source []byte, n ast.Node, language string, base replyStyle) {
	mono := base.size
	if mono == 0 {
		mono = 13
	}
	mono -= 1.5
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
	ui.Column(c).FillWidth().Radius(chromeRadius).Background(tk.Muted).
		Border(1, tk.Border).Padding(9, 11).Margin(6, 0, 8, 0).
		Children(func() {
			if lang := strings.TrimSpace(language); lang != "" {
				ui.Text(c, lang).Font(replyMonoFont).FontSize(mono-1).
					TextColor(tk.MutedForeground).Margin(0, 0, 6, 0)
			}
			for _, line := range strings.Split(value, "\n") {
				if line == "" {
					// An empty Text measures nothing; a space keeps the
					// line's height, and the gap inside the code.
					line = " "
				}
				ui.Text(c, line).Font(replyMonoFont).FontSize(mono).
					TextColor(tk.Foreground).FixedLineHeight(mono * 1.5)
			}
		})
}

// replyTable draws a GFM table as a static grid: equal columns, a tinted
// header, and hairlines between cells. It does not scroll on its own; the
// reader's column is the one scroll.
func replyTable(c *ui.Context, tk tokens, source []byte, n *gast.Table, base replyStyle) {
	cellSize := base.size
	if cellSize == 0 {
		cellSize = 13
	}
	cellSize--
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
	ui.Column(c).FillWidth().Radius(chromeRadius).Border(1, tk.Border).Clip().
		Margin(6, 0, 8, 0).Children(func() {
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
						size, weight := cellSize, 0
						if header {
							weight = 600
						}
						el := ui.RichText(c).FontSize(size).LineHeight(1.45).
							TextColor(tk.Foreground)
						if base.family != "" {
							el = el.Font(base.family)
						}
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
								size: size, family: base.family, weight: weight,
								color: tk.Foreground,
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
		// One step further from the page than a fenced block, and in the
		// same direction, so code reads as the same material in both
		// appearances instead of floating on top of the page in dark.
		code.codeBg = tk.Muted.MixOklab(ui.RGB(0, 0, 0), 0.04)
		code.size = style.size - 0.5
		code.color = tk.Foreground
		replyInlineText(c, string(n.Text(source)), code)
	case *ast.Emphasis:
		next := style
		switch {
		case n.Level >= 2:
			next.weight = 600
		case replyItalic(source, n):
			next.italic = true
		default:
			// PingFang has no true italic: a shear through Chinese reads as
			// a mistake, so a run with Han in it is emphasized by weight.
			next.weight = 600
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

// replyItalic reports whether an emphasis run reads as italic: one that holds
// Han does not, because the interface font would only slant it.
func replyItalic(source []byte, n *ast.Emphasis) bool {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch t := child.(type) {
		case *ast.Text:
			if hasHan(string(t.Segment.Value(source))) {
				return false
			}
		case *ast.String:
			if hasHan(string(t.Value)) {
				return false
			}
		}
	}
	return true
}

// hasHan reports whether text holds a Han character.
func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func replyInlineText(c *ui.Context, value string, style replyStyle) {
	if value == "" {
		return
	}
	el := ui.Text(c, value)
	if style.size > 0 {
		el.FontSize(style.size)
	}
	if style.family != "" {
		// A span carries its own family: the rich text around it only sets
		// the size and colour its spans inherit.
		el = el.Font(style.family)
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
