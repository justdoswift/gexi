package main

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	params := map[string]any{"kind": kind}
	if agent != "" {
		params["agent"] = agent
	}
	if pane != "" {
		params["pane"] = pane
	}
	if result := sendEvent(params); result != nil {
		// Claude Code adds a UserPromptSubmit hook's stdout to the prompt.
		if ctx, _ := result["context"].(string); ctx != "" {
			fmt.Print(ctx)
		}
	}
	return true
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
