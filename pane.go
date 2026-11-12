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
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/egoist/mygo/plugins/terminal"
)

// paneOutputLog is how much of a pane's recent output a remote mirror can
// ask for by offset: past it, the client is sent a fresh snapshot instead.
const paneOutputLog = 4 << 20

// ptyConn is the in-memory backend: gexi owns the PTY, so the terminal
// reads every byte the shell writes. That is also where agent detection gets
// its master fd.
type ptyConn struct {
	mu      sync.Mutex
	f       *os.File
	ready   chan struct{}
	pending *pty.Winsize

	// out is the tail of what the program wrote, which a remote mirror of
	// the pane replays from a snapshot: chunks from outStart, the offset of
	// the oldest byte kept, through outEnd. cutMu holds the log still while
	// a snapshot is taken, so the snapshot and outEnd name the same state;
	// busy says the last chunk read may not have reached the terminal yet.
	cutMu    sync.Mutex
	out      [][]byte
	outBytes int
	outStart int64
	outEnd   int64
	busy     bool

	// waiters are the mirrors parked on this pane's output. A write closes
	// and forgets them all, which is what wakes a long poll the moment the
	// program has something to say.
	waitMu  sync.Mutex
	waiters []chan struct{}
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
	// The terminal applied the chunk read last before it reads again, so
	// the log now matches what it shows.
	c.cutMu.Lock()
	c.busy = false
	c.cutMu.Unlock()
	n, err := c.f.Read(p)
	if n > 0 {
		c.cutMu.Lock()
		c.log(p[:n])
		c.busy = true
		c.cutMu.Unlock()
	}
	return n, err
}

// log keeps a chunk for the mirrors, letting go of the oldest chunks past
// paneOutputLog. cutMu is held.
func (c *ptyConn) log(b []byte) {
	c.out = append(c.out, append([]byte(nil), b...))
	c.outBytes += len(b)
	c.outEnd += int64(len(b))
	c.wake()
	for c.outBytes > paneOutputLog && len(c.out) > 1 {
		c.outBytes -= len(c.out[0])
		c.outStart += int64(len(c.out[0]))
		c.out = c.out[1:]
	}
}

// snapshot returns what the pane shows as escape sequences and the log
// offset they stand for, so a mirror can take the output after it without
// doubling or losing any. The cut waits for the terminal to have applied
// every chunk logged, then holds the log while it reads the screen.
func (c *ptyConn) snapshot(term *terminal.Terminal) (int64, []byte) {
	for try := 0; ; try++ {
		if try > 0 {
			time.Sleep(time.Millisecond)
		}
		c.cutMu.Lock()
		if c.busy && try < 50 {
			c.cutMu.Unlock()
			continue
		}
		off := c.outEnd
		snap := term.Snapshot()
		c.cutMu.Unlock()
		return off, snap
	}
}

// waitOutput parks a mirror until the program writes. The returned cancel
// drops the registration, for a poll that leaves without being woken.
func (c *ptyConn) waitOutput() (<-chan struct{}, func()) {
	c.waitMu.Lock()
	ch := make(chan struct{})
	c.waiters = append(c.waiters, ch)
	c.waitMu.Unlock()
	return ch, func() {
		c.waitMu.Lock()
		for i, w := range c.waiters {
			if w == ch {
				c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
				break
			}
		}
		c.waitMu.Unlock()
	}
}

// wake releases every parked mirror. A channel closed here is closed once:
// the waiters list is emptied as it is released.
func (c *ptyConn) wake() {
	c.waitMu.Lock()
	for _, ch := range c.waiters {
		close(ch)
	}
	c.waiters = nil
	c.waitMu.Unlock()
}

// outputTotal is how many bytes the program has written so far: unchanged
// between two looks means it has gone quiet.
func (c *ptyConn) outputTotal() int64 {
	c.cutMu.Lock()
	defer c.cutMu.Unlock()
	return c.outEnd
}

