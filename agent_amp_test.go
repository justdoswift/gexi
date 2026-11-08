package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAmpKind(t *testing.T) {
	if k := agentNamed("amp"); k != agentAmp {
		t.Errorf("agentNamed(amp) = %q", k)
	}
	if agentAmp.name() != "Amp" {
		t.Errorf("name = %q", agentAmp.name())
	}
	launch := agentAmp.launch()
	if len(launch) < 3 || launch[2] != "amp" {
		t.Errorf("launch = %v", launch)
	}
	if mark, ok := logoMarks["amp"]; !ok || mark.body == "" {
		t.Error("amp has no logo mark")
	}
	if logoSource("amp", "#ffffff") == nil {
		t.Error("amp logo does not decode")
	}
}

// TestAmpResume: Amp has no flag that names a new thread, so a fresh launch
// falls back to the newest thread in the checkout. A command that already
// continues one is left alone.
func TestAmpResume(t *testing.T) {
	fresh := resolveAgentCommand(shellCommand("amp -m high"))
	raw, _ := rawCommand(fresh.launch)
	if raw != "amp -m high" {
		t.Errorf("launch = %q, want Amp as typed", raw)
	}
	restore, _ := rawCommand(fresh.restore)
	if restore != "amp -m high threads continue --last" {
		t.Errorf("restore = %q", restore)
	}
	if !resumesAgent(fresh.restore) {
		t.Errorf("resume command not recognized: %q", restore)
	}

	for _, raw := range []string{
		"amp threads continue --last",
		"amp threads continue T-01a1bb80-f175-7439-9033-6676481a5939",
		"amp last",
	} {
		resolved := resolveAgentCommand(shellCommand(raw))
		if launch, _ := rawCommand(resolved.launch); launch != raw {
			t.Errorf("%s: launch = %q", raw, launch)
		}
		if restore, _ := rawCommand(resolved.restore); restore != raw {
			t.Errorf("%s: restore = %q", raw, restore)
		}
	}
}

// TestAmpEventPinsThread: the plugin reports the thread Amp opened, and that
// id outlives the agent even when detection only knows the binary.
func TestAmpEventPinsThread(t *testing.T) {
	p := &pane{ID: "ab12", command: shellCommand("amp"), restoreCommand: shellCommand("amp"), baseRestore: shellCommand("amp")}
	s := &session{ID: "s1", Root: leaf(p)}
	w := &workspace{Projects: []*project{{Sessions: []*session{s}}}}

	thread := "T-01a1bb80-f175-7439-9033-6676481a5939"
	if _, err := w.handleEvent(agentEvent{
		Kind: "session.start", Agent: "amp", Pane: "ab12", Session: thread,
	}); err != nil {
		t.Fatalf("session.start: %v", err)
	}
	want := shellCommand("amp threads continue " + shellQuote(thread))
	if !slices.Equal(p.restoreCommand, want) || !slices.Equal(p.lastAgentRestore, want) {
		t.Fatalf("restore = %q, want %q", p.restoreCommand[2], want[2])
	}
	if p.agent == nil || p.agent.Kind != agentAmp || p.agent.SessionID != thread {
		t.Fatalf("agent = %+v, want Amp's thread", p.agent)
	}

	// Detection sees the process but not the thread. It must keep the exact
	// one the event reported.
	if p.applyAgent(&agentDetection{Kind: agentAmp, Resume: shellCommand("amp threads continue --last")}) {
		t.Error("a session-less detection replaced the thread")
	}
	if !slices.Equal(p.restoreCommand, want) {
		t.Fatalf("restore after detection = %q", p.restoreCommand[2])
	}

	// The agent exits; the next launch still returns to the same thread.
	p.applyAgent(nil)
	if got := p.savedCommand(); !slices.Equal(got, want) {
		t.Fatalf("saved command = %q, want %q", got[2], want[2])
	}
	// A late session.end closes the turn but must not put the agent back.
	if _, err := w.handleEvent(agentEvent{
		Kind: "session.end", Agent: "amp", Pane: "ab12", Session: thread,
	}); err != nil {
		t.Fatalf("session.end: %v", err)
	}
	if p.agent != nil {
		t.Fatalf("session.end revived the agent: %+v", p.agent)
	}
}

func TestAmpPluginInstalled(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	path := ampEventsFile()
	if want := filepath.Join(config, "amp", "plugins", "gexi-events.ts"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if got, err := writeAmpEvents(path); err != nil || got != path {
		t.Fatalf("write = %q, %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if strings.Contains(src, "child_process") || strings.Contains(src, "__GEXI_BIN__") {
		t.Error("plugin launches a helper process")
	}
	for _, want := range []string{
		`import { connect } from "node:net"`,
		`export const description = "Reports Amp turns to a gexi pane"`,
		`if (!process.env.GEXI_PANE) return`,
		`amp.on("session.start"`, `report("session.start", "", event.thread.id)`,
		`ctx.thread.state.subscribe`, `report("needs-input", "", event.thread.id)`,
		`amp.on("agent.start"`, `await report("turn.start", event.message, event.thread.id)`,
		`if (current && event.thread.id !== current) return`,
		`return { message: { content: context } }`,
		`amp.on("agent.end"`, `report("turn.end", last, event.thread.id)`,
		`amp.onDispose(() => report("session.end", "", current))`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("plugin is missing %s", want)
		}
	}
}

func TestAmpHeadless(t *testing.T) {
	command, ok := agentHeadless(agentAmp)
	if !ok {
		t.Fatal("Amp has no headless command")
	}
	if !slices.Equal(command, []string{"amp", "-x"}) {
		t.Fatalf("command = %v, want amp -x", command)
	}
}

// TestAmpThreadFromResume: a saved pane resumes its exact thread, so the
// pane knows its session before the process is up and --last cannot replace it.
func TestAmpThreadFromResume(t *testing.T) {
	thread := "T-01a1bb80-f175-7439-9033-6676481a5939"
	det := agentFromResume(shellCommand("amp threads continue " + shellQuote(thread)))
	if det == nil || det.Kind != agentAmp || det.SessionID != thread {
		t.Fatalf("detection = %+v, want Amp's thread", det)
	}
	if got := ampThreadID("amp threads continue --last"); got != "" {
		t.Fatalf("--last named thread %q", got)
	}
	if got := ampThreadID("amp -m high threads continue " + thread); got != thread {
		t.Fatalf("thread after flags = %q, want %q", got, thread)
	}

	// A monitor tick that sees the shell before Amp starts clears the
	// detection. The next session-less detection must bring the saved exact
	// thread back rather than replacing it with --last.
	exact := shellCommand("amp threads continue " + shellQuote(thread))
	p := &pane{
		command:          exact,
		restoreCommand:   exact,
		baseRestore:      exact,
		lastAgentRestore: exact,
	}
	p.applyAgent(nil)
	if p.applyAgent(&agentDetection{Kind: agentAmp, Resume: shellCommand("amp threads continue --last")}) {
		t.Error("generic detection reported a change over a saved thread")
	}
	if p.agent == nil || p.agent.SessionID != thread {
		t.Fatalf("agent = %+v, want the saved thread", p.agent)
	}
	if !slices.Equal(p.restoreCommand, exact) {
		t.Fatalf("restore = %q, want %q", p.restoreCommand[2], exact[2])
	}
}
