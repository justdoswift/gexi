// Command gexi drives a running Gexi window over its Unix socket:
// one JSON request per connection, one JSON response back.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type envelope struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

type client struct{ socket string }

func (c client) call(method string, params map[string]any) (any, error) {
	if params == nil {
		params = map[string]any{}
	}
	conn, err := net.DialTimeout("unix", c.socket, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", c.socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := map[string]any{"id": newID(), "method": method, "params": params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	var resp envelope
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "request failed"
		}
		return nil, errors.New(resp.Error)
	}
	if len(resp.Result) == 0 {
		return map[string]any{}, nil
	}
	var result any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// MARK: arguments

type options struct {
	json       bool
	pane       string
	agent      string
	socket     string
	vertical   bool
	horizontal bool
	enter      bool
	scrollback bool
	lines      int
	cwd        string
}

var opt options
var positional []string

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gexi: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Print(`gexi — control a running Gexi workspace

Usage:
  gexi ping
  gexi info [--json]
  gexi list [--json]
  gexi capture [-p PANE] [--scrollback] [--lines N] [--json]
  gexi send   [-p PANE] [-e|--enter] <text...>
  gexi split  [-p PANE] [--vertical|--horizontal] [--cwd DIR] [-- COMMAND...]
  gexi run    [-p PANE] [--vertical|--horizontal] [--cwd DIR] COMMAND...
  gexi focus  [-p PANE]
  gexi close  [-p PANE]
  gexi new-session [--cwd DIR] [-- COMMAND...]
  gexi session-select SESSION
  gexi new-project [--cwd DIR]
  gexi settings
  gexi event  KIND [--agent NAME] [--json]
  gexi plan   [--agent NAME]      (reads a hook payload on stdin)
  gexi quit

Options:
  -p, --pane PANE     Pane id or prefix (default: $GEXI_PANE or the focused pane)
  --socket PATH       Control socket (default: $GEXI_HOME/control.sock)
  --json              Print the raw JSON result
  -e, --enter         Append a carriage return after send text
  --scrollback        Capture scrollback history as well
  --lines N           With --scrollback, how many lines to read
  --cwd DIR           Working directory for a new tab, split, or run
  --agent NAME        Agent name attached to an event (claude, codex, …)
  -h, --help          Show this help
`)
	os.Exit(0)
}

func parseArgs() {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json":
			opt.json = true
		case "-p", "--pane":
			i++
			if i >= len(args) {
				fail("--pane needs a value")
			}
			opt.pane = args[i]
		case "--socket":
			i++
			if i >= len(args) {
				fail("--socket needs a value")
			}
			opt.socket = args[i]
		case "--agent":
			i++
			if i >= len(args) {
				fail("--agent needs a name")
			}
			opt.agent = args[i]
		case "--vertical":
			opt.vertical = true
		case "--horizontal":
			opt.horizontal = true
		case "-e", "--enter":
			opt.enter = true
		case "--scrollback":
			opt.scrollback = true
		case "--lines":
			i++
			if i >= len(args) {
				fail("--lines needs a number")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fail("--lines needs a number")
			}
			opt.lines = n
		case "--cwd":
			i++
			if i >= len(args) {
				fail("--cwd needs a path")
			}
			opt.cwd = args[i]
		case "-h", "--help":
			usage()
		case "--":
			positional = append(positional, args[i+1:]...)
			return
		default:
			positional = append(positional, a)
		}
	}
}

// readStdinJSON reads the hook payload when a hook pipes one in. A person
// running `gexi event` from a terminal has no pipe, and must not block.
func readStdinJSON() map[string]any {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	// A hook closes stdin right after its payload. A pipe that stays open
	// (a shell pipeline, a wrapper script) must not hang the command, so
	// wait only briefly for the payload to arrive.
	ch := make(chan []byte, 1)
	go func() {
		raw, _ := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
		ch <- raw
	}()
	var raw []byte
	select {
	case raw = <-ch:
	case <-time.After(250 * time.Millisecond):
		return nil
	}
	var detail map[string]any
	if json.Unmarshal(raw, &detail) != nil {
		return nil
	}
	return detail
}

