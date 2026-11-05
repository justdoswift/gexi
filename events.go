package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

	p.activity = &agentActivity{
		State:   state,
		Agent:   ev.Agent,
		Session: ev.Session,
		Kind:    ev.Kind,
		Since:   time.Now().Format(time.RFC3339),
	}
	result["pane"] = p.ID
	w.invalidate()

	if eventNotifies[ev.Kind] && w.shouldNotify(p, ev.Kind) {
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
		return "Turn finished"
	case "needs-input":
		return "Waiting for you"
	default:
		return "Agent"
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

	command := func(kind string) string {
		return shellQuote(bin) + " event " + kind + " --agent claude"
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart":     []any{hookEntry(command("session.start"), "")},
			"UserPromptSubmit": []any{hookEntry(command("turn.start"), "")},
			"Stop":             []any{hookEntry(command("turn.end"), "")},
			// Only a permission request blocks the agent. Claude Code also
			// fires Notification for idle_prompt sixty seconds after a turn
			// ends; without the matcher that would leave the sidebar loader
			// on for a finished session.
			"Notification": []any{hookEntry(command("needs-input"), "permission_prompt")},
			"SessionEnd":   []any{hookEntry(command("session.end"), "")},
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
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
