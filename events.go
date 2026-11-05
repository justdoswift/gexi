package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/justdoswift/gexi/locales"
)

// Agent lifecycle events arrive from CLI hooks (Claude Code today), not from
// scraping the PTY. A hook runs inside the pane, so it already has GEXI_PANE
// and GEXI_HOME; all it does is hand one line of JSON to the socket.
//
// PTY output still says whether a pane is busy at the byte level. Events say
// what the agent thinks it is doing, which raw output cannot: an idle Claude
// prompt and a Claude waiting for permission look the same in the terminal.

const (
	activityIdle    = "idle"
	activityWorking = "working"
	activityWaiting = "waiting"
)

// eventState is the pane state an event puts the agent in.
var eventState = map[string]string{
	"session.start": activityIdle,
	"turn.start":    activityWorking,
	"turn.end":      activityIdle,
	"needs-input":   activityWaiting,
	"session.end":   activityIdle,
}

// eventNotifies is where a desktop notification earns its noise. Turn ends
// and permission prompts are what the user is waiting for; the rest only
// updates state.
var eventNotifies = map[string]bool{
	"turn.end":    true,
	"needs-input": true,
}

type agentActivity struct {
	State   string `json:"state"`
	Agent   string `json:"agent,omitempty"`
	Session string `json:"session,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Since   string `json:"since,omitempty"`
}

type agentEvent struct {
	Kind    string          `json:"kind"`
	Agent   string          `json:"agent,omitempty"`
	Pane    string          `json:"pane,omitempty"`
	Session string          `json:"session,omitempty"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

// handleEvent records one hook event on its pane. Main thread.
func (w *workspace) handleEvent(ev agentEvent) (any, error) {
	if ev.Kind == "" {
		return nil, fmt.Errorf("event needs a kind")
	}
	state, known := eventState[ev.Kind]
	if !known {
		return nil, fmt.Errorf("unknown event kind: %s", ev.Kind)
	}

	var p *pane
	if ev.Pane != "" {
		resolved, err := w.resolvePane(ev.Pane)
		if err != nil {
			return nil, err
		}
		p = resolved
	}

	result := map[string]any{"state": state}
	if p == nil {
		return result, nil
	}
	switch ev.Kind {
	case "turn.start":
		// The hook's stdout becomes context for this prompt, which is where
		// the one-time branch rename note reaches the agent.
		if ctx := w.worktreeNamingContext(p); ctx != "" {
			result["context"] = ctx
		}
	case "turn.end":
		if p.Worktree != nil {
			w.refreshWorktree(p)
		}
	}

	p.activity = &agentActivity{
		State:   state,
		Agent:   ev.Agent,
		Session: ev.Session,
		Kind:    ev.Kind,
		Since:   time.Now().Format(time.RFC3339),
	}
	result["pane"] = p.ID
	w.invalidate()

	if w.settings.Notifications && eventNotifies[ev.Kind] && w.shouldNotify(p, ev.Kind) {
		notifyUser(eventTitle(ev.Kind), paneLabel(p, ev.Agent))
	}
	return result, nil
}

// shouldNotify throttles repeats, so a burst of Notifications from the same
// agent does not become a wall of banners. Main thread.
func (w *workspace) shouldNotify(p *pane, kind string) bool {
	key := p.ID + "/" + kind
	now := time.Now()
	if last, ok := w.lastNotify[key]; ok && now.Sub(last) < 10*time.Second {
		return false
	}
	if w.lastNotify == nil {
		w.lastNotify = map[string]time.Time{}
	}
	w.lastNotify[key] = now
	return true
}

func eventTitle(kind string) string {
	switch kind {
	case "turn.end":
		return locales.NotifyTurnFinished()
	case "needs-input":
		return locales.NotifyWaiting()
	default:
		return locales.NotifyAgent()
	}
}

func paneLabel(p *pane, agent string) string {
	name := agent
	if name == "" {
		name = p.title()
	}
	dir := filepath.Base(p.dir())
	if dir == "" || dir == "." || dir == "/" {
		return name
	}
	return name + " · " + dir
}

// notifyUser posts a banner through osascript. There is no cgo here, so
// UserNotifications is out of reach; this is the cheap path that works.
func notifyUser(title, body string) {
	script := fmt.Sprintf(
		"display notification %s with title %s",
		appleScriptString(body),
		appleScriptString("Gexi · "+title),
	)
	go func() { _ = exec.Command("osascript", "-e", script).Run() }()
}

func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// MARK: - Hook installation

func hookSettingsPath() string {
	return filepath.Join(supportDir(), "hooks", "claude-settings.json")
}

// claudeHookKinds pairs each Claude Code hook event with the Gexi event it
// reports. Only a permission request blocks the agent: Claude Code also
// fires Notification for idle_prompt sixty seconds after a turn ends, which
// without the matcher would leave the sidebar loader on for a finished
// session.
var claudeHookKinds = []struct{ event, kind, matcher string }{
	{"SessionStart", "session.start", ""},
	{"UserPromptSubmit", "turn.start", ""},
	{"Stop", "turn.end", ""},
	{"Notification", "needs-input", "permission_prompt"},
	{"SessionEnd", "session.end", ""},
}

// installClaudeHooks writes the extra settings file Gexi passes to
// `claude --settings`. It is layered on top of the user's own settings, so
// hooks the user already has (vibe-island, orca, …) keep running.
func installClaudeHooks() (string, error) {
	bin := gexiBinary()
	if bin == "" {
		return "", fmt.Errorf("gexi binary not found")
	}
	path := hookSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	hooks := map[string]any{}
	for _, h := range claudeHookKinds {
		hooks[h.event] = []any{hookEntry(shellQuote(bin)+" event "+h.kind+" --agent claude", h.matcher)}
	}
	data, err := json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// globalHooksInstalled reports that the user's own settings now carry the
// hooks, so a launch does not have to pass them again.
var globalHooksInstalled bool

// globalClaudeSettingsPath is the file every `claude` on this Mac reads,
// whether Gexi launched it or the user typed it into a pane.
func globalClaudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// installGlobalClaudeHooks merges Gexi's events into the user's own Claude
// Code settings. A claude Gexi launches gets them through --settings, but one
// the user starts by hand in a pane only reads this file, and without a hook
// here its sidebar loader would never light. Each command exits at once when
// GEXI_PANE is unset, so Claude Code running anywhere else on the Mac reports
// nothing.
func installGlobalClaudeHooks() error {
	bin := gexiBinary()
	if bin == "" {
		return fmt.Errorf("gexi binary not found")
	}
	path := globalClaudeSettingsPath()
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &top); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return err
	}
	hooks := map[string]json.RawMessage{}
	if raw, ok := top["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return err
		}
	}

	for _, h := range claudeHookKinds {
		var groups []json.RawMessage
		if raw, ok := hooks[h.event]; ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return err
			}
		}
		// Replace Gexi's own entry, which may name an older binary after an
		// update, and leave every other tool's hooks where they are.
		kept := groups[:0]
		for _, g := range groups {
			if !isGexiHookGroup(g) {
				kept = append(kept, g)
			}
		}
		entry, err := json.Marshal(hookEntry(guardedHookCommand(bin, h.kind), h.matcher))
		if err != nil {
			return err
		}
		if hooks[h.event], err = json.Marshal(append(kept, entry)); err != nil {
			return err
		}
	}
	if top["hooks"], err = json.Marshal(hooks); err != nil {
		return err
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Write beside the file and rename it into place, so a crash cannot leave
	// the user's settings half written.
	tmp := path + ".gexi.tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	globalHooksInstalled = true
	return nil
}

