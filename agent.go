package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Agent tracking notices an agent running in the pane's foreground process
// group and turns it into a command that resumes the same conversation next
// launch.

type agentKind string

const (
	agentClaude agentKind = "claude"
	agentCodex  agentKind = "codex"
	agentOmp    agentKind = "omp"
	agentDevin  agentKind = "devin"
	agentGrok   agentKind = "grok"
	agentDroid  agentKind = "droid"
	agentPerch  agentKind = "perch"
	agentPi     agentKind = "pi"
	agentAmp    agentKind = "amp"
)

type agentDetection struct {
	Kind      agentKind
	SessionID string
	Resume    []string
}

// name is the agent's product name. Chrome shows it instead of the terminal
// title, which carries the agent's spinner glyph.
func (k agentKind) name() string {
	switch k {
	case agentClaude:
		return "Claude Code"
	case agentCodex:
		return "Codex"
	case agentOmp:
		return "omp"
	case agentDevin:
		return "Devin"
	case agentGrok:
		return "Grok"
	case agentDroid:
		return "Droid"
	case agentPerch:
		return "Perch"
	case agentPi:
		return "Pi"
	case agentAmp:
		return "Amp"
	}
	return string(k)
}

// launch is the command that opens a fresh conversation with the agent, for
// a pane that moves into a worktree while the agent runs. Its flags, such as
// a model or permission mode, are not carried: only the kind is known here.
func (k agentKind) launch() []string {
	switch k {
	case agentPi:
		return shellCommand(piLaunchCommand())
	case agentClaude, agentCodex, agentOmp, agentDevin, agentGrok, agentDroid, agentPerch, agentAmp:
		return shellCommand(string(k))
	}
	return nil
}

// resume is the command that brings back a session an event reported. Amp
// names a thread by id, so the pane can return to the exact conversation it
// had even when several panes share one checkout.
func (k agentKind) resume(session string) []string {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil
	}
	switch k {
	case agentAmp:
		return shellCommand("amp threads continue " + shellQuote(session))
	}
	return nil
}

type resolvedCommand struct {
	launch  []string
	restore []string
}

// resolveAgentCommand injects a session id for Claude Code at launch, so the
// pane can restore with --resume later.
func resolveAgentCommand(command []string) resolvedCommand {
	raw, ok := rawCommand(command)
	if !ok {
		return resolvedCommand{command, command}
	}
	switch filepath.Base(firstToken(raw)) {
	case "claude":
		return resolveClaude(raw, command)
	case "codex":
		return resolveCodex(raw, command)
	case "omp", "devin", "grok":
		return resolveContinuing(raw, command, "--continue", "--continue", "-c")
	case "pi":
		return resolvePi(raw, command)
	case "amp":
		return resolveAmp(raw, command)
	case "droid":
		return resolveContinuing(raw, command, "--resume --last", "--resume", "-r", "resume")
	}
	return resolvedCommand{command, command}
}

// withShellFallback keeps a pane alive after the agent it runs exits: the raw
// command ends by replacing itself with an interactive login shell, so the
// pane comes back to a prompt instead of closing with the agent. A pane is a
// shell first; the agent is only what runs on top of it.
func withShellFallback(command []string) []string {
	raw, ok := rawCommand(command)
	if !ok || len(command) < 3 || strings.TrimSpace(raw) == "" {
		return command
	}
	return replaceRaw(raw+" ; exec "+shellQuote(loginShell())+" -l", command)
}

func resolveClaude(raw string, command []string) resolvedCommand {
	// The user's own settings carry the hooks after installGlobalClaudeHooks,
	// so every claude in a pane reports events — one gexi launched and one the
	// user typed alike. Only when that file could not be written does a
	// launch fall back to a private settings file.
	settings := ""
	if !globalHooksInstalled && !strings.Contains(raw, "--settings") {
		if hooks, err := installClaudeHooks(); err == nil {
			settings = " --settings " + shellQuote(hooks)
		}
	}
	if strings.Contains(raw, "--resume") || strings.Contains(raw, "--continue") || strings.Contains(raw, " -c ") {
		if settings == "" {
			return resolvedCommand{command, command}
		}
		launch := raw + settings
		return resolvedCommand{replaceRaw(launch, command), replaceRaw(launch, command)}
	}
	id := newID()
	launch := raw + " --session-id " + id + settings
	restore := strings.Replace(launch, "--session-id "+id, "--resume "+id, 1)
	return resolvedCommand{replaceRaw(launch, command), replaceRaw(restore, command)}
}

