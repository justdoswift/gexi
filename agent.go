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

// Agent tracking mirrors the Swift build: notice an agent running in the
// pane's foreground process group, and turn it into a command that resumes
// the same conversation next launch.

type agentKind string

const (
	agentClaude agentKind = "claude"
	agentCodex  agentKind = "codex"
)

type agentDetection struct {
	Kind      agentKind
	SessionID string
	Resume    []string
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
	}
	return resolvedCommand{command, command}
}

func resolveClaude(raw string, command []string) resolvedCommand {
	if strings.Contains(raw, "--resume") || strings.Contains(raw, "--continue") || strings.Contains(raw, " -c ") {
		return resolvedCommand{command, command}
	}
	id := newID()
	launch := raw + " --session-id " + id
	restore := strings.Replace(launch, "--session-id "+id, "--resume "+id, 1)
	return resolvedCommand{replaceRaw(launch, command), replaceRaw(restore, command)}
}

func resolveCodex(raw string, command []string) resolvedCommand {
	if strings.Contains(raw, " resume") || strings.HasSuffix(strings.TrimSpace(raw), "resume") {
		return resolvedCommand{command, command}
	}
	return resolvedCommand{command, replaceRaw("codex resume --last", command)}
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
		}
	}
	return nil
}

func kindOf(proc processSnapshot) agentKind {
	name := strings.ToLower(filepath.Base(proc.path))
	if strings.HasPrefix(name, "claude") {
		return agentClaude
	}
	if strings.HasPrefix(name, "codex") {
		return agentCodex
	}
	if len(proc.args) > 0 {
		name = strings.ToLower(filepath.Base(proc.args[0]))
		if strings.HasPrefix(name, "claude") {
			return agentClaude
		}
		if strings.HasPrefix(name, "codex") {
			return agentCodex
		}
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
	slug := strings.NewReplacer("/", "-", ".", "-").Replace(cwd)
	dir := filepath.Join(root, slug)

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

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