// guardedHookCommand is the hook for the user's settings: it returns at once
// unless it runs inside a Gexi pane.
func guardedHookCommand(bin, kind string) string {
	script := `[ -n "${GEXI_PANE-}" ] || exit 0; exec ` +
		shellQuote(bin) + " event " + kind + " --agent claude"
	return "/bin/sh -c " + shellQuote(script)
}

// isGexiHookGroup tells a hook Gexi installed from anyone else's.
func isGexiHookGroup(raw json.RawMessage) bool {
	var group struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &group) != nil {
		return false
	}
	for _, h := range group.Hooks {
		if strings.Contains(h.Command, "GEXI_PANE") && strings.Contains(h.Command, " event ") {
			return true
		}
	}
	return false
}

// hookEntry is one command hook, optionally narrowed to the event's matcher
// (Notification's permission_prompt, idle_prompt, …).
func hookEntry(command, matcher string) map[string]any {
	entry := map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": command,
			"timeout": 10,
		}},
	}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return entry
}

// gexiBinary finds the client a hook will run. PATH first: that is where
// `go build -o gexi` lands. Otherwise the app binary itself answers `event`,
// which is the only option inside a bundle.
func gexiBinary() string {
	if v := os.Getenv("GEXI_BIN"); v != "" {
		return v
	}
	if p, err := exec.LookPath("gexi"); err == nil {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return ""
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func activityJSON(a *agentActivity) any {
	if a == nil {
		return nil
	}
	return a
}
