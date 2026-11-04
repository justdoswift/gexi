package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// The wire format is JustCli's: one JSON request line per connection, one
// JSON response line back, then the connection closes.

type controlRequest struct {
	ID     string                     `json:"id"`
	Method string                     `json:"method"`
	Params map[string]json.RawMessage `json:"params"`
}

type controlResponse struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}

func (w *workspace) handle(req controlRequest) controlResponse {
	resp := controlResponse{ID: req.ID}
	var (
		result any
		err    error
	)
	w.onMain(func() { result, err = w.dispatch(req) })
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	resp.OK = true
	resp.Result = result
	return resp
}

func (w *workspace) dispatch(req controlRequest) (any, error) {
	p := req.Params

	switch req.Method {
	case "ping":
		return map[string]any{
			"version": Version,
			"pid":     os.Getpid(),
			"socket":  socketPath(),
		}, nil

	case "workspace.info":
		return map[string]any{
			"session":     w.SessionID,
			"socket":      socketPath(),
			"version":     Version,
			"selectedTab": nullable(w.SelectedTab),
			"focusedPane": nullable(w.FocusedPane),
			"tabs":        w.tabsJSON(),
			"paneStyle":   w.settings.PaneStyle,
		}, nil

	case "tab.list":
		return w.tabsJSON(), nil

	case "tab.select":
		raw := str(p, "tab")
		if raw == "" {
			return nil, fmt.Errorf("tab.select needs a tab id")
		}
		raw = strings.ToLower(raw)
		var matches []*tab
		for _, t := range w.Tabs {
			if strings.ToLower(t.ID) == raw {
				w.selectTab(t.ID)
				return map[string]any{"tab": t.ID}, nil
			}
			if strings.HasPrefix(strings.ToLower(t.ID), raw) {
				matches = append(matches, t)
			}
		}
		if len(matches) != 1 {
			if len(matches) == 0 {
				return nil, fmt.Errorf("No such tab: %s", raw)
			}
			return nil, fmt.Errorf("Ambiguous tab id: %s", raw)
		}
		w.selectTab(matches[0].ID)
		return map[string]any{"tab": matches[0].ID}, nil

	case "tab.new":
		t, err := w.newTab(w.commandParam(p), str(p, "cwd"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"tab": t.ID, "pane": w.FocusedPane}, nil

	case "pane.list":
		out := []any{}
		for _, pane := range w.visiblePanes() {
			out = append(out, w.paneJSON(pane))
		}
		return out, nil

	case "pane.text":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		scrollback := boolParam(p, "scrollback")
		return map[string]any{
			"pane":       pane.ID,
			"title":      pane.title(),
			"cwd":        pane.dir(),
			"text":       pane.text(),
			"scrollback": scrollback,
		}, nil

	case "pane.send":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		text := str(p, "text")
		if text == "" {
			return nil, fmt.Errorf("pane.send requires a text parameter")
		}
		pane.send(text)
		return map[string]any{"pane": pane.ID}, nil

	case "pane.split":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		ax, err := parseAxis(str(p, "axis"))
		if err != nil {
			return nil, err
		}
		np, err := w.splitPane(pane, ax, w.commandParam(p), str(p, "cwd"))
		if err != nil {
			return nil, err
		}
		name := "horizontal"
		if ax == axisVertical {
			name = "vertical"
		}
		return map[string]any{"pane": np.ID, "axis": name}, nil

	case "pane.close":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		w.closePane(pane)
		return map[string]any{"closed": pane.ID}, nil

	case "pane.focus":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		w.focusPane(pane)
		return map[string]any{"pane": pane.ID}, nil

	case "debug.proc":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		group := pane.foregroundGroup()
		out := map[string]any{"group": group, "agent": agentJSON(pane.agent)}
		if snap, ok := snapshotOf(group); ok {
			out["leader"] = map[string]any{
				"pid":  snap.pid,
				"pgid": snap.pgid,
				"path": snap.path,
				"args": snap.args,
			}
		}
		return out, nil

	case "app.settings":
		return map[string]any{"opened": false}, nil

	case "app.quit":
		w.quitting = true
		go func() {
			time.Sleep(150 * time.Millisecond)
			mygo.App.Quit()
		}()
		return map[string]any{"quitting": true}, nil

	default:
		return nil, fmt.Errorf("Unknown method: %s", req.Method)
	}
}

func (w *workspace) paneJSON(p *pane) map[string]any {
	tabID := ""
	if t := w.tabOf(p); t != nil {
		tabID = t.ID
	}
	return map[string]any{
		"id":             p.ID,
		"title":          p.title(),
		"cwd":            p.dir(),
		"focused":        p.ID == w.FocusedPane,
		"hasFocus":       p.ID == w.FocusedPane,
		"hasSurface":     true,
		"tab":            tabID,
		"command":        p.command,
		"restoreCommand": p.restoreCommand,
		"agent":          agentJSON(p.agent),
	}
}

func agentJSON(a *agentDetection) any {
	if a == nil {
		return nil
	}
	return map[string]any{"kind": string(a.Kind), "session": nullable(a.SessionID)}
}

func (w *workspace) tabsJSON() []any {
	out := []any{}
	for _, t := range w.Tabs {
		ids := []string{}
		for _, pane := range t.Root.panes() {
			ids = append(ids, pane.ID)
		}
		out = append(out, map[string]any{
			"id":       t.ID,
			"title":    t.title(),
			"selected": t.ID == w.SelectedTab,
			"panes":    ids,
		})
	}
	return out
}

// commandParam turns "command" into the login-shell invocation JustCli uses,
// so PATH, aliases, and shell init apply.
func (w *workspace) commandParam(p map[string]json.RawMessage) []string {
	if raw := str(p, "command"); raw != "" {
		return shellCommand(raw)
	}
	return nil
}

func parseAxis(raw string) (axis, error) {
	switch strings.ToLower(raw) {
	case "", "h", "horizontal", "right":
		return axisHorizontal, nil
	case "v", "vertical", "down":
		return axisVertical, nil
	default:
		return 0, fmt.Errorf("Unknown split axis: %s", raw)
	}
}

func str(p map[string]json.RawMessage, key string) string {
	raw, ok := p[key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func boolParam(p map[string]json.RawMessage, key string) bool {
	raw, ok := p[key]
	if !ok {
		return false
	}
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// startControl serves the socket. It returns a function that removes it.
func startControl(w *workspace) func() {
	dir := supportDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("control: %v", err)
		return func() {}
	}
	path := socketPath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("control: %v", err)
		return func() {}
	}
	_ = os.Chmod(path, 0o600)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveControl(w, conn)
		}
	}()

	return func() {
		_ = ln.Close()
		_ = os.Remove(path)
	}
}

func serveControl(w *workspace, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	if !scanner.Scan() {
		return
	}
	var req controlRequest
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		_ = json.NewEncoder(conn).Encode(controlResponse{ID: "", Error: err.Error()})
		return
	}

	resp := w.handle(req)
	_ = json.NewEncoder(conn).Encode(resp)
}