func resolveCodex(raw string, command []string) resolvedCommand {
	if strings.Contains(raw, " resume") || strings.HasSuffix(strings.TrimSpace(raw), "resume") {
		return resolvedCommand{command, command}
	}
	return resolvedCommand{command, replaceRaw("codex resume --last", command)}
}

// resolvePi starts Pi with gexi's event extension, and restores it with
// --continue on top of the same flags. The extension is additive (-e can be
// repeated), so the user's own extensions stay loaded.
func resolvePi(raw string, command []string) resolvedCommand {
	launch := raw
	if path, err := installPiEvents(); err == nil {
		launch = strings.TrimSpace(raw + " -e " + shellQuote(path))
	}
	restore := launch
	already := false
	for _, token := range strings.Fields(raw) {
		if token == "--continue" || token == "-c" {
			already = true
			break
		}
	}
	if !already {
		restore = strings.TrimSpace(launch + " --continue")
	}
	return resolvedCommand{replaceRaw(launch, command), replaceRaw(restore, command)}
}

// resolveAmp starts Amp as the user typed it and restores the newest thread
// in the pane's directory. Amp has no launch flag that names a new thread, so
// the exact id arrives later from gexi's plugin and replaces this fallback.
func resolveAmp(raw string, command []string) resolvedCommand {
	if ampContinues(raw) {
		return resolvedCommand{command, command}
	}
	restore := strings.TrimSpace(raw + " threads continue --last")
	return resolvedCommand{command, replaceRaw(restore, command)}
}

// ampContinues reports whether the command already resumes a thread, either
// as `amp threads continue` or through Amp's `amp last` alias.
func ampContinues(raw string) bool {
	fields := strings.Fields(raw)
	if len(fields) > 1 && fields[1] == "last" {
		return true
	}
	for i, token := range fields {
		switch token {
		case "threads", "thread", "t":
			if i+1 < len(fields) && (fields[i+1] == "continue" || fields[i+1] == "c") {
				return true
			}
		}
	}
	return false
}

// agentFromResume is the conversation a saved launch already names. Amp is
// the only agent whose resume command carries the exact thread, so a restored
// pane can show its mark and keep the thread before the process is detected.
func agentFromResume(command []string) *agentDetection {
	raw, ok := rawCommand(command)
	if !ok {
		return nil
	}
	id := ampThreadID(raw)
	if id == "" {
		return nil
	}
	return &agentDetection{Kind: agentAmp, SessionID: id, Resume: command}
}

// ampThreadID reads the thread from `amp threads continue <id>`. The `--last`
// fallback names no thread and returns "".
func ampThreadID(raw string) string {
	fields := strings.Fields(raw)
	for i := 0; i+2 < len(fields); i++ {
		switch fields[i] {
		case "threads", "thread", "t":
		default:
			continue
		}
		if fields[i+1] != "continue" && fields[i+1] != "c" {
			continue
		}
		id := strings.Trim(fields[i+2], `'"`)
		if strings.HasPrefix(id, "T-") && isUUID(id[2:]) {
			return id
		}
	}
	return ""
}

// resolveContinuing appends the flags that resume the newest session in the
// pane's directory. Flags are added to the command rather than replacing it,
// so model and permission flags survive the restore.
func resolveContinuing(raw string, command []string, add string, already ...string) resolvedCommand {
	for _, token := range strings.Fields(raw) {
		for _, flag := range already {
			if token == flag || strings.HasPrefix(token, flag+"=") {
				return resolvedCommand{command, command}
			}
		}
	}
	restore := strings.TrimSpace(raw + " " + add)
	return resolvedCommand{command, replaceRaw(restore, command)}
}

// paneRunsAgent reports whether the pane's own command is an agent, rather
// than a shell an agent runs inside.
func paneRunsAgent(command []string) bool {
	raw, ok := rawCommand(command)
	if !ok {
		return false
	}
	return agentNamed(filepath.Base(firstToken(raw))) != ""
}

// stopAgentGroup ends the process group an agent runs in, so git can move
// the files it was working on. SIGINT is what Ctrl-C sends; the escalation
// covers a turn that ignores it and an agent that ignores both.
func stopAgentGroup(group int) bool {
	if group <= 0 {
		return true
	}
	for _, sig := range []unix.Signal{unix.SIGINT, unix.SIGINT, unix.SIGHUP, unix.SIGKILL} {
		if err := unix.Kill(-group, sig); err != nil {
			// ESRCH: the group is already gone.
			return true
		}
		if waitGroupGone(group, 700*time.Millisecond) {
			return true
		}
	}
	return !groupAlive(group)
}

