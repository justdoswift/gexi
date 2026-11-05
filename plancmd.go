package main

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

// runPlanCommand answers `Gexi plan` from a Claude Code PreToolUse hook. The
// hook pipes the tool call on stdin; this forwards it to the running window
// and exits at once, so the agent never waits on the UI.
func runPlanCommand(args []string) bool {
	if len(args) == 0 || args[0] != "plan" {
		return false
	}
	agent, pane := "claude", os.Getenv("GEXI_PANE")
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
		}
	}
	detail := readHookPayload()
	if detail == nil {
		return true
	}
	params := map[string]any{"detail": detail}
	if agent != "" {
		params["agent"] = agent
	}
	if pane != "" {
		params["pane"] = pane
	}
	sendQuiet("plan.update", params)
	return true
}

// readHookPayload reads the JSON a hook pipes in. A person running the
// command by hand has no pipe, and must not block; a pipe that stays open
// gets only a moment to deliver.
func readHookPayload() map[string]any {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	ch := make(chan []byte, 1)
	go func() {
		raw, _ := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
		ch <- raw
	}()
	var raw []byte
	select {
	case raw = <-ch:
	case <-time.After(250 * time.Millisecond):
		return nil
	}
	var detail map[string]any
	if json.Unmarshal(raw, &detail) != nil {
		return nil
	}
	return detail
}
