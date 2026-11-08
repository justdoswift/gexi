package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeRemoteBackend stands in for the workspace: the HTTP layer only needs
// state and a dispatch, not a window.
type fakeRemoteBackend struct {
	mu      sync.Mutex
	enabled bool
	code    string
	devices []remoteDevice
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
	if !remoteMethods[req.Method] {
		return controlResponse{ID: req.ID, Error: "method not allowed"}
	}
	return controlResponse{ID: req.ID, OK: true, Result: map[string]any{"method": req.Method}}
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
		"pane.text", "pane.send", "session.list", "pane.list",
		"pane.split", "pane.close", "pane.focus", "session.close",
	} {
		if !remoteMethods[allowed] {
			t.Errorf("%s should be allowed", allowed)
		}
	}
	for _, denied := range []string{"app.quit", "app.settings", "project.new", "session.new", "event", "debug.proc"} {
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
