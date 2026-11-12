package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A sidebar row's context menu renames or closes it. A session's name typed
// by hand is kept on the session ahead of the branch, agent, or terminal
// title it would otherwise show, so an agent renaming its own branch cannot
// overwrite the name.

func (w *workspace) sessionRowMenu(m *ui.Menu, s *session) {
	// A session on another Mac is named by its host, which has no rename
	// command for this window to call.
	if s.remote == nil {
		if m.Item(locales.Rename()).Chosen() {
			w.openSessionRename(s)
		}
		m.Separator()
	}
	if m.Item(locales.CloseSession()).Chosen() {
		w.askCloseSession(s)
	}
}

func (w *workspace) projectRowMenu(m *ui.Menu, pr *project) {
	// A paired Mac is forgotten rather than named here: its name belongs to
	// the host.
	if pr.remote != nil {
		if m.Item(locales.SettingsRemoteRemove()).Danger().Chosen() {
			w.closeProject(pr)
		}
		return
	}
	if m.Item(locales.Rename()).Chosen() {
		w.openProjectRename(pr)
	}
	m.Separator()
	if m.Item(locales.CloseProject()).Danger().Chosen() {
		w.closeProject(pr)
	}
}

func (w *workspace) openSessionRename(s *session) {
	if s == nil || s.remote != nil {
		return
	}
	w.sidebarRenameSession = s
	w.sidebarRenameProject = nil
	w.sidebarRenameText = sessionLabel(s)
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = true
	w.sidebarRenameOpen = true
	w.invalidate()
}

func (w *workspace) openProjectRename(pr *project) {
	if pr == nil || pr.remote != nil {
		return
	}
	w.sidebarRenameSession = nil
	w.sidebarRenameProject = pr
	w.sidebarRenameText = pr.name()
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = true
	w.sidebarRenameOpen = true
	w.invalidate()
}

func (w *workspace) cancelSidebarRename() {
	w.sidebarRenameOpen = false
	w.sidebarRenameSession = nil
	w.sidebarRenameProject = nil
	w.sidebarRenameText = ""
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = false
	w.invalidate()
}

func (w *workspace) submitSidebarRename() {
	name := strings.TrimSpace(w.sidebarRenameText)
	if name == "" {
		w.sidebarRenameErr = locales.RenameEmpty()
		w.invalidate()
		return
	}
	switch {
	case w.sidebarRenameSession != nil:
		if s := w.sidebarRenameSession; w.sessionByID(s.ID) == s {
			s.Label = name
		}
	case w.sidebarRenameProject != nil:
		if pr := w.sidebarRenameProject; w.projectByID(pr.ID) == pr {
			pr.Label = name
		}
	}
	w.sidebarRenameOpen = false
	w.sidebarRenameSession = nil
	w.sidebarRenameProject = nil
	w.sidebarRenameText = ""
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = false
	w.invalidate()
	w.scheduleSave()
}

// A pane's header names the pane. A name typed by hand, or one made from
// the pane's first message, outranks the agent's own name and the terminal
// title; a generated name reads as a work log — the day, the kind of work,
// and what the work is about, as 20261111|需求|登录页扫码.

// paneNameDateLayout is how a generated name starts: the day the work began.
const paneNameDateLayout = "20060102"

// paneNameKinds are the kinds of work a generated name picks from. An agent
// may name another short kind; these are the ones it is offered.
var paneNameKinds = []string{"需求", "问题", "测试", "重构", "文档"}

func (w *workspace) paneHeaderMenu(m *ui.Menu, p *pane) {
	if p == nil {
		return
	}
	// A pane on another Mac is named by its host.
	if p.remote != nil {
		return
	}
	if m.Item(locales.RenamePane()).Chosen() {
		w.openPaneRename(p)
	}
	if p.canNameFromFirstMessage() {
		if m.Item(locales.PaneNameFromFirstMessage()).Chosen() {
			w.openPaneRename(p)
			w.generatePaneName()
		}
	}
	if p.Label != "" {
		m.Separator()
		if m.Item(locales.PaneNameReset()).Chosen() {
			w.clearPaneName(p)
		}
	}
}

