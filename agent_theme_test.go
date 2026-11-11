package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A switch of appearance moves Claude and Perch from a pinned light or dark
// theme to auto, so the terminal's own report carries the change to them.
func TestSyncAgentThemesFollowsAppearance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERCH_CLI_SETTINGS_DIR", "")

	claude := filepath.Join(home, ".claude", "settings.json")
	writeTestFile(t, claude, `{"model":"opus","theme":"light-daltonized"}`)
	perch := filepath.Join(home, ".perch", "cli-settings.json")
	writeTestFile(t, perch, `{"version":1,"language":"zh-CN","theme":"dark"}`)

	syncAgentThemes()

	if got := readTestTheme(t, claude); got != "auto" {
		t.Fatalf("Claude theme is %q, want auto", got)
	}
	if got := readTestField(t, claude, "model"); got != "opus" {
		t.Fatalf("Claude's model is %q, want opus", got)
	}
	if got := readTestTheme(t, perch); got != "auto" {
		t.Fatalf("Perch theme is %q, want auto", got)
	}
	if got := readTestField(t, perch, "language"); got != "zh-CN" {
		t.Fatalf("Perch's language is %q, want zh-CN", got)
	}
	if got := readTestField(t, perch, "version"); got != "1" {
		t.Fatalf("Perch's version is %q, want 1", got)
	}
}

// A theme the user made their own is not a light or dark pick, and stays.
func TestSyncAgentThemesKeepsCustomTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERCH_CLI_SETTINGS_DIR", "")

	claude := filepath.Join(home, ".claude", "settings.json")
	writeTestFile(t, claude, `{"theme":"custom:mint"}`)
	perch := filepath.Join(home, ".perch", "cli-settings.json")
	writeTestFile(t, perch, `{"version":1,"theme":"auto"}`)

	syncAgentThemes()

	if got := readTestTheme(t, claude); got != "custom:mint" {
		t.Fatalf("Claude theme is %q, want the custom theme kept", got)
	}
	if got := readTestTheme(t, perch); got != "auto" {
		t.Fatalf("Perch theme is %q, want auto", got)
	}
}

// An agent that never wrote settings keeps none: gexi only edits a file that
// is already there.
func TestSyncAgentThemesLeavesMissingSettingsAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERCH_CLI_SETTINGS_DIR", "")

	syncAgentThemes()

	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".perch", "cli-settings.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was written, want it left missing", path)
		}
	}
}

// A settings file gexi cannot parse is left exactly as it is.
func TestSyncAgentThemesKeepsBrokenSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERCH_CLI_SETTINGS_DIR", "")

	claude := filepath.Join(home, ".claude", "settings.json")
	writeTestFile(t, claude, `{"theme":`)

	syncAgentThemes()

	data, err := os.ReadFile(claude)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"theme":`; got != want {
		t.Fatalf("Claude settings are %q, want them untouched", got)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestTheme(t *testing.T, path string) string {
	t.Helper()
	return readTestField(t, path, "theme")
}

func readTestField(t *testing.T, path, field string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(top[field], &value); err != nil {
		t.Fatal(err)
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return json.Number(string(top[field])).String()
	default:
		t.Fatalf("%s is %T, want a string or number", field, value)
		return ""
	}
}
