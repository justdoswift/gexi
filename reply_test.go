package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestAgentConversationKeepsBothSides wires the agents that report their
// words through the event handler: the prompt a turn began with and the answer
// it ended with both land in the log, in that order, while an agent without a
// text channel adds nothing.
func TestAgentConversationKeepsBothSides(t *testing.T) {
	p := &pane{ID: "p1"}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.start", Pane: "p1", Agent: "claude", Prompt: "把登录跳转修一下",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "claude",
		Message: "## 结论\n\n修好了。",
	}); err != nil {
		t.Fatal(err)
	}
	if len(p.conversation) != 2 {
		t.Fatalf("conversation = %+v, want the prompt and the answer", p.conversation)
	}
	if first := p.conversation[0]; !first.FromUser || first.Text != "把登录跳转修一下" || first.Agent != agentClaude {
		t.Fatalf("first message = %+v, want the user's prompt", first)
	}
	if last := p.conversation[1]; last.FromUser || last.Text != "## 结论\n\n修好了。" {
		t.Fatalf("second message = %+v, want Claude's answer", last)
	}

	// A turn that ends without words (only tools) must not add an empty
	// message to the log.
	if _, err := w.handleEvent(agentEvent{Kind: "turn.end", Pane: "p1", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if len(p.conversation) != 2 {
		t.Fatalf("an empty turn added a message: %+v", p.conversation)
	}

	// An agent with no text channel here adds nothing on either side.
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.start", Pane: "p1", Agent: "pi", Prompt: "pi 的问题",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "pi", Message: "pi 的回答",
	}); err != nil {
		t.Fatal(err)
	}
	if len(p.conversation) != 2 {
		t.Fatalf("a pi turn reached the log: %+v", p.conversation)
	}

	// Amp reports both sides the same way, in a pane of its own.
	ap := &pane{ID: "p2"}
	as := &session{ID: "s2", Root: leaf(ap)}
	w.Projects = append(w.Projects, &project{Sessions: []*session{as}})
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.start", Pane: "p2", Agent: "amp", Session: "T-1", Prompt: "再看看测试",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p2", Agent: "amp", Session: "T-1", Message: "## Amp 结论\n\n也修好了。",
	}); err != nil {
		t.Fatal(err)
	}
	if len(ap.conversation) != 2 || ap.conversation[1].Text != "## Amp 结论\n\n也修好了。" {
		t.Fatalf("conversation = %+v, want Amp's turn appended", ap.conversation)
	}
	if got := ap.conversationAgent(); got != agentAmp {
		t.Fatalf("conversation agent = %q, want amp", got)
	}
}