func (w *workspace) openPaneRename(p *pane) {
	if p == nil || p.remote != nil {
		return
	}
	w.paneRenameTarget = p
	w.paneRenameText = p.Label
	w.paneRenameErr = ""
	w.paneRenameSelect = true
	w.paneRenameBusy = false
	w.paneRenameOpen = true
	w.invalidate()
}

func (w *workspace) cancelPaneRename() {
	w.paneRenameOpen = false
	w.paneRenameTarget = nil
	w.paneRenameText = ""
	w.paneRenameErr = ""
	w.paneRenameSelect = false
	w.paneRenameBusy = false
	w.invalidate()
}

// clearPaneName goes back to the title the agent or the terminal gives the
// pane, and remembers that the pane should not name itself again.
func (w *workspace) clearPaneName(p *pane) {
	if p == nil {
		return
	}
	p.Label = ""
	p.nameAsked = true
	w.invalidate()
	w.scheduleSave()
}

func (w *workspace) submitPaneRename() {
	name := strings.TrimSpace(w.paneRenameText)
	if name == "" {
		w.paneRenameErr = locales.RenameEmpty()
		w.invalidate()
		return
	}
	if p := w.paneRenameTarget; p != nil && w.paneByID(p.ID) == p {
		p.Label = name
		// A name given by hand stays, whatever the first message says.
		p.nameAsked = true
	}
	w.paneRenameOpen = false
	w.paneRenameTarget = nil
	w.paneRenameText = ""
	w.paneRenameErr = ""
	w.paneRenameSelect = false
	w.paneRenameBusy = false
	w.invalidate()
	w.scheduleSave()
}

// canNameFromFirstMessage reports whether the pane knows its first message
// and runs an agent that can read it.
func (p *pane) canNameFromFirstMessage() bool {
	return p != nil && p.remote == nil && strings.TrimSpace(p.firstPrompt) != "" &&
		paneNamingKind(p, "") != ""
}

// paneNamingKind is the agent that can name p: the one the event named, else
// the one running in the pane. Only an agent with a headless mode can.
func paneNamingKind(p *pane, hint agentKind) agentKind {
	if p == nil || p.remote != nil {
		return ""
	}
	candidates := []agentKind{hint, p.nameKind}
	if p.agent != nil {
		candidates = append(candidates, p.agent.Kind)
	}
	for _, kind := range candidates {
		if kind == "" {
			continue
		}
		if _, ok := agentHeadless(kind); ok {
			return kind
		}
	}
	return ""
}

// generatePaneName makes a name from the first message and puts it in the
// prompt, so it can be read before it is kept. It runs off the main thread.
func (w *workspace) generatePaneName() {
	p := w.paneRenameTarget
	if p == nil || w.paneRenameBusy || !p.canNameFromFirstMessage() {
		return
	}
	w.paneRenameBusy = true
	w.paneRenameErr = ""
	w.invalidate()
	w.askPaneName(p, paneNamingKind(p, ""), func(name string, err error) {
		if w.paneRenameTarget != p || !w.paneRenameOpen {
			return
		}
		w.paneRenameBusy = false
		if err != nil {
			w.paneRenameErr = locales.PaneNameFailed(err.Error())
		} else {
			w.paneRenameText = name
			w.paneRenameErr = ""
			w.paneRenameSelect = true
		}
		w.invalidate()
	})
}

// namePaneWithAgent names a pane from its first message once, when it has
// no name of its own. A pane the user has named is never renamed, and a
// failed answer leaves the title it already shows.
func (w *workspace) namePaneWithAgent(p *pane, hint agentKind) {
	if p == nil || w.win == nil || p.Label != "" || p.nameAsked {
		return
	}
	kind := paneNamingKind(p, hint)
	if kind == "" || strings.TrimSpace(p.firstPrompt) == "" {
		return
	}
	p.nameAsked = true
	w.askPaneName(p, kind, func(name string, err error) {
		if err != nil || name == "" || w.paneByID(p.ID) != p || p.Label != "" {
			return
		}
		p.Label = name
		w.invalidate()
		w.scheduleSave()
	})
}