// outputSince returns the output logged from offset on, and false when the
// log no longer holds it.
func (c *ptyConn) outputSince(offset int64) ([]byte, bool) {
	c.cutMu.Lock()
	defer c.cutMu.Unlock()
	if offset < c.outStart || offset > c.outEnd {
		return nil, false
	}
	skip := offset - c.outStart
	var out []byte
	for _, chunk := range c.out {
		if skip >= int64(len(chunk)) {
			skip -= int64(len(chunk))
			continue
		}
		out = append(out, chunk[skip:]...)
		skip = 0
	}
	return out, true
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
	// Label is the name the user gave the pane, or the one gexi made from
	// its first message. It outranks the agent's name and the terminal
	// title, so neither can overwrite a name the user set.
	Label string
	// firstPrompt is the first message the user sent to the agent here,
	// kept so a name can be made from what the work is about.
	firstPrompt string
	// nameAsked says the pane's agent was already asked to name it, or the
	// user named it by hand; the first message does not ask twice.
	nameAsked bool
	// nameKind is the agent that can name the pane, kept after the agent
	// exits so its first message can still be turned into a name.
	nameKind agentKind
	// titleSeen is the title last looked at, and titleSeenAt when it last
	// changed: a spinner mark that stops moving is not work.
	titleSeen   string
	titleSeenAt time.Time
	// outSeen is how much output the pane had the last time it was looked
	// at, and outAt when that count last moved: an agent marked at work
	// that neither writes nor animates is done, whatever its hook said.
	outSeen int64
	outAt   time.Time
	// subagents are the Task/Agent calls the agent reports running in this
	// pane, keyed by the id a start and its stop share. A background one
	// outlives its turn, which is what keeps the session visibly busy after
	// the prompt has returned.
	subagents map[string]paneSubagent
	// stageMirror is the live preview a stage rail card shows while this
	// pane is off stage: a sink terminal fed by a Mirror of this pane's.
	stageMirror *stageMirror
	// viewW and viewH are the terminal view's last layout size, which the
	// mirror is laid out at before it is scaled into its card.
	viewW, viewH float32
	// cellW and cellH are a cell's size in DIPs, measured whenever the view
	// is laid out at the card's size.
	cellW, cellH float32
	// stageEnter asks the next frame to bring this pane onto the stage with
	// a grow and fade, as a card comes forward.
	stageEnter bool
	// stageCardShown is false until a rail card has drawn once, so a card
	// that has just joined the rail animates in.
	stageCardShown bool
	// stageCardShrink asks the card that has just left the stage to start
	// large and shrink into the rail.
	stageCardShrink bool
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
	// haloStart is when the completion sweep over the terminal began; zero
	// when none runs.
	haloStart time.Time
	// beamStart is when the pane's work began, so the light on its bottom
	// edge fades in rather than appearing at full strength; zero while the
	// pane is at rest.
	beamStart time.Time
	// headerActionsW is the width the pane's action buttons took in the
	// frame before, so a header can set its path and branch pill beside the
	// room they leave rather than by fixed thresholds.
	headerActionsW float32
	ws             *workspace
	// bgProject is the project whose session held this pane when it went to
	// the background, so reattaching lands it in the same place.
	bgProject *project
	// remote is set on a pane the window shows from another Mac: its
	// terminal is fed by that host's stream, and its actions go back over
	// the link.
	remote *remotePaneLink
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
	// A shell reports its own path through OSC 7, and that path may not exist
	// here. The pane still has to start in a folder that does.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir, _ = os.UserHomeDir()
	}
	resolved := resolveAgentCommand(command)
	// A pane whose own program is an agent keeps a shell behind it: when the
	// agent exits, the shell takes the pane over instead of the pane closing
	// with the process. p.command stays the agent's, so agent detection,
	// worktree moves, and the control API still see an agent pane.
	launch := resolved.launch
	if paneRunsAgent(resolved.launch) {
		launch = withShellFallback(resolved.launch)
	}

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
	// Amp's saved command names the exact thread. Knowing its mark now keeps
	// a session-less detection from replacing the thread with --last.
	if det := agentFromResume(resolved.restore); det != nil {
		p.agent = det
		p.sawAgent = true
	}

	lightTheme, darkTheme := ws.terminalThemes()
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
		Font:        ws.terminalTextFont(),
		// The user's themes, one for each appearance: the view takes the
		// dark one in a dark window, so switching appearance keeps it.
		Theme:     lightTheme,
		DarkTheme: darkTheme,
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

	if err := p.start(launch, dir); err != nil {
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
		if strings.Contains(arg, "--resume") || strings.Contains(arg, "--continue") ||
			strings.Contains(arg, " resume") || strings.Contains(arg, "threads continue") {
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

// titleSpinners are the marks an agent puts in front of the terminal title:
// Claude Code cycles through the quarter circles while it works and rests on
// a star between turns; Codex, Amp and OpenCode use stars and braille
// frames. Chrome already says an agent is working with the session label's
// shimmer, so the mark is noise beside the agent's logo and is dropped.
const titleSpinners = "◐◑◒◓✳✻✶✢✦✧✱✲✽·"

// isTitleSpinner reports whether r is one of those marks: a listed glyph, or
// a braille pattern, which Amp and Codex spin through.
func isTitleSpinner(r rune) bool {
	return strings.ContainsRune(titleSpinners, r) || (r >= 0x2800 && r <= 0x28FF)
}

// titleShowsWork reports whether a terminal title opens with the animated
// part of an agent's spinner. A star is where Claude Code and Amp rest
// between turns, so it is not work; the frames they cycle through are.
// Hooks are the primary source of the sidebar state, but a pane opened
// mid-turn can miss agent.start; its title still moves.
func titleShowsWork(title string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(t)
	return strings.ContainsRune("◐◑◒◓", r) || (r >= 0x2800 && r <= 0x28FF)
}

// titleShowsWork is the pane's current title, when it has a terminal.
func (p *pane) titleShowsWork() bool {
	return p != nil && p.term != nil && titleShowsWork(p.term.Title())
}

// titleWorkWindow is how long a mark in the title counts as work without
// moving. A working agent's mark changes every few hundred milliseconds, so
// one that has stood still this long is the last frame it left behind.
const titleWorkWindow = 2 * time.Second

// titleWorking reports whether the pane's title says the agent is at work.
// The mark has to keep changing: an agent that stops can leave its last
// spinner frame in the title, and that frame is not work.
func (p *pane) titleWorking() bool {
	if p == nil || p.term == nil {
		return false
	}
	return p.titleWorkingFor(p.term.Title())
}

func (p *pane) titleWorkingFor(title string) bool {
	if !titleShowsWork(title) {
		p.titleSeen, p.titleSeenAt = title, time.Time{}
		return false
	}
	if title != p.titleSeen {
		p.titleSeen, p.titleSeenAt = title, time.Now()
		return true
	}
	return time.Since(p.titleSeenAt) < titleWorkWindow
}

// cleanTitle trims a terminal title and drops the agent's leading activity
// marks, leaving the words themselves.
func cleanTitle(title string) string {
	t := strings.TrimSpace(title)
	for t != "" {
		r, size := utf8.DecodeRuneInString(t)
		if !isTitleSpinner(r) {
			break
		}
		t = strings.TrimSpace(t[size:])
		// An emoji presentation selector belongs to the mark it followed.
		t = strings.TrimSpace(strings.TrimPrefix(t, "\uFE0F"))
	}
	return t
}

func (p *pane) title() string {
	// A name set by hand, or made from the first message, is the pane's own
	// and outranks whatever the agent or the terminal calls itself.
	if p.Label != "" {
		return p.Label
	}
	// A pane on another Mac is called what its host calls it, with the
	// host's activity mark dropped as a local title's is.
	if p.remote != nil && p.remote.title != "" {
		return cleanTitle(p.remote.title)
	}
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
	if p.term != nil {
		if d := p.term.Dir(); d != "" {
			return d
		}
	}
	if p.initialDir != "" {
		return p.initialDir
	}
	home, _ := os.UserHomeDir()
	return home
}

// noteFirstPrompt keeps the first message the user sent to the agent, so a
// name can be made from what the work is about. It reports whether this was
// that first message.
func (p *pane) noteFirstPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || p.firstPrompt != "" {
		return false
	}
	p.firstPrompt = prompt
	return true
}

// applyPerchTurn records whether a Perch turn is running in this pane. Perch
// has no hooks, so this is the only source of its working state; the prompt
// is kept so the sidebar can say what it is working on. It reports whether
// anything changed.
func (p *pane) applyPerchTurn(busy bool, prompt string) bool {
	if busy && p.noteFirstPrompt(prompt) && p.ws != nil {
		if p.nameKind == "" {
			p.nameKind = agentPerch
		}
		p.ws.namePaneWithAgent(p, agentPerch)
	}
	state := activityIdle
	if busy {
		state = activityWorking
	}
	if p.activity != nil && p.activity.State == state && p.activity.Agent == agentPerch.name() {
		return false
	}
	p.activity = &agentActivity{
		State: state,
		Agent: agentPerch.name(),
		Kind:  "turn.start",
		Since: time.Now().Format(time.RFC3339),
	}
	if !busy {
		p.activity.Kind = "turn.end"
	}
	return true
}

// applyAgent records a detection on the main thread and reports whether the
// restore command changed.
func (p *pane) applyAgent(det *agentDetection) bool {
	if det != nil {
		p.sawAgent = true
		if det.SessionID == "" {
			// A detection without a thread id must not throw away the exact
			// one the plugin reported, or the one the restored command
			// already named.
			if p.agent != nil && p.agent.Kind == det.Kind && p.agent.SessionID != "" {
				return false
			}
			if exact := agentFromResume(p.lastAgentRestore); exact != nil && exact.Kind == det.Kind {
				p.agent = exact
				p.restoreCommand = exact.Resume
				return false
			}
		}
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

// adoptSession records the conversation an agent says it is in. Amp reports
// its thread id through gexi's plugin, so the pane can come back to that
// exact thread rather than the newest one in the directory.
func (p *pane) adoptSession(kind agentKind, session string) {
	resume := kind.resume(session)
	if len(resume) == 0 {
		return
	}
	p.sawAgent = true
	p.agent = &agentDetection{Kind: kind, SessionID: session, Resume: resume}
	p.restoreCommand = resume
	p.lastAgentRestore = resume
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
	if p.ws != nil {
		p.ws.stopStageMirror(p)
	}
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
