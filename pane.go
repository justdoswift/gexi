package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/egoist/mygo/plugins/terminal"
)

// ptyConn is the in-memory backend: gexi owns the PTY, so the terminal
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
	// lastAgentRestore is the resume command of the last agent seen in this
	// pane. It outlives the agent: the next launch brings the conversation
	// back even when the agent had already exited when the layout was saved.
	lastAgentRestore []string
	// restored marks a pane built from the saved layout. A program that
	// fails to come up in it keeps the pane instead of closing it.
	restored bool
	sawAgent bool           // an agent was detected at some point in this pane
	activity *agentActivity // last hook event from the agent
	// subagents are the Task/Agent calls the agent reports running in this
	// pane, keyed by the id a start and its stop share. A background one
	// outlives its turn, which is what keeps the session visibly busy after
	// the prompt has returned.
	subagents map[string]paneSubagent
	// Worktree is the checkout this pane was moved into by its W button, or
	// one its folder was found in. worktreeBusy is the operation it waits
	// on: "create".
	Worktree     *paneWorktree
	worktreeBusy string
	// lastWorktreeDir is the pane folder the worktree monitor last looked
	// at; lastWorktreeRefresh throttles the pull request lookups that keep
	// a detected checkout's chip current.
	lastWorktreeDir     string
	lastWorktreeRefresh time.Time
	lastGroup           int
	lastScan            time.Time
	openedAt            time.Time
	shown               bool // the first frame has laid the pane out
	closing             bool
	closingAt           time.Time
	exited              bool
	// headerActionsW is the width the pane's action buttons took in the
	// frame before, so a header can set its path and branch pill beside the
	// room they leave rather than by fixed thresholds.
	headerActionsW float32
	ws             *workspace
}

// paneSubagent is one Task/Agent call running under the pane's agent. Type is
// the agent definition it runs, for the CLI to report; the sidebar only needs
// how many there are.
type paneSubagent struct {
	Type  string
	Since time.Time
}

// subagentKey pairs a start with its stop. Claude Code always sends the id;
// the type stands in when an event arrives without one.
func subagentKey(id, typ string) string {
	if id != "" {
		return id
	}
	return "type:" + typ
}

func (p *pane) startSubagent(id, typ string) {
	if p.subagents == nil {
		p.subagents = map[string]paneSubagent{}
	}
	p.subagents[subagentKey(id, typ)] = paneSubagent{Type: typ, Since: time.Now()}
}

func (p *pane) endSubagent(id, typ string) {
	delete(p.subagents, subagentKey(id, typ))
}

func (p *pane) clearSubagents() {
	clear(p.subagents)
}

func (p *pane) subagentCount() int { return len(p.subagents) }

