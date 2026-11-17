package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Droid reports its work through hooks too, but where Claude Code's settings
// layers can carry a hooks file of their own, gexi hands Droid a settings
// overlay at launch: `droid --settings <file>`. The overlay lives in gexi's
// support folder, never in ~/.factory, so the user's own hooks keep running
// and nothing gexi needs is written into their configuration.
//
// Only a droid gexi launches gets the overlay. A droid typed into a pane by
// hand has no overlay and reports nothing, so gexi shows no light for it.
func droidHookSettingsPath() string {
	return filepath.Join(supportDir(), "hooks", "droid-settings.json")
}

// droidHookKinds pairs each Droid hook event with the gexi event it reports.
// Droid has no SubagentStart, so a subagent count is not part of this.
var droidHookKinds = []struct{ event, kind string }{
	{"SessionStart", "session.start"},
	{"UserPromptSubmit", "turn.start"},
	{"Stop", "turn.end"},
	// Notification carries the reason; eventcmd narrows it to needs-input,
	// turn.end, or nothing at all.
	{"Notification", "needs-input"},
	{"SessionEnd", "session.end"},
}

// installDroidHooks writes the settings overlay gexi passes to
// `droid --settings`. It is rewritten on each launch so a moved binary or a
// new support folder is picked up, and it is the only file gexi adds for
// Droid.
func installDroidHooks() (string, error) {
	bin := gexiBinary()
	if bin == "" {
		return "", fmt.Errorf("gexi binary not found")
	}
	path := droidHookSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	hooks := map[string]any{}
	for _, h := range droidHookKinds {
		command := shellQuote(bin) + " event " + h.kind + " --agent droid"
		hooks[h.event] = []any{hookEntry(command, "")}
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
