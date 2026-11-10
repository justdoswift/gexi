package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// streamBackend plays the host's panes for the stream test: each read hands
// out the next chunk of output, so a test can watch the frames arrive.
type streamBackend struct {
	mu     sync.Mutex
	chunks []string
	reads  int
	sizes  [][2]int
	panes  []string
	wakeCh chan struct{}
}

// remoteWake is the program writing to the pane: a closed channel wakes the
// push loop at once, and the next call parks on a fresh one.
func (s *streamBackend) remoteWake(string) (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wakeCh == nil {
		s.wakeCh = make(chan struct{})
	}
	return s.wakeCh, func() {}
}

// poke stands in for the program's next write.
func (s *streamBackend) poke() {
	s.mu.Lock()
	if s.wakeCh != nil {
		close(s.wakeCh)
	}
	s.wakeCh = make(chan struct{})
	s.mu.Unlock()
}

func (s *streamBackend) remoteEnabled() bool { return true }
func (s *streamBackend) remoteCode() string  { return "123456" }
func (s *streamBackend) remoteDevices() []remoteDevice {
	return []remoteDevice{{ID: "d1", Name: "test", TokenHash: hashRemoteToken("token-1")}}
}
func (s *streamBackend) remoteAddDevice(remoteDevice) {}
func (s *streamBackend) remoteName() string           { return "stream-host" }

func (s *streamBackend) remoteDispatch(req controlRequest) controlResponse {
	if req.Method == "pane.send" {
		return controlResponse{ID: req.ID, OK: true, Result: map[string]any{"pane": "p1"}}
	}
	if req.Method != "pane.stream" {
		return controlResponse{ID: req.ID, Error: "method not allowed"}
	}
	var pane string
	var offset int64
	var cols, rows int
	if raw, ok := req.Params["pane"]; ok {
		_ = json.Unmarshal(raw, &pane)
	}
	if raw, ok := req.Params["offset"]; ok {
		_ = json.Unmarshal(raw, &offset)
	}
	if raw, ok := req.Params["cols"]; ok {
		_ = json.Unmarshal(raw, &cols)
	}
	if raw, ok := req.Params["rows"]; ok {
		_ = json.Unmarshal(raw, &rows)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	s.panes = append(s.panes, pane)
	s.sizes = append(s.sizes, [2]int{cols, rows})
	var chunk string
	if len(s.chunks) > 0 {
		chunk = s.chunks[0]
		s.chunks = s.chunks[1:]
	}
	out := map[string]any{
		"pane":   pane,
		"title":  "shell",
		"cwd":    "/tmp",
		"offset": offset + int64(len(chunk)),
		"data":   base64String(chunk),
		"cols":   cols,
		"rows":   rows,
		"viewW":  float32(cols) * 8,
		"viewH":  float32(rows) * 16,
	}
	if offset == 0 {
		out["reset"] = true
		out["snapshot"] = base64String("screen")
	}
	return controlResponse{ID: req.ID, OK: true, Result: out}
}

func (s *streamBackend) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func (s *streamBackend) chunkCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks)
}