func groupAlive(group int) bool {
	return unix.Kill(-group, 0) == nil
}

func waitGroupGone(group int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if !groupAlive(group) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// rawCommand is the raw string of a `shell -lc "..."` wrapper.
func rawCommand(command []string) (string, bool) {
	if len(command) < 3 {
		if len(command) == 1 {
			return command[0], true
		}
		return "", false
	}
	switch filepath.Base(command[0]) {
	case "zsh", "bash", "sh", "fish", "dash":
	default:
		return "", false
	}
	if command[1] != "-lc" && command[1] != "-c" {
		return "", false
	}
	return command[2], true
}

func replaceRaw(raw string, command []string) []string {
	if len(command) < 3 {
		return []string{raw}
	}
	out := append([]string(nil), command...)
	out[2] = raw
	return out
}

func firstToken(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// foregroundGroupOf returns the PTY's foreground process group, 0 if gone.
func foregroundGroupOf(fd uintptr) int {
	n, err := unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	if err != nil {
		return 0
	}
	return n
}

type processSnapshot struct {
	pid  int
	ppid int
	pgid int
	path string
	args []string
}

func snapshotOf(pid int) (processSnapshot, bool) {
	kinfo, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return processSnapshot{}, false
	}
	path, args := procArgs(pid)
	if path == "" && len(args) > 0 {
		path = args[0]
	}
	return processSnapshot{
		pid:  int(kinfo.Proc.P_pid),
		ppid: int(kinfo.Eproc.Ppid),
		pgid: int(kinfo.Eproc.Pgid),
		path: path,
		args: args,
	}, true
}

// snapshotsInGroup lists every process in pgid, for `shell -lc "agent"`,
// where the group leader is the shell and the agent is one level down.
func snapshotsInGroup(pgid int) []processSnapshot {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	var out []processSnapshot
	for _, k := range all {
		if int(k.Eproc.Pgid) != pgid || k.Proc.P_pid <= 0 {
			continue
		}
		if snap, ok := snapshotOf(int(k.Proc.P_pid)); ok {
			out = append(out, snap)
		}
	}
	return out
}

// procArgs parses KERN_PROCARGS2: argc, the executable path, then argv.
func procArgs(pid int) (string, []string) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) <= 4 {
		return "", nil
	}
	argc := int(int32(binary.LittleEndian.Uint32(raw[:4])))
	if argc <= 0 {
		return "", nil
	}
	i := 4
	for i < len(raw) && raw[i] != 0 {
		i++
	}
	path := string(raw[4:i])
	for i < len(raw) && raw[i] == 0 {
		i++
	}
	var args []string
	for n := 0; n < argc && i < len(raw); n++ {
		end := i
		for end < len(raw) && raw[end] != 0 {
			end++
		}
		args = append(args, string(raw[i:end]))
		i = end + 1
	}
	return path, args
}

func detectAgent(pgid int, fallbackCWD string) *agentDetection {
	if pgid <= 0 {
		return nil
	}
	var candidates []processSnapshot
	if leader, ok := snapshotOf(pgid); ok && kindOf(leader) != "" {
		candidates = []processSnapshot{leader}
	} else {
		candidates = snapshotsInGroup(pgid)
	}
	for _, proc := range candidates {
		switch kindOf(proc) {
		case agentClaude:
			id := sessionIDFromArgs(proc.args)
			if id == "" {
				id = newestClaudeSessionID(fallbackCWD)
			}
			if id == "" {
				return nil
			}
			return &agentDetection{Kind: agentClaude, SessionID: id, Resume: shellCommand("claude --resume " + id)}
		case agentCodex:
			return &agentDetection{Kind: agentCodex, Resume: shellCommand("codex resume --last")}
		case agentOmp:
			return &agentDetection{Kind: agentOmp, Resume: shellCommand("omp --continue")}
		case agentDevin:
			return &agentDetection{Kind: agentDevin, Resume: shellCommand("devin --continue")}
		case agentGrok:
			return &agentDetection{Kind: agentGrok, Resume: shellCommand("grok --continue")}
		case agentDroid:
			return &agentDetection{Kind: agentDroid, Resume: shellCommand("droid --resume --last")}
		case agentPerch:
			// Perch keeps one conversation per directory, keyed by the working
			// directory itself, so the bare command resumes it: no session id
			// to carry, and none to go stale.
			return &agentDetection{Kind: agentPerch, Resume: shellCommand("perch")}
		case agentPi:
			// Pi also keys sessions by directory, and --continue picks the
			// newest one there.
			return &agentDetection{Kind: agentPi, Resume: shellCommand(piResumeCommand())}
		case agentAmp:
			// Amp remembers the last thread for a checkout; the plugin's
			// session event replaces this with the exact thread id once the
			// conversation knows it.
			return &agentDetection{Kind: agentAmp, Resume: shellCommand("amp threads continue --last")}
		}
	}
	return nil
}