// TestConversationClearsForANewOne: a pane that starts another conversation
// has nothing from the old one to show. Claude reports no session with its
// events, so its own start is what says the conversation is new; Amp names
// its thread, and only the same thread's log survives.
func TestConversationClearsForANewOne(t *testing.T) {
	p := &pane{ID: "p1"}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "claude", Message: "Claude 的回答",
	}); err != nil {
		t.Fatal(err)
	}
	// A stray announcement from another agent leaves the log alone.
	if _, err := w.handleEvent(agentEvent{Kind: "session.start", Pane: "p1", Agent: "amp", Session: "T-1"}); err != nil {
		t.Fatal(err)
	}
	if got := p.conversationAgent(); got != agentClaude {
		t.Fatalf("an Amp announcement cleared Claude's log: %+v", p.conversation)
	}
	// Amp's own first words are another conversation.
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "amp", Session: "T-1", Message: "Amp 的回答",
	}); err != nil {
		t.Fatal(err)
	}
	if len(p.conversation) != 1 || p.conversationAgent() != agentAmp {
		t.Fatalf("conversation = %+v, want only Amp's answer", p.conversation)
	}
	// Claude still clears its own log on what it reports as a fresh session.
	if _, err := w.handleEvent(agentEvent{
		Kind: "session.start", Pane: "p1", Agent: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	if got := p.conversationAgent(); got != agentAmp {
		t.Fatalf("a Claude start cleared Amp's log: %+v", p.conversation)
	}
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "claude", Message: "新的回答",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.handleEvent(agentEvent{Kind: "session.start", Pane: "p1", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if len(p.conversation) != 0 {
		t.Fatalf("a new Claude session kept the log: %+v", p.conversation)
	}
}

// TestReplySurvivesTheSameSessionStart: Amp's plugin can announce the thread
// again without starting a new conversation. The log survives that, but a
// different thread clears it.
func TestReplySurvivesTheSameSessionStart(t *testing.T) {
	p := &pane{ID: "p1"}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "amp", Session: "T-1",
		Message: "Amp 的回答",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.handleEvent(agentEvent{
		Kind: "session.start", Pane: "p1", Agent: "amp", Session: "T-1",
	}); err != nil {
		t.Fatal(err)
	}
	if got := p.conversationAgent(); got != agentAmp {
		t.Fatalf("the same thread's announcement cleared the log: %+v", p.conversation)
	}

	if _, err := w.handleEvent(agentEvent{
		Kind: "session.start", Pane: "p1", Agent: "amp", Session: "T-2",
	}); err != nil {
		t.Fatal(err)
	}
	if len(p.conversation) != 0 {
		t.Fatalf("a different thread kept the log: %+v", p.conversation)
	}
}

// TestPromptEchoIsSaidOnce: Amp can report one prompt twice — the state
// stream names a turn from the thread's last message while the start hook
// reports the same words. The pane reads it as one question, and a prompt the
// user really sends again after an answer is still two.
func TestPromptEchoIsSaidOnce(t *testing.T) {
	p := &pane{ID: "p1"}
	p.addUserMessage(agentAmp, "T-1", "看一下登录跳转")
	p.addUserMessage(agentAmp, "T-1", "看一下登录跳转")
	if len(p.conversation) != 1 {
		t.Fatalf("conversation = %+v, want the echo dropped", p.conversation)
	}

	// A second report can land long after the first, and need not space
	// its words the same way: it is still the one question.
	p.conversation[0].At = time.Now().Add(-time.Hour)
	p.addUserMessage(agentAmp, "T-1", "  看一下登录跳转\n")
	if len(p.conversation) != 1 {
		t.Fatalf("conversation = %+v, want the late echo dropped", p.conversation)
	}

	// The same words after the agent answered are a new question.
	p.addReplyMessage(agentAmp, "T-1", "看过了，没问题。")
	p.addUserMessage(agentAmp, "T-1", "看一下登录跳转")
	if len(p.conversation) != 3 {
		t.Fatalf("conversation = %d messages, want the new question kept", len(p.conversation))
	}

	// A different prompt a moment later is not an echo.
	p.addUserMessage(agentAmp, "T-1", "再看一下注册")
	if len(p.conversation) != 4 {
		t.Fatalf("conversation = %d messages, want both prompts", len(p.conversation))
	}
}

// TestLongMessageIsCapped keeps a huge hook payload from becoming a huge
// layout. The cut lands on a rune boundary and the reader can say it was cut.
func TestLongMessageIsCapped(t *testing.T) {
	full := strings.Repeat("长", replyMaxBytes)
	p := &pane{ID: "p1"}
	p.addUserMessage(agentClaude, "s1", full)
	p.addReplyMessage(agentClaude, "s1", full)
	if len(p.conversation) != 2 {
		t.Fatalf("conversation = %d messages, want 2", len(p.conversation))
	}
	for _, m := range p.conversation {
		if !m.Truncated {
			t.Fatalf("message = %+v, want a truncated one", m)
		}
		if len(m.Text) > replyMaxBytes {
			t.Fatalf("kept %d bytes, cap is %d", len(m.Text), replyMaxBytes)
		}
		if !strings.HasPrefix(full, m.Text) {
			t.Fatal("the cut changed the text before it")
		}
	}
}

// TestConversationDropsItsOldestTurns keeps a session left running all day
// from growing the log, and the reader says some of it is gone.
func TestConversationDropsItsOldestTurns(t *testing.T) {
	p := &pane{ID: "p1"}
	chunk := strings.Repeat("a", conversationMaxBytes/2)
	for i := 0; i < 4; i++ {
		p.addReplyMessage(agentClaude, "s1", chunk)
	}
	total := 0
	for _, m := range p.conversation {
		total += len(m.Text)
	}
	if total > conversationMaxBytes {
		t.Fatalf("kept %d bytes, cap is %d", total, conversationMaxBytes)
	}
	if len(p.conversation) != 2 {
		t.Fatalf("conversation = %d messages, want the 2 newest", len(p.conversation))
	}
	if !p.conversationTrimmed {
		t.Fatal("the reader cannot say the oldest turns are gone")
	}
}

// TestReplyReaderLaysOutMarkdown draws the reader over a reply with the
// structures an agent actually writes: headings, emphasis, inline code,
// links, nested lists, a quote, a code block, a table and a task list.
func TestReplyReaderLaysOutMarkdown(t *testing.T) {
	text := strings.Join([]string{
		"# 修复登录重定向",
		"",
		"登录现在会回到原来的页面，而不是首页。",
		"",
		"## 改动",
		"",
		"- `session.go` 里的 **cookie** 过期时间",
		"- *刷新* 时保留 `next` 参数",
		"  - 嵌套项",
		"",
		"1. 第一步",
		"2. 第二步",
		"",
		"> 这是一段引用，里面有 `代码`。",
		"",
		"```go",
		"func main() {",
		"\tfmt.Println(\"hi\")",
		"}",
		"```",
		"",
		"| 文件 | 状态 |",
		"| --- | --- |",
		"| session.go | 已修复 |",
		"| auth_test.go | 新增 |",
		"",
		"- [x] 回归测试",
		"- [ ] 发布",
	}, "\n")

	p := &pane{ID: "p1"}
	p.addUserMessage(agentClaude, "s1", "登录后跳回了首页，把 `next` 参数带上")
	p.addReplyMessage(agentClaude, "s1", text)
	s := &session{ID: "s1", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{
		Projects:        []*project{pr},
		SelectedSession: s.ID,
		FocusedPane:     p.ID,
		reply: replyState{
			open: true, pane: p, agent: agentClaude, msgs: p.conversation,
		},
	}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Row(c).Fill().Children(func() {
			w.replyView(c, tk)
		})
	}, 820, 760)
	tester.Frame()

	for _, want := range []string{
		locales.ReplyUser(),
		"登录后跳回了首页",
		"修复登录重定向",
		"登录现在会回到原来的页面",
		"改动",
		"session.go",
		"cookie",
		"嵌套项",
		"第一步",
		"这是一段引用",
		"func main()",
		"fmt.Println",
		"已修复",
		"回归测试",
		"发布",
	} {
		if !tester.HasText(want) {
			t.Errorf("the reader does not show %q", want)
		}
	}
	if !tester.HasText("Claude Code") {
		t.Error("the reader does not name the agent")
	}
	if !tester.HasText(locales.ReplyCopy()) {
		t.Error("the reader has no copy button")
	}

	// Copy reads as a transcript of both sides, not as one loose answer.
	got := conversationMarkdown(p.conversation)
	for _, want := range []string{locales.ReplyUser(), "登录后跳回了首页", "修复登录重定向", "已修复"} {
		if !strings.Contains(got, want) {
			t.Errorf("the copy does not hold %q: %q", want, got)
		}
	}
}

// TestReplyReaderNamesAmp: an Amp reply wears Amp's name in the header, not
// Claude's. The panel is the same; only the agent the pane reports changes.
func TestReplyReaderNamesAmp(t *testing.T) {
	p := &pane{ID: "p1"}
	p.addReplyMessage(agentAmp, "T-1", "# Amp 的回答\n\n正文")
	s := &session{ID: "s1", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{
		Projects:        []*project{pr},
		SelectedSession: s.ID,
		FocusedPane:     p.ID,
		reply: replyState{
			open: true, pane: p, agent: agentAmp, msgs: p.conversation,
		},
	}
	tester := ui.NewTester(func(c *ui.Context) {
		tk := themeTokens(c.Theme())
		ui.Row(c).Fill().Children(func() {
			w.replyView(c, tk)
		})
	}, 820, 500)
	tester.Frame()
	if !tester.HasText("Amp") {
		t.Error("the reader does not name Amp")
	}
	if !tester.HasText("Amp 的回答") {
		t.Error("the reader does not show the Amp answer")
	}
}
