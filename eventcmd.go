package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// The app binary answers `event KIND` itself, the same subcommand the gexi CLI
// offers. A Claude Code hook runs `gexi event turn.start --agent claude` from
// inside a pane, so a packaged app needs no separate CLI next to it.
func runEventCommand(args []string) bool {
	if len(args) == 0 || args[0] != "event" {
		return false
	}
	kind, agent := "", ""
	pane := os.Getenv("GEXI_PANE")
	for i := 1; i < len(args); i++ {
		switch a := args[i]; a {
		case "--agent":
			if i+1 < len(args) {
				i++
				agent = args[i]
			}
		case "--pane", "-p":
			if i+1 < len(args) {
				i++
				pane = args[i]
			}
		case "--json":
		default:
			if kind == "" && !strings.HasPrefix(a, "-") {
				kind = a
			}
		}
	}
	if kind == "" {
		return true
	}
	hook := readHookPayload()
	// Droid sends every Notification reason through one hook; the payload
	// says which one it is, so the pane hears a prompt, a finished turn, or
	// nothing at all.
	if agent == "droid" {
		var keep bool
		kind, keep = droidEventKind(kind, hook)
		if !keep {
			return true
		}
	}
	params := map[string]any{"kind": kind}
	if agent != "" {
		params["agent"] = agent
	}
	if pane != "" {
		params["pane"] = pane
	}
	if msg := hook.message(); msg != "" {
		params["message"] = msg
	}
	// Claude Code hands UserPromptSubmit the prompt itself, which is what a
	// pane names itself from.
	if hook.Prompt != "" {
		params["prompt"] = hook.Prompt
	}
	if hook.AgentID != "" {
		params["subagent"] = hook.AgentID
	}
	if hook.AgentType != "" {
		params["subagentType"] = hook.AgentType
	}
	if result := sendEvent(params); result != nil {
		// Claude Code adds a UserPromptSubmit hook's stdout to the prompt.
		if ctx, _ := result["context"].(string); ctx != "" {
			fmt.Print(ctx)
		}
	}
	return true
}

// hookPayload is what Claude Code writes to a hook's stdin. Message fields
// carry the agent's words; the agent fields identify one subagent so a
// SubagentStart and its SubagentStop name the same run.
type hookPayload struct {
	LastAssistantMessage string `json:"last_assistant_message"`
	Message              string `json:"message"`
	// Prompt is the text the user submitted, which UserPromptSubmit
	// carries; a pane names itself from the first one.
	Prompt    string `json:"prompt"`
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	// NotificationType is Droid's reason for a Notification: a permission
	// prompt means the agent waits on the user, an idle prompt means the
	// turn is over, and an auth success is not about the work.
	NotificationType string `json:"notification_type"`
	// StopHookActive is Droid's word that another hook already kept the
	// turn going; its Stop must not end the turn in the pane either.
	StopHookActive bool `json:"stop_hook_active"`
}

// droidEventKind turns a Droid hook into the pane event it reports. Droid
// fires Notification for three different moments, all through one hook:
//
//   - permission_prompt or elicitation_dialog: the agent is waiting on the
//     user, which is needs-input.
//   - idle_prompt: the turn is over (including a turn the user cancelled
//     with Ctrl+C), so the pane stops working.
//   - auth_success: nothing about the work, so it reports nothing.
//
// A Stop whose stop_hook_active is set is a turn another hook continued; it
// must not switch the pane's light off while work goes on.
func droidEventKind(kind string, hook hookPayload) (string, bool) {
	switch kind {
	case "turn.end":
		if hook.StopHookActive {
			return "", false
		}
	case "needs-input":
		switch hook.NotificationType {
		case "", "permission_prompt", "elicitation_dialog":
			return "needs-input", true
		case "idle_prompt":
			return "turn.end", true
		default:
			return "", false
		}
	}
	return kind, true
}

// message is what the agent said: the final reply for Stop, and the prompt it
// waits on for Notification.
func (h hookPayload) message() string {
	if h.LastAssistantMessage != "" {
		return h.LastAssistantMessage
	}
	return h.Message
}

// readHookPayload reads the payload from stdin. A manual `gexi event` has a
// terminal on stdin, and is left alone so it does not sit waiting for a line
// that will never come.
func readHookPayload() hookPayload {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return hookPayload{}
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return hookPayload{}
	}
	var in hookPayload
	if json.Unmarshal(data, &in) != nil {
		return hookPayload{}
	}
	return in
}

// sendEvent hands one event to the running window and never fails loudly: a
// hook may fire while gexi is closed, and that must not disturb the agent.
func sendEvent(params map[string]any) map[string]any {
	socket := os.Getenv("GEXI_SOCKET")
	if socket == "" {
		socket = socketPath()
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	req := map[string]any{"id": newID(), "method": "event", "params": params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil
	}
	var resp struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	if json.Unmarshal(line, &resp) != nil || !resp.OK {
		return nil
	}
	return resp.Result
}
