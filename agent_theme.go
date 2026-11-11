package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// syncAgentThemes hands the appearance switch to the agents that keep a
// light or dark theme of their own. gexi's terminal answers the background
// color query (OSC 11) with the appearance in force and reports each change
// (mode 2031), so an agent whose theme is auto turns with the app.
//
// Amp needs nothing here: it enables mode 2031 itself and re-queries the
// palette when the colors change.
func syncAgentThemes() {
	// Claude Code: a fixed light or dark theme becomes auto. A custom theme
	// is the user's own colors, so it stays as it is.
	_ = setAgentThemeAuto(globalClaudeSettingsPath(), func(current string) bool {
		return current == "" || strings.HasPrefix(current, "light") || strings.HasPrefix(current, "dark")
	})
	// Perch: auto is already its default; only a pinned light or dark moves.
	_ = setAgentThemeAuto(perchSettingsPath(), func(current string) bool {
		return current == "light" || current == "dark"
	})
}

// perchSettingsPath is where the Perch CLI keeps its settings, the file its
// theme row writes.
func perchSettingsPath() string {
	if dir := strings.TrimSpace(os.Getenv("PERCH_CLI_SETTINGS_DIR")); dir != "" {
		return filepath.Join(dir, "cli-settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".perch", "cli-settings.json")
}

// setAgentThemeAuto sets the theme field of a JSON settings file to auto,
// when the file exists and its current theme is one follow accepts. Every
// other field is kept as it was.
func setAgentThemeAuto(path string, follow func(current string) bool) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	current := ""
	if raw, ok := top["theme"]; ok {
		if err := json.Unmarshal(raw, &current); err != nil {
			return err
		}
	}
	if !follow(current) {
		return nil
	}
	top["theme"] = json.RawMessage(`"auto"`)
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}