// askPaneName runs the pane's agent on its first message and hands back the
// name it made, on the main thread.
func (w *workspace) askPaneName(p *pane, kind agentKind, done func(string, error)) {
	if p == nil || kind == "" {
		return
	}
	first := strings.TrimSpace(p.firstPrompt)
	if first == "" {
		return
	}
	// The message says everything the name needs, so the question runs in a
	// scratch folder: the pane's own agent cannot read or change the work it
	// is naming by accident.
	dir, date := os.TempDir(), p.openedAt
	if date.IsZero() {
		date = time.Now()
	}
	go func() {
		answer, err := agentAnswer(kind, dir, paneNamePrompt(first))
		name := ""
		if err == nil {
			if name = paneNameFromAnswer(answer, date); name == "" {
				err = fmt.Errorf("%s returned no name", kind.name())
			}
		}
		w.onMain(func() { done(name, err) })
	}()
}

// paneNamePrompt asks the pane's own agent to read the first message as a
// work item: which kind it is, and what it is about.
func paneNamePrompt(first string) string {
	first = strings.TrimSpace(first)
	if r := []rune(first); len(r) > 4000 {
		first = string(r[:4000])
	}
	return "你在给一个开发工作条目起名字。阅读下面第一条用户消息，判断它属于哪一类：" +
		strings.Join(paneNameKinds, "、") + "。再概括业务名称：这条消息要做什么，最多 12 个字，不要标点、引号和解释。" +
		"只输出一行，格式为 类型|业务名称，不要输出其他内容。\n\n第一条消息：\n" + first
}

// paneNameFromAnswer turns the agent's one line into the pane's name: the
// day, the kind of work, and its subject. An answer without a kind still
// names the pane, with the kind read from the words themselves.
func paneNameFromAnswer(answer string, at time.Time) string {
	line := firstAnswerLine(answer)
	line = strings.ReplaceAll(line, "｜", "|")
	// An answer that already carries the day would otherwise read it as the
	// kind of work, and end up with two dates in the name.
	if len(line) > 9 && line[8] == '|' {
		if _, err := time.Parse(paneNameDateLayout, line[:8]); err == nil {
			line = line[9:]
		}
	}
	kind, subject := "", line
	if i := strings.IndexByte(line, '|'); i >= 0 {
		kind = line[:i]
		subject = strings.ReplaceAll(line[i+1:], "|", " ")
	}
	kind = paneNameKind(kind, subject)
	subject = paneNameSubject(subject)
	if subject == "" {
		return ""
	}
	return at.Format(paneNameDateLayout) + "|" + kind + "|" + subject
}

// firstAnswerLine is the line of an answer that carries the name, with the
// code fences, quotes, and list marks models wrap answers in taken off.
func firstAnswerLine(answer string) string {
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "-–—•*#`\"' "))
		line = strings.TrimSpace(strings.Trim(line, "`\"'*#"))
		if line == "" {
			continue
		}
		for _, prefix := range []string{"类型|业务名称：", "类型|业务名称:", "类型：", "类型:", "答案：", "答案:"} {
			line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
		return line
	}
	return ""
}

// paneNameKind is the kind of work a generated name carries. An agent's own
// short kind is kept, and an answer without one is read for it.
func paneNameKind(kind, subject string) string {
	kind = strings.TrimSpace(strings.Trim(kind, "`\"'。.：:"))
	switch strings.ToLower(kind) {
	case "bug", "fix", "issue", "问题", "缺陷", "故障":
		return "问题"
	case "feature", "request", "需求", "功能":
		return "需求"
	case "test", "testing", "测试", "用例":
		return "测试"
	case "refactor", "重构":
		return "重构"
	case "docs", "doc", "文档":
		return "文档"
	}
	if utf8.RuneCountInString(kind) > 0 && utf8.RuneCountInString(kind) <= 4 &&
		!strings.ContainsAny(kind, "|0123456789") {
		return kind
	}
	return guessPaneNameKind(subject)
}

