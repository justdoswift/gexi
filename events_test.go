package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justdoswift/gexi/locales"
)

// TestNotificationLine reads the agent's own words on a banner: the first
// line worth showing, with markdown markers gone, and a long reply cut.
func TestNotificationLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"## 结论\n\n细节", "结论"},
		{"\n\n- 修好了 splitPane\n更多", "修好了 splitPane"},
		{"```\ncode()\n```", "code()"},
		{"  \n\t\n", ""},
	}
	for _, c := range cases {
		if got := notificationLine(c.in); got != c.want {
			t.Fatalf("notificationLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	long := strings.Repeat("长", notificationLineMax+20)
	got := notificationLine(long)
	if runes := []rune(got); len(runes) != notificationLineMax || runes[len(runes)-1] != '…' {
		t.Fatalf("a long line became %d runes (%q), want %d ending in an ellipsis", len(runes), got, notificationLineMax)
	}
}

// TestNotificationCopyUsesTheReplyLine puts the reply in the body and keeps
// the agent and place in the subtitle, so two banners stay distinguishable.
func TestNotificationCopyUsesTheReplyLine(t *testing.T) {
	title, subtitle, body := notificationCopy(agentEvent{
		Kind: "turn.end", Agent: "claude", Message: "第一行\n第二行",
	}, "Gexi · branch")
	if title != locales.NotifyTurnFinished() {
		t.Fatalf("title %q", title)
	}
	if subtitle != "Claude Code · Gexi · branch" {
		t.Fatalf("subtitle %q", subtitle)
	}
	if body != "第一行" {
		t.Fatalf("body %q, want the reply's first line", body)
	}

	// A hook without the agent's words keeps the place as the body.
	_, _, body = notificationCopy(agentEvent{Kind: "turn.end", Agent: "claude"}, "Gexi")
	if body != "Gexi" {
		t.Fatalf("fallback body %q, want the place", body)
	}
}

// TestSubagentCountKeepsASessionWorking covers a background subagent still
// running after its turn ended. The pane is idle, but the session is not.
func TestSubagentCountKeepsASessionWorking(t *testing.T) {
	p := &pane{Worktree: &paneWorktree{Branch: "dark-willow"}}
	s := &session{ID: "s", Root: leaf(p)}

	if got := sessionActivityState(s); got != "" {
		t.Fatalf("idle session reports %q", got)
	}

	p.activity = &agentActivity{State: activityIdle}
	p.startSubagent("a1", "Explore")
	p.startSubagent("a2", "Plan")
	if got := p.subagentCount(); got != 2 {
		t.Fatalf("subagents = %d, want 2", got)
	}
	if got := sessionActivityState(s); got != activityWorking {
		t.Fatalf("state with a background subagent = %q, want working", got)
	}

	// A prompt that needs the user outranks the count: the label stops its
	// shimmer.
	p.activity = &agentActivity{State: activityWaiting}
	if got := sessionActivityState(s); got != activityWaiting {
		t.Fatalf("state = %q, want waiting", got)
	}

	p.endSubagent("a1", "Explore")
	if got := p.subagentCount(); got != 1 {
		t.Fatalf("after one stop: %d, want 1", got)
	}
	p.endSubagent("a2", "Plan")
	p.activity = &agentActivity{State: activityIdle}
	if got := sessionActivityState(s); got != "" {
		t.Fatalf("state after all stop = %q, want idle", got)
	}

	// A payload without ids still pairs by type, so an older hook cannot
	// leave a count that never goes away.
	p.startSubagent("", "Explore")
	p.endSubagent("", "Explore")
	if got := p.subagentCount(); got != 0 {
		t.Fatalf("id-less start and stop left %d", got)
	}
}

// TestSessionActivityAnyPaneWorking: a session with three panes keeps its
// sidebar loader while any one of them is still working, even after the
// first pane's turn ends.
func TestSessionActivityAnyPaneWorking(t *testing.T) {
	a := &pane{ID: "a", activity: &agentActivity{State: activityIdle}}
	b := &pane{ID: "b", activity: &agentActivity{State: activityWorking}}
	c := &pane{ID: "c", activity: &agentActivity{State: activityIdle}}
	root := &splitNode{
		axis:  axisHorizontal,
		ratio: 50,
		first: leaf(a),
		second: &splitNode{
			axis: axisVertical, ratio: 50, first: leaf(b), second: leaf(c),
		},
	}
	s := &session{ID: "s", Root: root}

	if got := sessionActivityState(s); got != activityWorking {
		t.Fatalf("state = %q, want working while one pane works", got)
	}
	b.activity = &agentActivity{State: activityIdle}
	if got := sessionActivityState(s); got != "" {
		t.Fatalf("state = %q, want idle after all panes stop", got)
	}
	c.activity = &agentActivity{State: activityWaiting}
	a.activity = &agentActivity{State: activityWorking}
	if got := sessionActivityState(s); got != activityWaiting {
		t.Fatalf("state = %q, want waiting to outrank working", got)
	}
}

// TestTurnResumeKeepsTheNote: a pane found mid-turn becomes working, but the
// one-time carried-changes note belongs to the real prompt's turn.start, not
// to this state-only resume.
func TestTurnResumeKeepsTheNote(t *testing.T) {
	p := &pane{ID: "ab12", Worktree: &paneWorktree{Carried: "/tmp/project"}}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	result, err := w.handleEvent(agentEvent{
		Kind: "turn.resume", Agent: "amp", Pane: "ab12", Session: "T-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.activity == nil || p.activity.State != activityWorking {
		t.Fatalf("activity = %+v, want working", p.activity)
	}
	if result.(map[string]any)["context"] != nil {
		t.Fatal("turn.resume consumed the carried-changes note")
	}
	if p.Worktree.Carried == "" {
		t.Fatal("turn.resume cleared the carried-changes note")
	}
}

// TestSubagentEventsReachThePane wires the two hook events through the socket
// handler: a start and its stop pair by id without touching the turn state.
func TestSubagentEventsReachThePane(t *testing.T) {
	p := &pane{ID: "ab12"}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if _, err := w.handleEvent(agentEvent{
		Kind: eventSubagentStart, Pane: "ab12", Subagent: "a1", SubagentType: "Explore",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := p.subagents["a1"].Type; got != "Explore" {
		t.Fatalf("stored type %q", got)
	}
	if p.activity != nil {
		t.Fatalf("a subagent changed the turn state to %+v", p.activity)
	}

	if _, err := w.handleEvent(agentEvent{
		Kind: eventSubagentEnd, Pane: "ab12", Subagent: "a1", SubagentType: "Explore",
	}); err != nil {
		t.Fatalf("end: %v", err)
	}
	if got := p.subagentCount(); got != 0 {
		t.Fatalf("after the stop %d subagents remain", got)
	}

	if _, err := w.handleEvent(agentEvent{Kind: "subagent.bogus"}); err == nil {
		t.Fatal("an unknown subagent event was accepted")
	}
}

// TestTurnEndStartsTheCompletionSweep covers when the light belongs to a
// finished task: a turn that left a background child running waits for that
// child's stop, new work puts the light away, and a plain turn end starts it.
func TestTurnEndStartsTheCompletionSweep(t *testing.T) {
	p := &pane{ID: "ab12"}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	if _, err := w.handleEvent(agentEvent{Kind: eventSubagentStart, Pane: "ab12", Subagent: "a1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.handleEvent(agentEvent{Kind: "turn.end", Pane: "ab12", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if !p.haloStart.IsZero() {
		t.Fatal("a turn ending with a running subagent lit the pane")
	}
	if _, err := w.handleEvent(agentEvent{Kind: eventSubagentEnd, Pane: "ab12", Subagent: "a1"}); err != nil {
		t.Fatal(err)
	}
	if p.haloStart.IsZero() {
		t.Fatal("the last subagent's stop did not light the pane")
	}

	if _, err := w.handleEvent(agentEvent{Kind: "turn.start", Pane: "ab12", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if !p.haloStart.IsZero() {
		t.Fatal("new work left the last completion's light running")
	}
	if _, err := w.handleEvent(agentEvent{Kind: "turn.end", Pane: "ab12", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if p.haloStart.IsZero() {
		t.Fatal("a finished turn did not light the pane")
	}
	// Waiting for the user is not a finished task.
	if _, err := w.handleEvent(agentEvent{Kind: "needs-input", Pane: "ab12", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if !p.haloStart.IsZero() {
		t.Fatal("a prompt waiting for input kept the completion light")
	}
}

func TestClaudeLaunchInjectsHooks(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", "/opt/test/bin/gexi")

	resolved := resolveAgentCommand(shellCommand("claude"))
	raw, ok := rawCommand(resolved.launch)
	if !ok {
		t.Fatalf("launch is not a shell command: %v", resolved.launch)
	}
	if !strings.Contains(raw, "--session-id") {
		t.Fatalf("launch lost the session id: %s", raw)
	}
	if !strings.Contains(raw, "--settings") {
		t.Fatalf("launch has no hook settings: %s", raw)
	}

	data, err := os.ReadFile(hookSettingsPath())
	if err != nil {
		t.Fatalf("hook settings not written: %v", err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Type, Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("hook settings are not JSON: %v", err)
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd", "SubagentStart", "SubagentStop"} {
		entries := settings.Hooks[name]
		if len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("hook %s missing", name)
		}
		if !strings.Contains(entries[0].Hooks[0].Command, "/opt/test/bin/gexi") {
			t.Fatalf("hook %s does not call gexi: %s", name, entries[0].Hooks[0].Command)
		}
	}
	// An idle Claude fires Notification too; only a permission prompt means
	// the agent is blocked.
	if got := settings.Hooks["Notification"][0].Matcher; got != "permission_prompt" {
		t.Fatalf("Notification matcher is %q, want permission_prompt", got)
	}

	// Resume must keep the hook settings, or a restored pane goes silent.
	restore, _ := rawCommand(resolved.restore)
	if !strings.Contains(restore, "--resume") || !strings.Contains(restore, "--settings") {
		t.Fatalf("restore lost session or hooks: %s", restore)
	}
}

func TestClaudeResumeKeepsHooks(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", "/opt/test/bin/gexi")

	resolved := resolveAgentCommand(shellCommand("claude --resume 2e1130fe-78ba-4fbc-917b-8254bd823f52"))
	raw, _ := rawCommand(resolved.launch)
	if !strings.Contains(raw, "--settings") {
		t.Fatalf("resume has no hook settings: %s", raw)
	}
	if strings.Count(raw, "--resume") != 1 {
		t.Fatalf("resume was not left alone: %s", raw)
	}
	restore, _ := rawCommand(resolved.restore)
	if restore != raw {
		t.Fatalf("resume restore differs from launch: %s", restore)
	}
}

// A bundle has no gexi CLI beside it, so the app binary itself must answer
// the hook's `event` call.
func TestClaudeWithoutClientUsesOwnBinary(t *testing.T) {
	t.Setenv("GEXI_HOME", t.TempDir())
	t.Setenv("GEXI_BIN", "")
	t.Setenv("PATH", t.TempDir())

	resolved := resolveAgentCommand(shellCommand("claude"))
	raw, _ := rawCommand(resolved.launch)
	if !strings.Contains(raw, "--settings") {
		t.Fatalf("launch has no hook settings: %s", raw)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("own executable: %v", err)
	}
	data, err := os.ReadFile(hookSettingsPath())
	if err != nil {
		t.Fatalf("hook settings not written: %v", err)
	}
	if !strings.Contains(string(data), exe) {
		t.Fatalf("hooks do not call this binary: %s", data)
	}
}

// A claude started by hand in a pane never sees --settings, so the user's own
// settings carry the same hooks, guarded by GEXI_PANE.
func TestGlobalClaudeHooksMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEXI_BIN", "/opt/test/bin/gexi")
	t.Cleanup(func() { globalHooksInstalled = false })

	path := globalClaudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "/opt/other/bridge"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installGlobalClaudeHooks(); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Installing again must replace gexi's entries, not stack another set.
	if err := installGlobalClaudeHooks(); err != nil {
		t.Fatalf("install again: %v", err)
	}

	var doc struct {
		Model string `json:"model"`
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string }
		} `json:"hooks"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("settings are not JSON: %v", err)
	}
	if doc.Model != "opus" {
		t.Fatalf("unrelated setting lost: %s", data)
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd", "SubagentStart", "SubagentStop"} {
		entries := doc.Hooks[name]
		var gexi []string
		for _, e := range entries {
			for _, h := range e.Hooks {
				if strings.Contains(h.Command, "GEXI_PANE") {
					gexi = append(gexi, h.Command)
				}
			}
		}
		if len(gexi) != 1 {
			t.Fatalf("%s has %d gexi hooks, want 1: %s", name, len(gexi), data)
		}
		if !strings.Contains(gexi[0], "/opt/test/bin/gexi") || !strings.Contains(gexi[0], " event ") {
			t.Fatalf("%s hook does not call gexi: %s", name, gexi[0])
		}
	}
	// Another tool's hook under the same event stays where it was.
	kept := false
	for _, e := range doc.Hooks["Stop"] {
		for _, h := range e.Hooks {
			if h.Command == "/opt/other/bridge" {
				kept = true
			}
		}
	}
	if !kept {
		t.Fatalf("another tool's Stop hook was dropped: %s", data)
	}
	if got := doc.Hooks["Notification"][len(doc.Hooks["Notification"])-1].Matcher; got != "permission_prompt" {
		t.Fatalf("Notification matcher is %q, want permission_prompt", got)
	}
}

// A working mark that nothing backs up must not keep the sidebar sweeping:
// no output, no animating title, no subagent means the turn is over even if
// its stop hook never came.
func TestActivityLapsesWhenNothingMoves(t *testing.T) {
	p := &pane{}
	p.outAt = time.Now()
	if p.activityLapsed() {
		t.Fatal("a pane that just wrote is not stale")
	}
	p.outAt = time.Now().Add(-activityQuiet - time.Second)
	if !p.activityLapsed() {
		t.Fatal("a pane quiet for past the window should lapse")
	}
	p.subagents = map[string]paneSubagent{"a": {}}
	if p.activityLapsed() {
		t.Fatal("a running subagent keeps the turn alive")
	}
}

// A clicked banner opens the pane it was about: the session comes forward,
// the pane takes the keyboard, and a click that arrives before the panes are
// back waits for the restore to finish.
func TestNotificationClickOpensItsPane(t *testing.T) {
	a, b := &pane{ID: "a"}, &pane{ID: "b"}
	first := &session{ID: "first", Root: leaf(a)}
	second := &session{ID: "second", Root: leaf(b)}
	w := &workspace{
		Projects:        []*project{{ID: "p", Sessions: []*session{first, second}}},
		SelectedSession: "first",
		FocusedPane:     "a",
	}

	if got := notificationID(b); got != "pane:b" {
		t.Fatalf("notificationID = %q, want pane:b", got)
	}
	w.openNotification("pane:b")
	if w.pendingNotificationPane != "b" {
		t.Fatalf("a click before the window is up was not kept: %q", w.pendingNotificationPane)
	}
	w.revealPendingNotification()
	if w.SelectedSession != "second" || w.FocusedPane != "b" || w.pendingNotificationPane != "" {
		t.Fatalf("session %q pane %q pending %q, want second/b/none",
			w.SelectedSession, w.FocusedPane, w.pendingNotificationPane)
	}

	// Something else on the Mac: a banner's ID is not a pane.
	w.openNotification("elsewhere")
	if w.pendingNotificationPane != "" {
		t.Fatal("an ID that names no pane was taken for one")
	}
}

func TestNotificationPaneID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"pane:abc", "abc"},
		{"", ""},
		{"abc", ""},
		{"pane:", ""},
	}
	for _, c := range cases {
		if got := notificationPaneID(c.in); got != c.want {
			t.Fatalf("notificationPaneID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The control socket's event method carries the prompt through to the pane:
// it is what a pane names itself from, and calling handleEvent directly
// would not catch the field being dropped on the way in.
func TestControlEventCarriesThePrompt(t *testing.T) {
	p := &pane{ID: "p"}
	s := &session{ID: "s", Root: leaf(p)}
	pr := &project{ID: "pr", Dir: t.TempDir(), Sessions: []*session{s}}
	w := &workspace{Projects: []*project{pr}}
	prompt, err := json.Marshal("补一个登录回归测试")
	if err != nil {
		t.Fatal(err)
	}
	req := controlRequest{Method: "event", Params: map[string]json.RawMessage{
		"kind":   json.RawMessage(`"turn.start"`),
		"agent":  json.RawMessage(`"claude"`),
		"pane":   json.RawMessage(`"p"`),
		"prompt": prompt,
	}}
	if _, err := w.dispatch(req); err != nil {
		t.Fatal(err)
	}
	if p.firstPrompt != "补一个登录回归测试" {
		t.Fatalf("first prompt = %q", p.firstPrompt)
	}
}
