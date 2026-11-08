package main

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/egoist/mygo/plugins/terminal"
)

// An update restart closes the windows before the layout is saved, so the
// agent can already be gone when the resume command is read. The last
// agent's resume has to survive that, or every pane comes back as a bare
// shell.
func TestSavedCommandKeepsAgentResume(t *testing.T) {
	shell := []string{"/bin/zsh", "-l"}
	resume := shellCommand("claude --resume 11111111-1111-4111-8111-111111111111")
	p := &pane{command: shell, restoreCommand: shell, baseRestore: shell}

	p.applyAgent(&agentDetection{
		Kind:      agentClaude,
		SessionID: "11111111-1111-4111-8111-111111111111",
		Resume:    resume,
	})
	if got := p.savedCommand(); !slices.Equal(got, resume) {
		t.Fatalf("with the agent running: saved %v", got)
	}

	p.applyAgent(nil)
	if got := p.savedCommand(); !slices.Equal(got, resume) {
		t.Fatalf("after the agent exited: saved %v, want %v", got, resume)
	}
}

// A pane's `zsh -lc` never reads ~/.zshrc, so the PATH the interactive
// shell builds is what keeps a tool installed in ~/.local/bin — claude —
// startable from a gexi opened in Finder.
func TestMergePaths(t *testing.T) {
	cases := []struct{ shell, inherited, want string }{
		{"", "/usr/bin", "/usr/bin"},
		{"/Users/me/.local/bin:/usr/bin", "/usr/bin:/bin", "/Users/me/.local/bin:/usr/bin:/bin"},
		{"/usr/bin::/usr/bin", "/bin", "/usr/bin:/bin"},
		{"/opt/homebrew/bin", "", "/opt/homebrew/bin"},
	}
	for _, c := range cases {
		if got := mergePaths(c.shell, c.inherited); got != c.want {
			t.Errorf("mergePaths(%q, %q) = %q, want %q", c.shell, c.inherited, got, c.want)
		}
	}
}

func TestWithPath(t *testing.T) {
	env := []string{"HOME=/Users/me", "PATH=/usr/bin:/bin", "TERM=dumb"}
	got := withPath("/Users/me/.local/bin:/usr/bin", env)
	if want := "PATH=/Users/me/.local/bin:/usr/bin:/bin"; got[1] != want {
		t.Fatalf("PATH = %q, want %q", got[1], want)
	}
	if got[0] != "HOME=/Users/me" || got[2] != "TERM=dumb" {
		t.Fatalf("other variables changed: %q", got)
	}

	// No PATH at all: the shell's is added.
	got = withPath("/usr/bin", []string{"HOME=/Users/me"})
	if want := []string{"HOME=/Users/me", "PATH=/usr/bin"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A failed probe leaves the environment alone.
	env = []string{"PATH=/usr/bin"}
	if got := withPath("", env); !slices.Equal(got, env) {
		t.Fatalf("empty shell path changed env: %q", got)
	}
}

func TestResumesAgent(t *testing.T) {
	cases := []struct {
		command []string
		want    bool
	}{
		{shellCommand("claude --resume 11111111-1111-4111-8111-111111111111"), true},
		{shellCommand("codex resume --last"), true},
		{shellCommand("omp --continue"), true},
		{shellCommand("pi --continue"), true},
		{shellCommand("droid --resume --last"), true},
		{[]string{"/bin/zsh", "-l"}, false},
		{shellCommand("claude"), false},
	}
	for _, c := range cases {
		if got := resumesAgent(c.command); got != c.want {
			t.Fatalf("resumesAgent(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

// A mirror asks for the output after an offset; the log keeps the last
// chunks by absolute offset and says when the offset it is asked for is
// gone, so the client starts over from a snapshot instead of missing bytes.
func TestPTYLogOffsets(t *testing.T) {
	c := newPTYConn()
	c.log([]byte("hello "))
	c.log([]byte("world"))
	if got, ok := c.outputSince(6); !ok || string(got) != "world" {
		t.Fatalf("since 6: %q, %v", got, ok)
	}
	if got, ok := c.outputSince(0); !ok || string(got) != "hello world" {
		t.Fatalf("since 0: %q, %v", got, ok)
	}
	if got, ok := c.outputSince(11); !ok || len(got) != 0 {
		t.Fatalf("since the end: %q, %v", got, ok)
	}
	if _, ok := c.outputSince(12); ok {
		t.Fatal("an offset past the log reads as kept")
	}

	// Letting go of the oldest chunks moves the start: an offset before it
	// is gone, one inside the tail still reads.
	c.out = nil
	c.outBytes = 0
	c.outStart = 0
	c.outEnd = 0
	big := make([]byte, paneOutputLog/2+1)
	for i := 0; i < 3; i++ {
		c.log(big)
	}
	if c.outStart == 0 || c.outStart >= c.outEnd {
		t.Fatalf("log did not let go: start %d, end %d", c.outStart, c.outEnd)
	}
	if _, ok := c.outputSince(0); ok {
		t.Fatal("an offset the log let go of reads as kept")
	}
	if got, ok := c.outputSince(c.outEnd - 10); !ok || len(got) != 10 {
		t.Fatalf("the tail: %d bytes, %v", len(got), ok)
	}
}

// A remote mirror starts from a snapshot of the pane and goes on with the
// output after it: a terminal fed both shows what the pane's terminal
// shows, as the remote window does.
func TestPaneMirrorReplaysFromSnapshot(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c",
		"printf '\\033[31mred\\033[0m line\\n'; sleep 0.2; printf 'second\\n'")
	f, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	conn := newPTYConn()
	conn.attach(f)
	term, err := terminal.New(terminal.Options{Conn: conn, Scrollback: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Resize(40, 10)

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(term.Text(), "second") {
		if time.Now().After(deadline) {
			t.Fatalf("the program's output never showed: %q", term.Text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	off, snap := conn.snapshot(term)
	delta, ok := conn.outputSince(off)
	if !ok {
		t.Fatalf("the log let go of the snapshot's offset %d", off)
	}

	pipe := newMirrorConn()
	sink, err := terminal.New(terminal.Options{Conn: pipe, Scrollback: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	sink.Resize(40, 10)
	sink.Feed(snap)
	sink.Feed(delta)
	if got, want := sink.Text(), term.Text(); got != want {
		t.Fatalf("the mirror shows\n%q\nwant\n%q", got, want)
	}
}
