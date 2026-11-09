package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRemoteBackend stands in for the workspace: the HTTP layer only needs
// state and a dispatch, not a window.
type fakeRemoteBackend struct {
	mu      sync.Mutex
	enabled bool
	code    string
	devices []remoteDevice
	methods []string
	params  map[string]map[string]json.RawMessage
}

func (f *fakeRemoteBackend) remoteEnabled() bool { return f.enabled }

func (f *fakeRemoteBackend) remoteCode() string { return f.code }

func (f *fakeRemoteBackend) remoteDevices() []remoteDevice {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remoteDevice(nil), f.devices...)
}

func (f *fakeRemoteBackend) remoteAddDevice(d remoteDevice) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = append(f.devices, d)
}

func (f *fakeRemoteBackend) remoteDispatch(req controlRequest) controlResponse {
	f.mu.Lock()
	f.methods = append(f.methods, req.Method)
	if f.params == nil {
		f.params = map[string]map[string]json.RawMessage{}
	}
	f.params[req.Method] = req.Params
	f.mu.Unlock()
	if !remoteMethods[req.Method] {
		return controlResponse{ID: req.ID, Error: "method not allowed"}
	}
	return controlResponse{ID: req.ID, OK: true, Result: map[string]any{"method": req.Method}}
}

// sawMethod reports whether the paired device called the method.
func (f *fakeRemoteBackend) sawMethod(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.methods, name)
}

// paramsOf is the last params the paired device sent with the method.
func (f *fakeRemoteBackend) paramsOf(name string) map[string]json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.params[name]
}

func (f *fakeRemoteBackend) remoteName() string { return "test-mac" }

func newTestRemote(t *testing.T) (*httptest.Server, *fakeRemoteBackend) {
	t.Helper()
	backend := &fakeRemoteBackend{enabled: true, code: "123456"}
	return httptest.NewServer(newRemoteHandler(backend)), backend
}

func postJSON(t *testing.T, url, token string, body any) (*http.Response, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp, buf.Bytes()
}