func base64String(s string) string {
	if s == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestRemoteStreamPushesFrames(t *testing.T) {
	backend := &streamBackend{chunks: []string{"hello ", "world"}}
	srv := httptest.NewServer(newRemoteHandler(backend))
	defer srv.Close()

	rc := &remoteConn{
		client: remoteClient{Name: "host", Address: strings.TrimPrefix(srv.URL, "http://"), Token: "token-1"},
		stop:   make(chan struct{}),
		host:   remoteHost{Address: strings.TrimPrefix(srv.URL, "http://"), Token: "token-1"},
	}
	p := &pane{remote: &remotePaneLink{conn: rc, id: "p1"}}
	defer func() {
		if s := rc.streamConn; s != nil {
			s.fail(errStreamClosed)
		}
	}()

	stop := make(chan struct{})

	// The mirror starts with a snapshot of the host's screen.
	v, ok := rc.nextPushedView(p, stop, true)
	if !ok {
		t.Fatal("first pushed view: stream did not come up")
	}
	if !v.Reset || v.Snapshot == "" {
		t.Fatalf("first frame should be a snapshot, got reset=%v snapshot=%q", v.Reset, v.Snapshot)
	}

	// The next chunk arrives over the same connection, without asking.
	next, ok := rc.nextPushedView(p, stop, false)
	if !ok {
		t.Fatal("second frame: stream did not come up")
	}
	raw, err := base64.StdEncoding.DecodeString(next.Data)
	if err != nil {
		t.Fatalf("second frame: bad data: %v", err)
	}
	if string(raw) != "world" {
		t.Fatalf("second frame = %q, want %q", raw, "world")
	}

	// A quiet pane is not polled: the push waits on the program instead.
	backend.mu.Lock()
	backend.chunks = append(backend.chunks, "quiet")
	backend.mu.Unlock()
	before := backend.count()
	time.Sleep(120 * time.Millisecond)
	if after := backend.count(); after-before != 0 {
		t.Fatalf("host read %d times while the pane was quiet, want the push to wait", after-before)
	}

	// And the next write wakes it at once, without waiting out the idle
	// check.
	start := time.Now()
	backend.poke()
	quick, ok := rc.nextPushedView(p, stop, false)
	if !ok {
		t.Fatal("write after quiet: stream did not come up")
	}
	if waited := time.Since(start); waited > 150*time.Millisecond {
		t.Fatalf("frame took %v after the program wrote, want the wake to push it", waited)
	}
	if raw, err := base64.StdEncoding.DecodeString(quick.Data); err != nil || string(raw) != "quiet" {
		t.Fatalf("frame after quiet = %q (%v), want %q", raw, err, "quiet")
	}

	close(stop)
}

func TestRemoteStreamRejectsUnpaired(t *testing.T) {
	backend := &streamBackend{}
	srv := httptest.NewServer(newRemoteHandler(backend))
	defer srv.Close()

	rc := &remoteConn{
		client: remoteClient{Address: strings.TrimPrefix(srv.URL, "http://"), Token: "wrong"},
		stop:   make(chan struct{}),
	}
	p := &pane{remote: &remotePaneLink{conn: rc, id: "p1"}}
	defer func() {
		if s := rc.streamConn; s != nil {
			s.fail(errStreamClosed)
		}
	}()
	stop := make(chan struct{})
	defer close(stop)

	if _, ok := rc.nextPushedView(p, stop, true); ok {
		t.Fatal("an unpaired device should not get frames")
	}
}

func TestMergeViews(t *testing.T) {
	a := remoteStreamView{Data: "ab", Offset: 2, Cols: 80, Rows: 24, Title: "one"}
	b := remoteStreamView{Data: "cd", Offset: 4, Cols: 100, Rows: 30, Title: "two"}
	got := mergeViews(a, b)
	if got.Data != "abcd" {
		t.Fatalf("data = %q, want the frames joined in order", got.Data)
	}
	if got.Offset != 4 || got.Cols != 100 || got.Rows != 30 || got.Title != "two" {
		t.Fatalf("merged view kept %+v, want the later frame's size and offset", got)
	}

	reset := remoteStreamView{Data: "x", Offset: 9, Reset: true, Snapshot: "screen"}
	if got := mergeViews(a, reset); got.Data != "x" || got.Snapshot != "screen" {
		t.Fatalf("a reset frame should replace what was queued, got %+v", got)
	}

	first := remoteStreamView{Data: "y", Offset: 1, Reset: true, Snapshot: "screen"}
	merged := mergeViews(first, b)
	if merged.Snapshot != "screen" {
		t.Fatalf("merging after a reset lost the snapshot: %+v", merged)
	}
	if merged.Data != "ycd" {
		t.Fatalf("data after a reset frame = %q, want the output that followed it", merged.Data)
	}
}
