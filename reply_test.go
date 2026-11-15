package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// TestClaudeTurnEndKeepsTheReply wires Claude's final answer through the event
// handler: it is kept for the reader, an empty turn leaves the last answer
// alone, another agent's turn is not mistaken for Claude's, and a new
// conversation clears it.
func TestClaudeTurnEndKeepsTheReply(t *testing.T) {
	p := &pane{ID: "p1"}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "claude",
		Message: "## 结论\n\n修好了。",
	}); err != nil {
		t.Fatal(err)
	}
	if p.reply == nil {
		t.Fatal("the turn's answer was not kept")
	}
	if p.reply.Agent != agentClaude || p.reply.Text != "## 结论\n\n修好了。" {
		t.Fatalf("reply = %+v", p.reply)
	}

	// A turn that ends without words (only tools) must not erase the last
	// answer the pane can show.
	if _, err := w.handleEvent(agentEvent{Kind: "turn.end", Pane: "p1", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if p.reply == nil || p.reply.Text == "" {
		t.Fatal("an empty turn erased the last reply")
	}

	// Another agent uses Message for its own notifications, not for Claude's
	// reader.
	if _, err := w.handleEvent(agentEvent{
		Kind: "turn.end", Pane: "p1", Agent: "amp", Message: "amp 的回答",
	}); err != nil {
		t.Fatal(err)
	}
	if got := p.reply.Text; got != "## 结论\n\n修好了。" {
		t.Fatalf("an amp turn replaced Claude's reply with %q", got)
	}

	// Another agent starting in the pane does not own Claude's reply.
	if _, err := w.handleEvent(agentEvent{Kind: "session.start", Pane: "p1", Agent: "amp"}); err != nil {
		t.Fatal(err)
	}
	if p.reply == nil {
		t.Fatal("an amp session start cleared Claude's reply")
	}

	// A fresh Claude conversation is not the one the old answer belongs to.
	if _, err := w.handleEvent(agentEvent{Kind: "session.start", Pane: "p1", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if p.reply != nil {
		t.Fatalf("a new session kept the old reply: %+v", p.reply)
	}
}

// TestReplyLongAnswerIsCapped keeps a huge hook payload from becoming a huge
// layout. The cut lands on a rune boundary and the reader can say it was cut.
func TestReplyLongAnswerIsCapped(t *testing.T) {
	full := strings.Repeat("长", replyMaxBytes)
	p := &pane{ID: "p1"}
	p.setReply(agentClaude, full)
	if p.reply == nil || !p.reply.Truncated {
		t.Fatalf("reply = %+v, want a truncated one", p.reply)
	}
	if len(p.reply.Text) > replyMaxBytes {
		t.Fatalf("kept %d bytes, cap is %d", len(p.reply.Text), replyMaxBytes)
	}
	if !strings.HasPrefix(full, p.reply.Text) {
		t.Fatal("the cut changed the text before it")
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
	p.setReply(agentClaude, text)
	s := &session{ID: "s1", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{
		Projects:        []*project{pr},
		SelectedSession: s.ID,
		FocusedPane:     p.ID,
		reply: replyState{
			open: true, pane: p, reply: p.reply,
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
}