func kindOf(proc processSnapshot) agentKind {
	if kind := agentNamed(filepath.Base(proc.path)); kind != "" {
		return kind
	}
	// A launcher or symlink may sit in front of the real binary (omp, for
	// one, runs from a versioned Tern executable), so check argv[0] too.
	if len(proc.args) > 0 {
		if kind := agentNamed(filepath.Base(proc.args[0])); kind != "" {
			return kind
		}
	}
	// Perch is a Node program: the process is `node .../bin/perch`, so the
	// binary and argv[0] are both node. The script it runs is the only place
	// its name appears.
	if perchScript(proc.args) != "" {
		return agentPerch
	}
	// Pi is a Node program too, running the package's bundled cli.js through
	// the bin/pi shim.
	if piScript(proc.args) != "" {
		return agentPi
	}
	return ""
}

// perchScript returns the Perch script among a node process's arguments, or
// "" when the process is not Perch. The npm shim is `bin/perch`, and the
// package it belongs to is perchai-cli; nothing else is taken for Perch, so a
// node program that merely mentions the word does not match.
func perchScript(args []string) string {
	for i, arg := range args {
		if i > 3 {
			break
		}
		base := filepath.Base(arg)
		if base != "perch" && base != "perch.mjs" {
			continue
		}
		if strings.Contains(arg, "perchai-cli") || strings.HasSuffix(filepath.Dir(arg), "bin") {
			return arg
		}
	}
	return ""
}

// piScript returns the Pi script among a node process's arguments, or ""
// when the process is not Pi. The managed install execs the package's
// bin/pi shim, a symlink to the bundled cli.js; a node program that merely
// has a file named pi does not match.
func piScript(args []string) string {
	for i, arg := range args {
		if i > 3 {
			break
		}
		base := filepath.Base(arg)
		if strings.Contains(arg, "pi-coding-agent") && (base == "cli.js" || base == "pi") {
			return arg
		}
		if base != "pi" && base != "pi.mjs" {
			continue
		}
		dir := filepath.Dir(arg)
		if filepath.Base(dir) == ".bin" || strings.HasSuffix(dir, "/bin") {
			return arg
		}
	}
	return ""
}

func agentNamed(name string) agentKind {
	name = strings.ToLower(name)
	switch {
	case strings.HasPrefix(name, "claude"):
		return agentClaude
	case strings.HasPrefix(name, "codex"):
		return agentCodex
	case name == "omp":
		return agentOmp
	case name == "devin":
		return agentDevin
	case name == "grok":
		return agentGrok
	case name == "droid":
		return agentDroid
	case name == "perch":
		return agentPerch
	case name == "pi":
		return agentPi
	case name == "amp":
		return agentAmp
	}
	return ""
}

func sessionIDFromArgs(args []string) string {
	for i, arg := range args {
		for _, flag := range []string{"--resume", "--session-id"} {
			if strings.HasPrefix(arg, flag+"=") {
				value := strings.TrimPrefix(arg, flag+"=")
				if isUUID(value) {
					return value
				}
			}
			if arg == flag && i+1 < len(args) && isUUID(args[i+1]) {
				return args[i+1]
			}
		}
	}
	return ""
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
				return false
			}
		}
	}
	return true
}

// newestClaudeSessionID finds the transcript Claude Code is writing for cwd.
func newestClaudeSessionID(cwd string) string {
	root := filepath.Join(homeDir(), ".claude", "projects")
	if override := os.Getenv("CLAUDE_CONFIG_DIR"); override != "" {
		root = filepath.Join(override, "projects")
	}
	dir := filepath.Join(root, claudeProjectSlug(cwd))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type candidate struct {
		id  string
		mod time.Time
	}
	var list []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if isUUID(id) {
			list = append(list, candidate{id, info.ModTime()})
		}
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	return list[0].id
}

// claudeProjectSlug is the folder Claude Code files a directory's transcripts
// under: every character that is not a letter, a digit, or a hyphen becomes a
// hyphen. A path with a space is the one that matters here, since gexi keeps
// its own worktrees under "Application Support", and a missing folder there
// left the agent undetected, showing a terminal glyph beside the glyph the
// title already carried.
func claudeProjectSlug(dir string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '-'
		}
	}, dir)
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
