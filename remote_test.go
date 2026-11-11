package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func (f *fakeRemoteBackend) remoteWake(string) (<-chan struct{}, func()) {
	return make(chan struct{}), func() {}
}

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
	cases := []struct {
		in     string
		want   string
		secure bool
	}{
		{"192.168.1.23", "192.168.1.23:47823", false},
		{"192.168.1.23:1234", "192.168.1.23:1234", false},
		{"http://192.168.1.23", "192.168.1.23:47823", false},
		{"http://192.168.1.23:5000/", "192.168.1.23:5000", false},
		// An https address keeps TLS and takes 443, never gexi's cleartext port.
		{"https://relay.example.com", "relay.example.com:443", true},
		{"  https://mac.local:8000  ", "mac.local:8000", true},
		{"", "", false},
		{"http://", "", false},
	}
	for _, c := range cases {
		got, secure := normalizeRemoteAddress(c.in)
		if got != c.want || secure != c.secure {
			t.Errorf("normalizeRemoteAddress(%q) = %q, %v; want %q, %v", c.in, got, secure, c.want, c.secure)
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

	host, err := pairRemoteHost(address, false, "123456", "")
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

	if _, err := pairRemoteHost(address, false, "000000", ""); err == nil {
		t.Fatal("pairing with a wrong code succeeded")
	}
}

// TestRemoteProjectNewSessionAsksTheHost: the sidebar's + on a paired Mac
// makes the session there, not a local shell filed under a remote project.
func TestRemoteProjectNewSessionAsksTheHost(t *testing.T) {
	srv, backend := newTestRemote(t)
	defer srv.Close()
	address := strings.TrimPrefix(srv.URL, "http://")
	host, err := pairRemoteHost(address, false, "123456", "")
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
	host, err := pairRemoteHost(address, false, "123456", "")
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

// TestPairingCodeExpires: a code left in the settings file stops pairing on
// its own, so an old screenshot is worth nothing.
func TestPairingCodeExpires(t *testing.T) {
	w := &workspace{}
	w.settings.RemoteCode = "123456"
	w.settings.RemoteCodeAt = time.Now().Add(-pairingCodeTTL - time.Minute).Format(time.RFC3339)
	if got := w.remoteCode(); got != "" {
		t.Fatalf("expired code still pairs: %q", got)
	}
	w.settings.RemoteCodeAt = time.Now().Format(time.RFC3339)
	if got := w.remoteCode(); got != "123456" {
		t.Fatalf("fresh code = %q, want 123456", got)
	}
}

// TestPairingLimiterStopsSpreadGuesses: the per-address cap alone would let a
// guess walk six digits from many addresses, so the whole server has a cap.
func TestPairingLimiterStopsSpreadGuesses(t *testing.T) {
	l := newPairLimiter()
	for i := 0; i < pairGlobalFails; i++ {
		if !l.allow(fmt.Sprintf("10.0.0.%d", i)) {
			t.Fatalf("refused too early at %d", i)
		}
		l.fail(fmt.Sprintf("10.0.0.%d", i))
	}
	if l.allow("192.0.2.1") {
		t.Fatal("a fresh address was still allowed after the global cap")
	}
}

// TestRemoteEnabledWithRelay: a built-in relay serves the same pages, so
// pairing must not answer "remote access is off" on a Mac whose LAN switch
// is off.
func TestRemoteEnabledWithRelay(t *testing.T) {
	w := &workspace{settings: appSettings{RelayEnabled: true}}
	if !w.remoteEnabled() {
		t.Fatal("a Mac with a relay reported remote access off")
	}
}

// TestChooseRoutePrefersLAN: the link picks the Mac's LAN address when it
// answers, and the relay when it does not.
func TestChooseRoutePrefersLAN(t *testing.T) {
	ok := func(rw http.ResponseWriter, r *http.Request) {
		writeJSON(rw, http.StatusOK, map[string]any{"id": "x", "ok": true})
	}
	lan := httptest.NewServer(http.HandlerFunc(ok))
	defer lan.Close()
	relay := httptest.NewServer(http.HandlerFunc(ok))
	defer relay.Close()

	strip := func(u string) string { return strings.TrimPrefix(u, "http://") }
	rc := &remoteConn{
		host:   remoteHost{Address: strip(relay.URL), Token: "t", LAN: []string{strip(lan.URL)}},
		client: remoteClient{Name: "h", Address: strip(relay.URL), Token: "t"},
	}
	rc.chooseRoute()
	if rc.address != strip(lan.URL) {
		t.Fatalf("route = %q, want the LAN address", rc.address)
	}

	// The LAN goes away: the next check falls back to the relay.
	lan.Close()
	rc.routeAt = time.Time{}
	rc.chooseRoute()
	if rc.address != strip(relay.URL) {
		t.Fatalf("route = %q, want the relay", rc.address)
	}
}

// A pane gives its grid to the Mac watching it only while this window is in
// the background: the window being used owns the grid, even while nobody
// types in it, so the pane never resizes under someone reading it.
func TestRemoteWatchedFollowsTheWindow(t *testing.T) {
	ws := &workspace{windowActive: true}
	p := &pane{ws: ws, remoteCols: 100, remoteRows: 30, remoteWatchAt: time.Now()}

	if p.remoteWatched() {
		t.Fatal("an active window gave its grid to the watching Mac")
	}

	ws.windowActive = false
	if !p.remoteWatched() {
		t.Fatal("a background window did not give the pane to the watching Mac")
	}

	p.remoteWatchAt = time.Now().Add(-remoteWatchTTL - time.Second)
	if p.remoteWatched() {
		t.Fatal("a Mac that stopped watching kept the grid")
	}
}

// TestActiveWindowKeepsTheGrid: a poll from a watching Mac must not resize
// the pane while this window is in use, since the pane is on screen and the
// grid would change under the reader. The request waits for the window to
// go to the background, then takes the pane.
func TestActiveWindowKeepsTheGrid(t *testing.T) {
	term, _ := probeTerminal(t)
	ws := &workspace{windowActive: true}
	p := &pane{ws: ws, term: term}

	p.watchRemoteSize(100, 30)
	if cols, rows := term.Size(); cols == 100 && rows == 30 {
		t.Fatal("an active window's pane took the watching Mac's grid")
	}

	ws.windowActive = false
	p.watchRemoteSize(100, 30)
	if cols, rows := term.Size(); cols != 100 || rows != 30 {
		t.Fatalf("the grid in the background = %dx%d, want 100x30", cols, rows)
	}
}