// sendQuiet is the hook path: one request, one response, and no complaint
// when the window is closed.
func sendQuiet(socket, pane, method string, params map[string]any) {
	if params == nil {
		params = map[string]any{}
	}
	if pane != "" && params["pane"] == nil {
		params["pane"] = pane
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	req := map[string]any{"id": newID(), "method": method, "params": params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return
	}
	_, _ = bufio.NewReader(conn).ReadBytes('\n')
}

func supportDir() string {
	if v := os.Getenv("GEXI_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Library", "Application Support", "Gexi")
}

func main() {
	parseArgs()
	if len(positional) == 0 {
		usage()
	}
	if opt.socket == "" {
		opt.socket = filepath.Join(supportDir(), "control.sock")
	}

	pane := opt.pane
	if pane == "" {
		pane = os.Getenv("GEXI_PANE")
	}
	call := func(method string, params map[string]any) any {
		if params == nil {
			params = map[string]any{}
		}
		if pane != "" && params["pane"] == nil {
			params["pane"] = pane
		}
		result, err := client{opt.socket}.call(method, params)
		if err != nil {
			fail("%v", err)
		}
		return result
	}
	emit := func(result any) {
		if opt.json {
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(out))
		}
	}
	obj := func(result any) map[string]any {
		m, _ := result.(map[string]any)
		return m
	}
	str := func(m map[string]any, key string) string {
		s, _ := m[key].(string)
		return s
	}

	command := positional[0]
	rest := positional[1:]

	if command == "plan" {
		detail := readStdinJSON()
		if detail == nil {
			// A hook with nothing to say must not fail the agent.
			return
		}
		params := map[string]any{"detail": detail}
		if opt.agent != "" {
			params["agent"] = opt.agent
		}
		sendQuiet(opt.socket, pane, "plan.update", params)
		return
	}

	if command == "event" {
		if len(rest) == 0 {
			fail("event needs a kind: session.start, turn.start, turn.end, needs-input, session.end")
		}
		params := map[string]any{"kind": rest[0]}
		if opt.agent != "" {
			params["agent"] = opt.agent
		}
		if detail := readStdinJSON(); detail != nil {
			params["detail"] = detail
			if sid, ok := detail["session_id"].(string); ok && sid != "" {
				params["session"] = sid
			}
		}
		result := call("event", params)
		if opt.json {
			emit(result)
			return
		}
		fmt.Printf("event %s -> %s\n", rest[0], str(obj(result), "state"))
		return
	}

	switch command {
	case "ping":
		result := call("ping", nil)
		if opt.json {
			emit(result)
			return
		}
		m := obj(result)
		pid := ""
		if f, ok := m["pid"].(float64); ok {
			pid = strconv.Itoa(int(f))
		}
		fmt.Printf("Gexi %s pid %s\n", str(m, "version"), pid)

	case "info":
		result := call("workspace.info", nil)
		if opt.json {
			emit(result)
			return
		}
		m := obj(result)
		fmt.Printf("instance %s\n", str(m, "instance"))
		fmt.Printf("version  %s\n", str(m, "version"))
		fmt.Printf("socket   %s\n", str(m, "socket"))
		focused := str(m, "focusedPane")
		if focused == "" {
			focused = "-"
		}
		fmt.Printf("focused  %s\n", focused)
		projects, _ := m["projects"].([]any)
		fmt.Printf("projects %d\n", len(projects))
		for _, raw := range projects {
			pr, _ := raw.(map[string]any)
			sessions, _ := pr["sessions"].([]any)
			fmt.Printf("  %s  %s  sessions=%d\n", str(pr, "name"), str(pr, "dir"), len(sessions))
			for _, rawSession := range sessions {
				s, _ := rawSession.(map[string]any)
				mark := " "
				if sel, _ := s["selected"].(bool); sel {
					mark = "*"
				}
				panes, _ := s["panes"].([]any)
				id := str(s, "id")
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("    %s %s %s panes=%d\n", mark, str(s, "title"), id, len(panes))
			}
		}

	case "list":
		result := call("pane.list", nil)
		if opt.json {
			emit(result)
			return
		}
		panes, _ := result.([]any)
		for _, raw := range panes {
			p, _ := raw.(map[string]any)
			id := str(p, "id")
			if len(id) > 8 {
				id = id[:8]
			}
			mark := " "
			if focused, _ := p["focused"].(bool); focused {
				mark = "*"
			}
			fmt.Printf("%s  %s %s  %s\n", id, mark, str(p, "title"), str(p, "cwd"))
		}

	case "capture":
		params := map[string]any{}
		if opt.scrollback {
			params["scrollback"] = true
		}
		if opt.lines > 0 {
			params["lines"] = opt.lines
		}
		result := call("pane.text", params)
		if opt.json {
			emit(result)
			return
		}
		fmt.Print(str(obj(result), "text"))

	case "send":
		text := strings.Join(rest, " ")
		if len(rest) == 0 {
			if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
				data, _ := io.ReadAll(os.Stdin)
				text = strings.TrimSuffix(string(data), "\n")
			}
		}
		if text == "" {
			fail("send needs text")
		}
		if opt.enter {
			text += "\r"
		}
		call("pane.send", map[string]any{"text": text})
		if opt.json {
			emit(map[string]any{"ok": true})
		}

	case "split", "run":
		isRun := command == "run"
		axis := "horizontal"
		if opt.vertical {
			axis = "vertical"
		}
		params := map[string]any{"axis": axis}
		command := strings.Join(rest, " ")
		if command != "" {
			params["command"] = command
		}
		if opt.cwd != "" {
			params["cwd"] = opt.cwd
		}
		if isRun && command == "" {
			fail("run needs a command")
		}
		result := call("pane.split", params)
		if opt.json {
			emit(result)
			return
		}
		fmt.Println(str(obj(result), "pane"))

	case "focus":
		result := call("pane.focus", nil)
		emit(result)

	case "close":
		result := call("pane.close", nil)
		emit(result)

	case "new-session":
		params := map[string]any{}
		if command := strings.Join(rest, " "); command != "" {
			params["command"] = command
		}
		if opt.cwd != "" {
			params["cwd"] = opt.cwd
		}
		result := call("session.new", params)
		if opt.json {
			emit(result)
			return
		}
		fmt.Println(str(obj(result), "pane"))

	case "session-select":
		if len(rest) == 0 {
			fail("session-select needs a session id")
		}
		result := call("session.select", map[string]any{"session": rest[0]})
		if opt.json {
			emit(result)
			return
		}
		fmt.Println(str(obj(result), "session"))

	case "new-project":
		params := map[string]any{}
		if opt.cwd != "" {
			params["cwd"] = opt.cwd
		}
		result := call("project.new", params)
		if opt.json {
			emit(result)
			return
		}
		fmt.Println(str(obj(result), "session"))

	case "settings":
		call("app.settings", nil)

	case "quit":
		call("app.quit", nil)

	default:
		fail("Unknown command: %s", command)
	}
}
