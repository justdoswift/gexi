package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/egoist/mygo/plugins/terminal"
)

// ptyConn is the in-memory backend: Gexi owns the PTY, so the terminal
// reads every byte the shell writes. That is also where agent detection gets
// its master fd.
type ptyConn struct {
	mu      sync.Mutex
	f       *os.File
	ready   chan struct{}
	pending *pty.Winsize
}

func newPTYConn() *ptyConn { return &ptyConn{ready: make(chan struct{})} }

// attach starts the flow; reads and writes wait until it happens.
func (c *ptyConn) attach(f *os.File) {
	c.mu.Lock()
	c.f = f
	size := c.pending
	c.mu.Unlock()
	if size != nil {
		_ = pty.Setsize(f, size)
	}
	close(c.ready)
}

func (c *ptyConn) Read(p []byte) (int, error) {
	<-c.ready
	return c.f.Read(p)
}

func (c *ptyConn) Write(p []byte) (int, error) {
	<-c.ready
	return c.f.Write(p)
}

func (c *ptyConn) Close() error {
	c.mu.Lock()
	f := c.f
	c.mu.Unlock()
	if f != nil {
		return f.Close()
	}
	return nil
}

func (c *ptyConn) Resize(cols, rows int) error {
	size := &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	c.mu.Lock()
	if c.f == nil {
		c.pending = size
		c.mu.Unlock()
		return nil
	}
	f := c.f
	c.mu.Unlock()
	return pty.Setsize(f, size)
}

// pane is one terminal: our PTY, mygo's Ghostty-backed terminal, and what the
// control API reports.
type pane struct {
	ID             string
	term           *terminal.Terminal
	conn           *ptyConn
	ptmx           *os.File
	cmd            *exec.Cmd
	initialDir     string
	command        []string // what this pane was launched with
	restoreCommand []string // what the next launch should run
	baseRestore    []string // restoreCommand with no agent running
	agent          *agentDetection
	activity       *agentActivity // last hook event from the agent
	lastGroup      int
	lastScan       time.Time
	openedAt       time.Time
	closing        bool
	closingAt      time.Time
	exited         bool
	ws             *workspace
}

func newPane(ws *workspace, id string, command []string, dir string, snapshot string) (*pane, error) {
	if id == "" {
		id = newID()
	}
	if len(command) == 0 {
		command = []string{loginShell(), "-l"}
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	resolved := resolveAgentCommand(command)

	p := &pane{
		ID:             id,
		openedAt:       time.Now(),
		initialDir:     dir,
		command:        resolved.launch,
		restoreCommand: resolved.restore,
		baseRestore:    resolved.restore,
		ws:             ws,
		conn:           newPTYConn(),
	}

	term, err := terminal.New(terminal.Options{
		Conn: connOf(p),
		Env: []string{
			"GEXI_PANE=" + id,
			"GEXI_SESSION=" + ws.SessionID,
			"GEXI_SOCKET=" + socketPath(),
			"GEXI_HOME=" + supportDir(),
		},
		Scrollback: 10 << 20,
		Padding:    terminal.Padding{X: 12, Y: 10},
		Font: terminal.Font{
			Family:     "SF Mono, Menlo, monospace",
			Size:       12,
			Weight:     400,
			LineHeight: 1.2,
		},
		OnTitle: func(string) {
			ws.invalidate()
		},
		OnExit: func(int) {
			p.exited = true
			ws.invalidate()
		},
	})
	if err != nil {
		return nil, err
	}
	p.term = term

	// Paint the last screen above the new shell, before it writes anything.
	// Plain text, not raw PTY bytes: replaying those reflows with the width.
	if snapshot != "" {
		term.Feed([]byte(strings.ReplaceAll(snapshot, "\n", "\r\n") + "\r\n"))
	}

	if err := p.start(resolved.launch, dir); err != nil {
		_ = term.Close()
		return nil, err
	}
	return p, nil
}

func connOf(p *pane) *ptyConn { return p.conn }

func (p *pane) start(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(cleanEnvironment(os.Environ()),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=Gexi",
		"GEXI_PANE="+p.ID,
		"GEXI_SESSION="+p.ws.SessionID,
		"GEXI_SOCKET="+socketPath(),
		"GEXI_HOME="+supportDir(),
	)

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		return err
	}
	p.cmd = cmd
	p.ptmx = f
	p.conn.attach(f)

	go func() {
		_ = cmd.Wait()
		p.exited = true
		p.ws.paneExited(p)
	}()
	return nil
}

// foregroundGroup reads the PTY's foreground process group; 0 once it is gone.
func (p *pane) foregroundGroup() int {
	if p.ptmx == nil {
		return 0
	}
	return foregroundGroupOf(p.ptmx.Fd())
}

func (p *pane) title() string {
	if t := strings.TrimSpace(p.term.Title()); t != "" {
		return t
	}
	if len(p.command) > 0 {
		return filepath.Base(p.command[0])
	}
	return "Terminal"
}

// dir prefers the directory the shell reported (OSC 7), as Gexi does.
func (p *pane) dir() string {
	if d := p.term.Dir(); d != "" {
		return d
	}
	if p.initialDir != "" {
		return p.initialDir
	}
	home, _ := os.UserHomeDir()
	return home
}

// applyAgent records a detection on the main thread and reports whether the
// restore command changed.
func (p *pane) applyAgent(det *agentDetection) bool {
	if det != nil {
		if p.agent != nil && p.agent.Kind == det.Kind && p.agent.SessionID == det.SessionID {
			return false
		}
		p.agent = det
		p.restoreCommand = det.Resume
		return true
	}
	if p.agent != nil {
		p.agent = nil
		p.restoreCommand = p.baseRestore
		return true
	}
	return false
}

func (p *pane) send(text string) { p.term.Send([]byte(text)) }

func (p *pane) text() string { return p.term.Text() }

func (p *pane) close() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGHUP)
	}
	if p.ptmx != nil {
		_ = p.ptmx.Close()
	}
	_ = p.term.Close()
}

// cleanEnvironment drops the markers of a Claude Code host when the app was
// started from inside one. Without this every pane inherits them, and a
// `claude` started there believes it is a child session: it keeps the host's
// provider variables instead of loading ~/.claude/settings.json.
func cleanEnvironment(env []string) []string {
	host := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDECODE=") {
			host = true
			break
		}
	}
	if !host {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case key == "CLAUDECODE", key == "CLAUDE_PID", key == "CLAUDE_EFFORT",
			strings.HasPrefix(key, "CLAUDE_CODE_"),
			strings.HasPrefix(key, "CLAUDE_AGENT_SDK_"),
			strings.HasPrefix(key, "ANTHROPIC_"):
			continue
		}
		out = append(out, kv)
	}
	return out
}
