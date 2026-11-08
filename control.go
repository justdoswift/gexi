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

// The wire format is gexi's: one JSON request line per connection, one
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
			"instance":        w.InstanceID,
			"socket":          socketPath(),
			"version":         Version,
			"selectedSession": nullable(w.SelectedSession),
			"focusedPane":     nullable(w.FocusedPane),
			"projects":        w.projectsJSON(),
			"paneStyle":       w.settings.PaneStyle,
		}, nil

	case "project.list":
		return w.projectsJSON(), nil

	case "project.new":
		pr, s, err := w.openProject(str(p, "cwd"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"project": pr.ID, "session": s.ID, "pane": w.FocusedPane}, nil

	case "session.list":
		out := []any{}
		for _, pr := range w.Projects {
			for _, s := range pr.Sessions {
				out = append(out, w.sessionJSON(pr, s))
			}
		}
		return out, nil

	case "session.select":
		s, err := w.resolveSession(str(p, "session"))
		if err != nil {
			return nil, err
		}
		w.selectSession(s.ID)
		return map[string]any{"session": s.ID}, nil

	case "session.new":
		var err error
		var pr *project
		if raw := str(p, "project"); raw != "" {
			found, err := w.resolveProject(raw)
			if err != nil {
				return nil, err
			}
			pr = found
		} else if cur := w.selected(); cur != nil {
			pr = w.projectOf(cur)
		} else {
			pr, _, err = w.openProject(str(p, "cwd"))
			if err != nil {
				return nil, err
			}
		}
		s, err := w.newSession(pr, w.commandParam(p), str(p, "cwd"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"session": s.ID, "pane": w.FocusedPane}, nil

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
		w.requestClose(pane)
		return map[string]any{"closed": pane.ID}, nil

	case "pane.focus":
		pane, err := w.resolvePane(str(p, "pane"))
		if err != nil {
			return nil, err
		}
		w.focusPane(pane)
		return map[string]any{"pane": pane.ID}, nil

	case "event":
		ev := agentEvent{
			Kind:    str(p, "kind"),
			Agent:   str(p, "agent"),
			Pane:    str(p, "pane"),
			Session: str(p, "session"),
			Message: str(p, "message"),
		}
		ev.Detail = p["detail"]
		return w.handleEvent(ev)

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
		w.openSettings()
		return map[string]any{"opened": true}, nil

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
	sessionID, projectID := "", ""
	if s := w.sessionOf(p); s != nil {
		sessionID = s.ID
		if pr := w.projectOf(s); pr != nil {
			projectID = pr.ID
		}
	}
	return map[string]any{
		"id":             p.ID,
		"title":          p.title(),
		"cwd":            p.dir(),
		"focused":        p.ID == w.FocusedPane,
		"hasFocus":       p.ID == w.FocusedPane,
		"hasSurface":     true,
		"session":        sessionID,
		"project":        projectID,
		"command":        p.command,
		"restoreCommand": p.restoreCommand,
		"agent":          agentJSON(p.agent),
		"activity":       activityJSON(p.activity),
		"subagents":      p.subagentsJSON(),
	}
}

func agentJSON(a *agentDetection) any {
	if a == nil {
		return nil
	}
	return map[string]any{"kind": string(a.Kind), "session": nullable(a.SessionID)}
}

func (w *workspace) sessionJSON(pr *project, s *session) map[string]any {
	ids := []string{}
	if s.Root != nil {
		for _, pane := range s.Root.panes() {
			ids = append(ids, pane.ID)
		}
	}
	projectID := ""
	if pr != nil {
		projectID = pr.ID
	}
	out := map[string]any{
		"id":       s.ID,
		"title":    s.title(),
		"project":  projectID,
		"selected": s.ID == w.SelectedSession,
		"panes":    ids,
	}
	worktrees := []any{}
	if s.Root != nil {
		for _, p := range s.Root.panes() {
			if p.Worktree == nil {
				continue
			}
			worktrees = append(worktrees, map[string]any{
				"pane":   p.ID,
				"path":   p.Worktree.Path,
				"branch": p.Worktree.Branch,
				"base":   p.Worktree.Base,
				"pr":     nullableInt(p.Worktree.PR),
				"url":    nullable(p.Worktree.URL),
				"state":  nullable(p.Worktree.State),
			})
		}
	}
	if len(worktrees) > 0 {
		out["worktrees"] = worktrees
	}
	return out
}

func (w *workspace) projectsJSON() []any {
	out := []any{}
	for _, pr := range w.Projects {
		sessions := []any{}
		for _, s := range pr.Sessions {
			sessions = append(sessions, w.sessionJSON(pr, s))
		}
		out = append(out, map[string]any{
			"id":        pr.ID,
			"dir":       pr.Dir,
			"name":      pr.name(),
			"collapsed": w.collapsed[pr.ID],
			"sessions":  sessions,
		})
	}
	return out
}

// resolveProject accepts a full project id, a unique prefix, or its folder.
func (w *workspace) resolveProject(raw string) (*project, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if s := w.selected(); s != nil {
			if pr := w.projectOf(s); pr != nil {
				return pr, nil
			}
		}
		if len(w.Projects) > 0 {
			return w.Projects[0], nil
		}
		return nil, fmt.Errorf("No project is open")
	}
	lower := strings.ToLower(raw)
	var matches []*project
	for _, pr := range w.Projects {
		if pr.ID == raw || pr.Dir == raw || strings.ToLower(pr.name()) == lower {
			return pr, nil
		}
		if strings.HasPrefix(strings.ToLower(pr.ID), lower) {
			matches = append(matches, pr)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("No such project: %s", raw)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("Ambiguous project id: %s", raw)
	}
}

// commandParam turns "command" into the login-shell invocation gexi uses,
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

func nullableInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// startControl serves the socket. It returns a function that removes it.
func startControl(w *workspace) func() {
	dir := supportDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("control: %v", err)
		return func() {}
	}
	path := socketPath()
	// A socket that answers belongs to a live instance. Leave it alone
	// rather than take the control channel away from the app that owns it;
	// a socket nobody answers on is stale and is replaced.
	if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		log.Printf("control: %s belongs to another instance", path)
		return func() {}
	}
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