func TestRemotePairing(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()

	resp, _ := postJSON(t, srv.URL+"/api/pair", "", map[string]string{
		"code": "000000", "device": "iPhone",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong code: got %d, want 401", resp.StatusCode)
	}

	resp, body := postJSON(t, srv.URL+"/api/pair", "", map[string]string{
		"code": "123456", "device": "iPhone",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("right code: got %d: %s", resp.StatusCode, body)
	}
	var paired struct {
		Token string `json:"token"`
		Host  string `json:"host"`
	}
	if err := json.Unmarshal(body, &paired); err != nil {
		t.Fatal(err)
	}
	if paired.Token == "" || paired.Host != "test-mac" {
		t.Fatalf("pairing returned %+v", paired)
	}
	if got := backend.remoteDevices(); len(got) != 1 || got[0].TokenHash != hashRemoteToken(paired.Token) {
		t.Fatalf("device not kept: %+v", got)
	}
}

func TestRemotePairingRateLimited(t *testing.T) {
	srv, _ := newTestRemote(t)
	defer srv.Close()

	for i := 0; i < pairMaxFails; i++ {
		resp, _ := postJSON(t, srv.URL+"/api/pair", "", map[string]string{"code": "999999"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("try %d: got %d, want 401", i, resp.StatusCode)
		}
	}
	resp, _ := postJSON(t, srv.URL+"/api/pair", "", map[string]string{"code": "123456"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after %d failures: got %d, want 429", pairMaxFails, resp.StatusCode)
	}
}

func TestRemotePairingOff(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()
	backend.enabled = false

	resp, _ := postJSON(t, srv.URL+"/api/pair", "", map[string]string{"code": "123456"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("remote off: got %d, want 403", resp.StatusCode)
	}
}

func TestRemoteRPC(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()

	_, body := postJSON(t, srv.URL+"/api/pair", "", map[string]string{"code": "123456"})
	var paired struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &paired); err != nil {
		t.Fatal(err)
	}

	resp, body := postJSON(t, srv.URL+"/api/rpc", paired.Token, map[string]any{
		"id": "a", "method": "session.list", "params": map[string]any{},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rpc: got %d: %s", resp.StatusCode, body)
	}
	var env controlResponse
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK {
		t.Fatalf("rpc failed: %s", env.Error)
	}

	resp, _ = postJSON(t, srv.URL+"/api/rpc", "", map[string]any{
		"id": "b", "method": "session.list", "params": map[string]any{},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", resp.StatusCode)
	}

	resp, _ = postJSON(t, srv.URL+"/api/rpc", paired.Token, map[string]any{
		"id": "c", "method": "app.quit", "params": map[string]any{},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("app.quit: got %d, want 403", resp.StatusCode)
	}

	// Revoking the device ends its access at once.
	backend.mu.Lock()
	backend.devices = nil
	backend.mu.Unlock()
	resp, _ = postJSON(t, srv.URL+"/api/rpc", paired.Token, map[string]any{
		"id": "d", "method": "session.list", "params": map[string]any{},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked: got %d, want 401", resp.StatusCode)
	}
}

func TestNormalizeRemoteAddress(t *testing.T) {
	cases := map[string]string{
		"192.168.1.23":               "192.168.1.23:47823",
		"192.168.1.23:1234":          "192.168.1.23:1234",
		"http://192.168.1.23":        "192.168.1.23:47823",
		"http://192.168.1.23:5000/":  "192.168.1.23:5000",
		"  https://mac.local:8000  ": "mac.local:8000",
		"":                           "",
		"http://":                    "",
	}
	for in, want := range cases {
		if got := normalizeRemoteAddress(in); got != want {
			t.Errorf("normalizeRemoteAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPairingCode(t *testing.T) {
	for i := 0; i < 20; i++ {
		code := newPairingCode()
		if !isPairingCode(code) {
			t.Fatalf("newPairingCode returned %q", code)
		}
		if len(code) != 6 {
			t.Fatalf("code %q is not 6 digits", code)
		}
	}
	if isPairingCode("12345") || isPairingCode("12345a") || isPairingCode("") {
		t.Fatal("isPairingCode accepted a bad code")
	}
	for _, code := range []string{"000000", "999999"} {
		if !isPairingCode(code) {
			t.Fatalf("isPairingCode rejected %q", code)
		}
	}
}

func TestRemoteMethodWhitelist(t *testing.T) {
	// A paired window draws the pane header the host does, so the buttons
	// that header shows work through the same methods.
	for _, allowed := range []string{
		"pane.text", "pane.send", "session.list", "pane.list", "project.list",
		"pane.split", "pane.close", "pane.focus", "pane.worktree",
		"session.new", "session.close",
	} {
		if !remoteMethods[allowed] {
			t.Errorf("%s should be allowed", allowed)
		}
	}
	for _, denied := range []string{"app.quit", "app.settings", "project.new", "event", "debug.proc"} {
		if remoteMethods[denied] {
			t.Errorf("%s should not be allowed", denied)
		}
	}
}

// A session's layout goes over the wire as the tree the window draws, so a
// host's splits show up on the client as the same splits.
func TestRemoteLayout(t *testing.T) {
	root := &splitNode{
		axis:  axisHorizontal,
		ratio: 60,
		first: leaf(&pane{ID: "a"}),
		second: &splitNode{
			axis:   axisVertical,
			ratio:  40,
			first:  leaf(&pane{ID: "b"}),
			second: leaf(&pane{ID: "c"}),
		},
	}
	data, err := json.Marshal(layoutJSON(root))
	if err != nil {
		t.Fatal(err)
	}
	var got remoteLayout
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Axis != "horizontal" || got.Ratio != 60 || got.First.Pane != "a" {
		t.Fatalf("first side: %+v", got)
	}
	if got.Second == nil || got.Second.Axis != "vertical" {
		t.Fatalf("second side: %+v", got.Second)
	}
	if got.Second.First.Pane != "b" || got.Second.Second.Pane != "c" {
		t.Fatalf("leaves: %+v", got.Second)
	}
}

func TestRemoteTokenHash(t *testing.T) {
	token := newRemoteToken()
	if len(token) != 64 {
		t.Fatalf("token length %d", len(token))
	}
	if hashRemoteToken(token) != hashRemoteToken(token) {
		t.Fatal("hash is not stable")
	}
	if hashRemoteToken(token) == hashRemoteToken(token+"x") {
		t.Fatal("different tokens hashed alike")
	}
}

func TestRemoteClientPairAndCall(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()
	address := strings.TrimPrefix(srv.URL, "http://")

	host, err := pairRemoteHost(address, "123456", "")
	if err != nil {
		t.Fatal(err)
	}
	if host.Name != "test-mac" || host.Token == "" || host.Address != address {
		t.Fatalf("paired host %+v", host)
	}

	client := remoteClient{Address: address, Token: host.Token}
	var out map[string]any
	if err := client.call("session.list", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out["method"] != "session.list" {
		t.Fatalf("call returned %+v", out)
	}

	backend.mu.Lock()
	backend.devices = nil
	backend.mu.Unlock()
	if err := client.call("session.list", nil, &out); !errors.Is(err, errRemoteNotPaired) {
		t.Fatalf("revoked call: got %v, want errRemoteNotPaired", err)
	}
}

func TestRemoteClientRejectsWrongCode(t *testing.T) {
	srv, _ := newTestRemote(t)
	defer srv.Close()
	address := strings.TrimPrefix(srv.URL, "http://")

	if _, err := pairRemoteHost(address, "000000", ""); err == nil {
		t.Fatal("pairing with a wrong code succeeded")
	}
}

// TestRemoteProjectNewSessionAsksTheHost: the sidebar's + on a paired Mac
// makes the session there, not a local shell filed under a remote project.
func TestRemoteProjectNewSessionAsksTheHost(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()
	address := strings.TrimPrefix(srv.URL, "http://")
	host, err := pairRemoteHost(address, "123456", "")
	if err != nil {
		t.Fatal(err)
	}

	w := &workspace{}
	rc := &remoteConn{
		ws:     w,
		host:   host,
		client: remoteClient{Name: host.Name, Address: address, Token: host.Token},
		stop:   make(chan struct{}),
	}
	pr := &project{ID: remoteProjectID(address), Label: host.Name,
		remote: &remoteProjectLink{conn: rc, host: host}}
	rc.project = pr
	w.remoteProjects = []*project{pr}

	s, err := w.newSession(pr, nil, "")
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	if s != nil {
		t.Fatalf("a paired Mac made a local session: %+v", s)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if backend.sawMethod("session.new") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the host was never asked for a session")
}

// TestMirrorSize: the card lays the mirror out at the size the host named,
// and has a size to start from before the first stream brings it.
func TestMirrorSize(t *testing.T) {
	named := &pane{remote: &remotePaneLink{viewW: 812, viewH: 640}}
	if w, h := mirrorSize(named); w != 812 || h != 640 {
		t.Fatalf("named view: %v by %v", w, h)
	}
	bare := &pane{remote: &remotePaneLink{}}
	if w, h := mirrorSize(bare); w <= 0 || h <= 0 {
		t.Fatalf("estimate: %v by %v", w, h)
	}
}

// TestWorktreeJSONRoundTrip: a host sends its panes' checkouts as the
// fields the paired window's header chips read, so the two names must agree.
func TestWorktreeJSONRoundTrip(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	w := &workspace{}
	wt := &paneWorktree{
		Path: gone, Branch: "tokyo", Base: "main", PR: 12,
		URL: "https://example.test/pull/12", State: "open",
		mergeable: "MERGEABLE", review: "APPROVED", checks: "passing",
		mergeState: "CLEAN", busy: worktreeBusyCheck,
	}
	term, _ := probeTerminal(t)
	p := &pane{ID: "a", term: term, initialDir: gone,
		Worktree: wt, worktreeBusy: worktreeBusyCreate}
	raw, err := json.Marshal(w.paneJSON(p))
	if err != nil {
		t.Fatal(err)
	}
	var got remotePane
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	rt := got.Worktree
	if rt == nil {
		t.Fatal("the checkout did not come over")
	}
	if rt.Branch != "tokyo" || rt.Base != "main" || rt.PR != 12 || rt.State != "open" {
		t.Fatalf("checkout = %+v", rt)
	}
	if !rt.Missing {
		t.Fatal("a checkout that is not on disk should say so")
	}
	if rt.Busy != worktreeBusyCheck {
		t.Fatalf("busy = %q, want the host's operation", rt.Busy)
	}
	if got.WorktreeBusy != worktreeBusyCreate {
		t.Fatalf("worktreeBusy = %q, want the one being made", got.WorktreeBusy)
	}
	if label, ready, _ := w.mergeStatusOf(rt.status()); !ready || label == "" {
		t.Fatalf("status = %q ready=%v, want mergeable", label, ready)
	}
}

// TestRemoteMergeStatusMatchesLocal: the Merge chip on a paired Mac colours
// itself from the fields its host sent, as the host's own chip does.
func TestRemoteMergeStatusMatchesLocal(t *testing.T) {
	w := &workspace{}
	cases := []struct {
		name   string
		local  paneWorktree
		remote remoteWorktree
	}{
		{"ready", paneWorktree{State: "open", mergeable: "MERGEABLE", checks: "passing"},
			remoteWorktree{State: "open", Mergeable: "MERGEABLE", Checks: "passing"}},
		{"conflicts", paneWorktree{State: "open", mergeable: "CONFLICTING"},
			remoteWorktree{State: "open", Mergeable: "CONFLICTING"}},
		{"merged", paneWorktree{State: "merged"}, remoteWorktree{State: "merged"}},
		{"review", paneWorktree{State: "open", mergeable: "MERGEABLE", checks: "none", review: "REVIEW_REQUIRED"},
			remoteWorktree{State: "open", Mergeable: "MERGEABLE", Checks: "none", Review: "REVIEW_REQUIRED"}},
	}
	for _, tc := range cases {
		local := &tc.local
		label, ready, danger := w.mergeStatus(local)
		gotLabel, gotReady, gotDanger := w.mergeStatusOf(tc.remote.status())
		if label != gotLabel || ready != gotReady || danger != gotDanger {
			t.Fatalf("%s: host %q/%v/%v, remote %q/%v/%v",
				tc.name, label, ready, danger, gotLabel, gotReady, gotDanger)
		}
		if a, b := w.mergeBlocker(local), w.mergeBlockerOf(tc.remote.status()); a != b {
			t.Fatalf("%s: blocker %q vs %q", tc.name, a, b)
		}
	}
}

// TestPaneWorktreeControlGuards: the controls a paired Mac presses run on
// this Mac's own checkouts only, and only for an action that exists.
func TestPaneWorktreeControlGuards(t *testing.T) {
	away := &pane{ID: "r1", remote: &remotePaneLink{conn: &remoteConn{}}}
	here := &pane{ID: "l1"}
	pr := &project{ID: "p", Sessions: []*session{{ID: "s",
		Root: &splitNode{axis: axisHorizontal, ratio: 50, first: leaf(away), second: leaf(here)}}}}
	w := &workspace{Projects: []*project{pr}, FocusedPane: "r1"}
	call := func(paneID, action string) error {
		_, err := w.dispatch(controlRequest{Method: "pane.worktree", Params: map[string]json.RawMessage{
			"pane":   json.RawMessage(strconv.Quote(paneID)),
			"action": json.RawMessage(strconv.Quote(action)),
		}})
		return err
	}
	if err := call("r1", "create"); err == nil {
		t.Fatal("a pane on another Mac was moved")
	}
	if err := call("l1", "bogus"); err == nil {
		t.Fatal("an unknown worktree action was taken")
	}
}

// TestRemoteNewSessionNamesTheProject: the folder row's + names the host
// project the session belongs in, so the shell starts there rather than in
// whichever project the host's own window last selected.
func TestRemoteNewSessionNamesTheProject(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()
	address := strings.TrimPrefix(srv.URL, "http://")
	host, err := pairRemoteHost(address, "123456", "")
	if err != nil {
		t.Fatal(err)
	}
	w := &workspace{}
	rc := &remoteConn{
		ws:     w,
		host:   host,
		client: remoteClient{Name: host.Name, Address: address, Token: host.Token},
		stop:   make(chan struct{}),
	}
	rc.project = &project{ID: remoteProjectID(address), remote: &remoteProjectLink{conn: rc}}

	rc.newSession("project-7")
	waitForTest(t, 2*time.Second, func() bool { return backend.sawMethod("session.new") })
	params := backend.paramsOf("session.new")
	if got := string(params["project"]); got != `"project-7"` {
		t.Fatalf("session.new project = %s, want %q", got, "project-7")
	}
}

// TestRemoteGroups: a host's sessions are drawn under the folder each runs
// in, in the host's project order, and a session in a project the host did
// not name goes last without a folder row.
func TestRemoteGroups(t *testing.T) {
	rc := &remoteConn{projects: []remoteProject{{ID: "p1", Name: "one"}, {ID: "p2", Name: "two"}}}
	mk := func(id, project string) *session {
		return &session{ID: id, remote: &remoteSessionLink{id: id, project: project}}
	}
	groups := rc.groups([]*session{mk("a", "p2"), mk("b", "p1"), mk("c", "gone")})
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].project.ID != "p1" || len(groups[0].sessions) != 1 || groups[0].sessions[0].ID != "b" {
		t.Fatalf("first group = %+v", groups[0])
	}
	if groups[1].project.ID != "p2" || len(groups[1].sessions) != 1 || groups[1].sessions[0].ID != "a" {
		t.Fatalf("second group = %+v", groups[1])
	}
	if groups[2].project.ID != "" || len(groups[2].sessions) != 1 || groups[2].sessions[0].ID != "c" {
		t.Fatalf("last group = %+v", groups[2])
	}
}

// TestRemoteTargetProject: a session made on a paired Mac belongs where the
// selected session runs; with nothing selected, the host's first project.
func TestRemoteTargetProject(t *testing.T) {
	rc := &remoteConn{projects: []remoteProject{{ID: "first"}}}
	pr := &project{ID: remoteProjectID("x"), remote: &remoteProjectLink{conn: rc}}
	pr.Sessions = []*session{{ID: "s", remote: &remoteSessionLink{conn: rc, id: "rs", project: "here"}}}
	w := &workspace{remoteProjects: []*project{pr}, SelectedSession: "s"}
	if got := w.remoteTargetProject(rc); got != "here" {
		t.Fatalf("selected session's project = %q, want %q", got, "here")
	}
	// With nothing selected, the window shows the host's first session, so
	// the session follows the same project.
	w.SelectedSession = ""
	if got := w.remoteTargetProject(rc); got != "here" {
		t.Fatalf("with nothing selected = %q, want %q", got, "here")
	}
	// A host with no session yet has no folder the window shows: its first
	// project is where the new one goes.
	pr.Sessions = nil
	if got := w.remoteTargetProject(rc); got != "first" {
		t.Fatalf("host with no sessions = %q, want %q", got, "first")
	}
}

// TestRemoteWantGrid: the card asks the host for the columns and rows it
// fits at the measured cell size, keeping one of each back for rounding.
func TestRemoteWantGrid(t *testing.T) {
	l := &remotePaneLink{cellW: 7.55, cellH: 19.2}
	cols, rows := l.wantGrid(1285, 809)
	if cols != 166 || rows != 40 {
		t.Fatalf("want %vx%v, want 166x40", cols, rows)
	}
	// The cell's size comes from the first stream that names the host's view.
	named := &remotePaneLink{viewW: 1285, viewH: 809, gridCols: 168, gridRows: 41}
	named.measureCell()
	if named.cellW <= 0 || named.cellH <= 0 {
		t.Fatalf("measured cell %vx%v", named.cellW, named.cellH)
	}
	if cols, rows := named.wantGrid(1285, 809); cols != 167 || rows != 40 {
		t.Fatalf("measured want %vx%v, want 167x40", cols, rows)
	}
	// A card with no size yet asks for nothing.
	if cols, rows := l.wantGrid(0, 0); cols != 10 || rows != 3 {
		t.Fatalf("empty card want %vx%v, want the floor", cols, rows)
	}
}

// TestWatchRemoteSize: a watching Mac's size takes the pane's terminal, and
// the view is then laid out at exactly that grid.
func TestWatchRemoteSize(t *testing.T) {
	term, _ := probeTerminal(t)
	p := &pane{term: term}
	p.watchRemoteSize(100, 30)
	if cols, rows := term.Size(); cols != 100 || rows != 30 {
		t.Fatalf("terminal %vx%v, want 100x30", cols, rows)
	}
	if !p.remoteWatched() {
		t.Fatal("the pane does not report the watching Mac")
	}
	w, h := remoteViewSize(100, 30, 7.5, 19)
	if w != 774 || h != 590 {
		t.Fatalf("view %vx%v, want 774x590", w, h)
	}
}

// TestWatchRemoteSizeLeavesTheSettledGrid: the pane's own view sizes the
// terminal from the held grid every frame, so its rounding decides where the
// terminal settles. A poll that names the same grid again must leave that
// size alone: forcing it back on every poll sends a SIGWINCH at the poll's
// rate, and a full-screen agent redraws its whole screen each time.
func TestWatchRemoteSizeLeavesTheSettledGrid(t *testing.T) {
	term, _ := probeTerminal(t)
	p := &pane{term: term}
	p.watchRemoteSize(100, 30)
	// The view rounds the card's width down a column, as a card narrower
	// than the requested grid does.
	term.Resize(99, 30)
	p.watchRemoteSize(100, 30)
	if cols, rows := term.Size(); cols != 99 || rows != 30 {
		t.Fatalf("a repeated poll resized the terminal to %vx%v, want 99x30", cols, rows)
	}
	// A new grid still takes the pane, and a watch that lapsed applies the
	// grid again when the Mac comes back.
	p.watchRemoteSize(120, 40)
	if cols, rows := term.Size(); cols != 120 || rows != 40 {
		t.Fatalf("a new request left the terminal at %vx%v, want 120x40", cols, rows)
	}
	term.Resize(118, 40)
	p.remoteWatchAt = time.Now().Add(-remoteWatchTTL - time.Second)
	p.watchRemoteSize(120, 40)
	if cols, rows := term.Size(); cols != 120 || rows != 40 {
		t.Fatalf("a fresh watch left the terminal at %vx%v, want 120x40", cols, rows)
	}
}

// TestStreamViewSizeFollowsTheHeldGrid: the watching Mac measures a cell from
// the view size and grid the stream names, so while it watches both must come
// from the held grid, not from the card the pane was laid out at before.
func TestStreamViewSizeFollowsTheHeldGrid(t *testing.T) {
	term, _ := probeTerminal(t)
	p := &pane{term: term, cellW: 7.5, cellH: 19, viewW: 1000, viewH: 500}
	if w, h := p.streamViewSize(120, 30); w != 1000 || h != 500 {
		t.Fatalf("unwatched view %vx%v, want the last card's 1000x500", w, h)
	}
	p.watchRemoteSize(120, 30)
	if w, h := p.streamViewSize(120, 30); w != 924 || h != 590 {
		t.Fatalf("watched view %vx%v, want 924x590", w, h)
	}
}

// TestRemoteMirrorTop: a mirror taller than its card shows the lowest row
// with text at the card's bottom, so a full-screen program's input line stays
// visible; a shell's prompt near the top leaves the view at the top.
func TestRemoteMirrorTop(t *testing.T) {
	// A screen that fits needs no shift.
	if got := remoteMirrorTop(800, 700, 36, 35); got != 0 {
		t.Fatalf("a fitting screen shifted by %v", got)
	}
	// 40 rows in 800 DIPs: 19.5 a row with the view's 10 a side padding.
	top := remoteMirrorTop(700, 800, 40, 39)
	if top >= 0 {
		t.Fatalf("a taller screen stayed at the top: %v", top)
	}
	if bottom := top + 10 + 40*19.5; bottom != 700 {
		t.Fatalf("the last row ends at %v, want the card's 700", bottom)
	}
	// A prompt on row 2 keeps the top of the screen in view.
	if got := remoteMirrorTop(700, 800, 40, 2); got != 0 {
		t.Fatalf("a top row shifted the view by %v", got)
	}
	// An anchor past the screen is clamped to its last row.
	if got, want := remoteMirrorTop(700, 800, 40, 99), remoteMirrorTop(700, 800, 40, 39); got != want {
		t.Fatalf("anchor past the screen = %v, want %v", got, want)
	}
}

// TestLeaveWorktreeFromRemoteRefusals: leaving a checkout the host did not
// make or one that is busy is refused, and the answer says which so the
// client can say it too. An agent in the pane no longer refuses: the host
// stops it as the local Leave button does.
func TestLeaveWorktreeFromRemoteRefusals(t *testing.T) {
	w := &workspace{}
	detected := &pane{ID: "a", Worktree: &paneWorktree{Detected: true}}
	if out, _ := w.leaveWorktreeFromRemote(detected).(map[string]any); out["left"] != false {
		t.Fatalf("a detected checkout was left: %+v", out)
	}
	busy := &pane{ID: "b", Worktree: &paneWorktree{busy: worktreeBusyPR}}
	if out, _ := w.leaveWorktreeFromRemote(busy).(map[string]any); out["left"] != false {
		t.Fatalf("a busy checkout was left: %+v", out)
	}

	t.Setenv("GEXI_HOME", t.TempDir())
	root := initRepoTest(t)
	wt, err := createWorktree(root)
	if err != nil || wt == nil {
		t.Fatalf("create worktree: %v %+v", err, wt)
	}
	running := &pane{ID: "c", Worktree: wt, agent: &agentDetection{Kind: agentClaude}}
	if out, _ := w.leaveWorktreeFromRemote(running).(map[string]any); out["left"] != true {
		t.Fatalf("a clean checkout with an agent was not left: %+v", out)
	}
}