// subagentsJSON lists the running subagents oldest first, so the CLI's output
// stays stable between calls.
func (p *pane) subagentsJSON() []map[string]any {
	out := make([]map[string]any, 0, len(p.subagents))
	for id, a := range p.subagents {
		out = append(out, map[string]any{
			"id":    id,
			"type":  a.Type,
			"since": a.Since.Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["since"] != out[j]["since"] {
			return out[i]["since"].(string) < out[j]["since"].(string)
		}
		return out[i]["id"].(string) < out[j]["id"].(string)
	})
	return out
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
	// A remote shell reports its own path through OSC 7, and that path does
	// not exist here. The ssh process still has to start in a local folder.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
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
	// A command that resumes an agent on launch is already what the next
	// launch should run, even before the monitor ever sees the process.
	if resumesAgent(resolved.restore) {
		p.lastAgentRestore = append([]string(nil), resolved.restore...)
	}

	term, err := terminal.New(terminal.Options{
		Conn: connOf(p),
		Env: []string{
			"GEXI_PANE=" + id,
			"GEXI_INSTANCE=" + ws.InstanceID,
			"GEXI_SOCKET=" + socketPath(),
			"GEXI_HOME=" + supportDir(),
		},
		Scrollback: 10 << 20,
		Padding:    terminal.Padding{X: 12, Y: 10},
		// The pane card is already the terminal's background colour, and
		// leaving the grid's own background undrawn lets the card change
		// colour while its pane is picked up, without the grid covering it.
		Transparent: true,
		Font: terminal.Font{
			Family:     terminalFont + ", SF Mono, Menlo, monospace",
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

// resumesAgent tells a command that brings a conversation back from one that
// starts fresh: claude --resume, codex resume --last, omp --continue, and the
// like.
func resumesAgent(command []string) bool {
	for _, arg := range command {
		if strings.Contains(arg, "--resume") || strings.Contains(arg, "--continue") || strings.Contains(arg, " resume") {
			return true
		}
	}
	return false
}

func (p *pane) start(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(withInteractivePath(cleanEnvironment(os.Environ())),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=gexi",
		"GEXI_PANE="+p.ID,
		"GEXI_INSTANCE="+p.ws.InstanceID,
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

// titleSpinners are the progress marks an agent puts in front of the terminal
// title while it works; Claude Code cycles through these quarter circles.
// Chrome already says an agent is working with the session label's shimmer,
// so the mark is noise beside the words and is dropped.
const titleSpinners = "◐◑◒◓"

// cleanTitle trims a terminal title and drops the agent's leading activity
// mark, leaving the words themselves.
func cleanTitle(title string) string {
	t := strings.TrimSpace(title)
	for _, spinner := range titleSpinners {
		if rest, ok := strings.CutPrefix(t, string(spinner)); ok {
			return strings.TrimSpace(rest)
		}
	}
	return t
}

func (p *pane) title() string {
	if t := cleanTitle(p.term.Title()); t != "" {
		return t
	}
	if len(p.command) > 0 {
		return filepath.Base(p.command[0])
	}
	return "Terminal"
}

// dir prefers the directory the shell reported (OSC 7), as gexi does.
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
		p.sawAgent = true
		if p.agent != nil && p.agent.Kind == det.Kind && p.agent.SessionID == det.SessionID {
			return false
		}
		p.agent = det
		p.restoreCommand = det.Resume
		p.lastAgentRestore = det.Resume
		return true
	}
	if p.agent != nil {
		p.agent = nil
		p.restoreCommand = p.baseRestore
		return true
	}
	return false
}

// savedCommand is what the next launch runs. An agent's resume command wins
// even after the agent exited: an update restart kills the agents first, and
// saving the bare shell there is what made every pane come back empty.
func (p *pane) savedCommand() []string {
	if len(p.lastAgentRestore) > 0 {
		return p.lastAgentRestore
	}
	return p.restoreCommand
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

// A pane runs `zsh -lc`, a login shell: it reads profile files but not
// ~/.zshrc, where many tools (claude among them) add their directory to
// PATH. Opened from Finder, gexi also inherits launchd's short PATH, so
// those tools vanish from every pane. The user's interactive shell knows
// the real PATH; ask it once, with a deadline, and keep the inherited PATH
// if it does not answer.

const (
	pathProbeStart = "__GEXI_PATH__"
	pathProbeEnd   = "__GEXI_PATH_END__"
)

var interactivePathOnce struct {
	sync.Once
	path string
}

func interactivePath() string {
	interactivePathOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		probe := "printf '" + pathProbeStart + "%s" + pathProbeEnd + "' \"$PATH\""
		cmd := exec.CommandContext(ctx, loginShell(), "-ilc", probe)
		cmd.Env = cleanEnvironment(os.Environ())
		cmd.WaitDelay = time.Second
		cmd.Stderr = io.Discard
		out, _ := cmd.Output()

		s := string(out)
		start := strings.Index(s, pathProbeStart)
		end := strings.Index(s, pathProbeEnd)
		if start < 0 || end <= start {
			return
		}
		if v := s[start+len(pathProbeStart) : end]; strings.Contains(v, "/") {
			interactivePathOnce.path = v
		}
	})
	return interactivePathOnce.path
}

// mergePaths puts the shell's directories first, then any the inherited
// environment has and the shell did not, so a probe that came back short
// never removes what already worked.
func mergePaths(shell, inherited string) string {
	if shell == "" {
		return inherited
	}
	seen := make(map[string]bool)
	out := make([]string, 0, strings.Count(shell, ":")+1)
	for _, path := range []string{shell, inherited} {
		for _, dir := range strings.Split(path, ":") {
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return strings.Join(out, ":")
}

// withPath rewrites env's PATH to what the interactive shell sets up.
func withPath(shellPath string, env []string) []string {
	if shellPath == "" {
		return env
	}
	for i, kv := range env {
		if key, value, ok := strings.Cut(kv, "="); ok && key == "PATH" {
			env[i] = "PATH=" + mergePaths(shellPath, value)
			return env
		}
	}
	return append(env, "PATH="+mergePaths(shellPath, ""))
}

func withInteractivePath(env []string) []string {
	return withPath(interactivePath(), env)
}