// guessPaneNameKind reads the kind from the words, for an answer that gave
// none: the words that ask for work say what kind of work it is.
func guessPaneNameKind(subject string) string {
	s := strings.ToLower(subject)
	switch {
	case strings.Contains(s, "测试"), strings.Contains(s, "用例"), strings.Contains(s, "回归"), strings.Contains(s, "test"):
		return "测试"
	case strings.Contains(s, "重构"), strings.Contains(s, "refactor"):
		return "重构"
	case strings.Contains(s, "文档"), strings.Contains(s, "说明"), strings.Contains(s, "docs"):
		return "文档"
	case strings.Contains(s, "修复"), strings.Contains(s, "报错"), strings.Contains(s, "异常"),
		strings.Contains(s, "崩溃"), strings.Contains(s, "失败"), strings.Contains(s, "不生效"),
		strings.Contains(s, "为什么"), strings.Contains(s, "问题"), strings.Contains(s, "bug"):
		return "问题"
	}
	return "需求"
}

// paneNameSubject is the part of a name after the kind: one short line,
// without the punctuation and separators a name cannot carry.
func paneNameSubject(subject string) string {
	subject = strings.NewReplacer("\n", " ", "\t", " ", "|", " ", "｜", " ").Replace(subject)
	subject = strings.TrimSpace(subject)
	for _, prefix := range []string{"业务名称：", "业务名称:", "名称：", "名称:"} {
		subject = strings.TrimSpace(strings.TrimPrefix(subject, prefix))
	}
	subject = strings.TrimSpace(strings.Trim(subject, "`\"'“”‘’。.，,;；、 "))
	if r := []rune(subject); len(r) > 24 {
		subject = strings.TrimSpace(string(r[:24]))
	}
	return subject
}

func (w *workspace) paneRenameDialog(c *ui.Context, tk tokens) {
	actions := []dialogAction{}
	if p := w.paneRenameTarget; p != nil && p.canNameFromFirstMessage() {
		label := locales.PaneNameFromFirstMessage()
		if w.paneRenameBusy {
			label = locales.PaneNameWorking()
		}
		actions = append(actions, dialogAction{label: label, kind: dialogQuiet, run: w.generatePaneName})
	}
	actions = append(actions,
		dialogAction{label: locales.Cancel(), kind: dialogQuiet, run: w.cancelPaneRename},
		dialogAction{label: locales.RenameConfirm(), kind: dialogPrimary, run: w.submitPaneRename},
	)
	dialogFrame(c, tk, &w.paneRenameOpen, 420, func() {
		dialogHeading(c, tk, locales.RenamePaneTitle(), locales.RenamePaneHint())
		dialogBody(c, func() {
			input := dialogInput(c, tk, &w.paneRenameText, locales.RenamePanePlaceholder()).
				Label(locales.RenamePanePlaceholder()).AutoFocus()
			if w.paneRenameSelect {
				input.SetTextSelection(0, utf8.RuneCountInString(w.paneRenameText))
				w.paneRenameSelect = false
			}
			input.OnSubmit(func() { w.submitPaneRename() })
			dialogError(c, tk, w.paneRenameErr)
		})
		dialogFooter(c, tk, actions...)
	})
}

func (w *workspace) sidebarRenameDialog(c *ui.Context, tk tokens) {
	title := locales.RenameSessionTitle()
	placeholder := locales.RenameSessionPlaceholder()
	hint := locales.RenameSessionHint()
	if w.sidebarRenameProject != nil {
		title = locales.RenameProjectTitle()
		placeholder = locales.RenameProjectPlaceholder()
		hint = ""
	}
	dialogFrame(c, tk, &w.sidebarRenameOpen, 400, func() {
		dialogHeading(c, tk, title, hint)
		dialogBody(c, func() {
			input := dialogInput(c, tk, &w.sidebarRenameText, placeholder).
				Label(placeholder).AutoFocus()
			if w.sidebarRenameSelect {
				input.SetTextSelection(0, utf8.RuneCountInString(w.sidebarRenameText))
				w.sidebarRenameSelect = false
			}
			input.OnSubmit(func() { w.submitSidebarRename() })
			dialogError(c, tk, w.sidebarRenameErr)
		})
		dialogFooter(c, tk,
			dialogAction{label: locales.Cancel(), kind: dialogQuiet, run: w.cancelSidebarRename},
			dialogAction{label: locales.RenameConfirm(), kind: dialogPrimary, run: w.submitSidebarRename},
		)
	})
}
