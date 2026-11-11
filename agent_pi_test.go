package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestPiScript(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"managed install", []string{"node", "/Users/x/.pi/agent/install/releases/1.5.0/node_modules/.bin/pi"}, true},
		{"node with flag", []string{"node", "--max-old-space-size=4096", "/Users/x/.nvm/versions/node/v22.20.0/bin/pi"}, true},
		{"package bundle", []string{"node", "/x/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js"}, true},
		{"plain node", []string{"node", "server.js"}, false},
		{"node mentioning pi", []string{"node", "/app/pi-server.js"}, false},
		{"perch", []string{"node", "/Users/x/.nvm/versions/node/v22.20.0/bin/perch"}, false},
		{"pi flag", []string{"node", "/x/bin/pi", "--continue"}, true},
		{"empty", nil, false},
	}
	for _, c := range cases {
		got := piScript(c.args) != ""
		if got != c.want {
			t.Errorf("%s: piScript(%v) = %v, want %v", c.name, c.args, got, c.want)
		}
	}
}

func TestPiKind(t *testing.T) {
	if k := agentNamed("pi"); k != agentPi {
		t.Errorf("agentNamed(pi) = %q", k)
	}
	if agentPi.name() != "Pi" {
		t.Errorf("name = %q", agentPi.name())
	}
	launch := agentPi.launch()
	if len(launch) == 0 || !strings.HasPrefix(launch[2], "pi -e ") {
		t.Errorf("launch = %v", launch)
	}
	if mark, ok := logoMarks["pi"]; !ok || mark.body == "" {
		t.Error("pi has no logo mark")
	}
}

// TestPiEventsExtension: the extension speaks the socket itself, so no
// helper process is launched from inside Pi.
func TestPiEventsExtension(t *testing.T) {
	path, err := installPiEvents()
	if err != nil {
		t.Fatal(err)
	}
	if path != piEventsFile() {
		t.Errorf("path = %q, want %q", path, piEventsFile())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if strings.Contains(src, "child_process") || strings.Contains(src, "__GEXI_BIN__") {
		t.Error("extension still launches a process")
	}
	for _, want := range []string{
		`import { connect } from "node:net"`,
		`pi.on("session_start"`, `report("session.start")`,
		`pi.on("input"`, `await report("turn.start", event.text)`,
		`pi.on("agent_settled"`, `report("turn.end", last)`,
		`pi.on("ui_prompt_start"`, `report("needs-input")`,
		`pi.on("session_shutdown"`, `report("session.end")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("extension is missing %s", want)
		}
	}
}

func TestPiResume(t *testing.T) {
	fresh := resolveAgentCommand(shellCommand("pi --model sonnet"))
	launch, restore := fresh.launch[2], fresh.restore[2]
	if !strings.HasPrefix(launch, "pi --model sonnet -e ") || !strings.Contains(launch, piEventsFile()) {
		t.Errorf("launch = %q, want the typed flags plus the event extension", launch)
	}
	if strings.Contains(launch, "--continue") {
		t.Errorf("launch = %q, want a fresh conversation", launch)
	}
	if !strings.HasSuffix(restore, "--continue") || !strings.Contains(restore, piEventsFile()) {
		t.Errorf("restore = %q, want the extension and --continue", restore)
	}

	// The flags that already resume are left alone.
	for _, raw := range []string{"pi --continue", "pi -c"} {
		resolved := resolveAgentCommand(shellCommand(raw))
		if !slices.Equal(resolved.restore, resolved.launch) {
			t.Errorf("%s: restore = %q, want unchanged", raw, resolved.restore[2])
		}
		if !strings.Contains(resolved.launch[2], piEventsFile()) {
			t.Errorf("%s: launch = %q, want the extension", raw, resolved.launch[2])
		}
	}
}
