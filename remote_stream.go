package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// The stream is the pane mirror over one connection: the pairing Mac names
// the panes it shows and the size it fits them at, and this side pushes a
// frame whenever one of them has output. Polling cost a round trip per frame
// and another per keystroke; a pushed frame arrives as soon as the terminal
// has it, so a key's echo pays one trip through the network, not three.

const (
	// streamIdleCheck is how often a pane with no output is looked at anyway,
	// for the changes that do not write to it: a title, a directory, the
	// size the host drew it at.
	streamIdleCheck = 250 * time.Millisecond
	// streamFrameGap caps a pane's frames at the display's own rate: the Mac
	// watching repaints once per frame it gets, and a program that writes in
	// a tight loop would otherwise cost it a repaint per write.
	streamFrameGap = 16 * time.Millisecond
	// streamBeat keeps a quiet stream from looking dead to a proxy that
	// would close it; it is also the ping that fails a dropped link.
	streamBeat = 20 * time.Second
	// streamMaxPanes bounds what one pairing device may watch: a window
	// shows a handful of panes, and each watch costs a read per tick.
	streamMaxPanes = 24
)

// streamRequest is one line the client writes: a pane to watch, or one it
// stopped showing.
type streamRequest struct {
	Op       string `json:"op"`
	Pane     string `json:"pane"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Snapshot bool   `json:"snapshot"`
}

// streamWatch is one pane the client sees: the read offset this side keeps
// for it, the grid the client fits it at, and what the last frame said, so a
// resize with no output still goes out.
type streamWatch struct {
	id       string
	cols     int
	rows     int
	offset   int64
	snapshot bool

	// dirty carries a change the client asked for: a new grid, or a mirror
	// that starts over. A pane with nothing to say waits on the program's
	// output instead, so a quiet mirror costs the main thread nothing.
	dirty     chan struct{}
	sent      bool
	lastCols  int
	lastRows  int
	lastViewW float32
	lastViewH float32
	lastTitle string
	lastCwd   string
}

type streamServer struct {
	backend remoteBackend
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	panes   map[string]*streamWatch
	out     chan []byte
	closed  bool
}

// stream serves one client connection: it reads watch requests on one
// goroutine and writes frames on this one, which is the only writer.
func (h *remoteHandler) stream(rw http.ResponseWriter, r *http.Request) {
	// The body keeps being read while frames are written, and a refusal is
	// written without waiting to drain a body the client is still sending:
	// both sides would otherwise wait on each other.
	_ = http.NewResponseController(rw).EnableFullDuplex()
	if r.Method != http.MethodPost {
		writeJSON(rw, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
		return
	}
	token := bearerToken(r)
	if token == "" || !h.authorized(token) {
		writeJSON(rw, http.StatusUnauthorized, map[string]any{"error": "not paired"})
		return
	}
	flusher, ok := rw.(http.Flusher)
	if !ok {
		writeJSON(rw, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}
	rw.Header().Set("Content-Type", "application/x-ndjson")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Accel-Buffering", "no")
	rw.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s := &streamServer{
		backend: h.backend,
		ctx:     ctx,
		cancel:  cancel,
		panes:   map[string]*streamWatch{},
		out:     make(chan []byte, 512),
	}
	go func() {
		defer cancel()
		dec := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<20))
		for {
			var req streamRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			switch req.Op {
			case "watch":
				s.watch(req.Pane, req.Cols, req.Rows, req.Snapshot)
			case "unwatch":
				s.unwatch(req.Pane)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			s.close()
			return
		case b := <-s.out:
			if _, err := rw.Write(b); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *streamServer) watch(pane string, cols, rows int, snapshot bool) {
	if pane == "" {
		return
	}
	s.mu.Lock()
	w := s.panes[pane]
	if w == nil {
		if len(s.panes) >= streamMaxPanes {
			s.mu.Unlock()
			return
		}
		w = &streamWatch{id: pane, snapshot: true, dirty: make(chan struct{}, 1)}
		s.panes[pane] = w
		go s.runPane(pane, w)
	}
	changed := false
	if cols > 0 && rows > 0 && (w.cols != cols || w.rows != rows) {
		w.cols, w.rows = cols, rows
		changed = true
	}
	if snapshot && !w.snapshot {
		w.snapshot = true
		changed = true
	}
	s.mu.Unlock()
	if changed {
		select {
		case w.dirty <- struct{}{}:
		default:
		}
	}
}

// runPane pushes one pane's frames: the program's own writes wake it, so a
// quiet pane asks nothing of the main thread, and the first byte of output
// is a frame instead of waiting for the next look.
func (s *streamServer) runPane(pane string, w *streamWatch) {
	for {
		if s.ctx.Err() != nil {
			return
		}
		ch, cancel := s.backend.remoteWake(pane)
		timer := time.NewTimer(streamIdleCheck)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			cancel()
			return
		case <-w.dirty:
		case <-ch:
		case <-timer.C:
		}
		timer.Stop()
		cancel()
		if !s.frame(pane, w) {
			return
		}
		time.Sleep(streamFrameGap)
	}
}

func (s *streamServer) unwatch(pane string) {
	s.mu.Lock()
	delete(s.panes, pane)
	s.mu.Unlock()
}

func (s *streamServer) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
}

func (s *streamServer) writes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// frame reads one watched pane and writes the frame when it has output, a
// new screen, or a size the client has not been told. It reports false when
// the pane is no longer watched or the stream is done.
func (s *streamServer) frame(pane string, watching *streamWatch) bool {
	s.mu.Lock()
	w := s.panes[pane]
	if w == nil || w != watching || s.closed {
		s.mu.Unlock()
		return false
	}
	params := map[string]any{"pane": pane, "offset": w.offset}
	if w.cols > 0 && w.rows > 0 {
		params["cols"], params["rows"] = w.cols, w.rows
	}
	if w.snapshot {
		params["snapshot"] = true
	}
	s.mu.Unlock()

	resp := s.backend.remoteDispatch(controlRequest{
		ID:     newID(),
		Method: "pane.stream",
		Params: rawParams(params),
	})
	if !resp.OK {
		b, _ := json.Marshal(map[string]any{"pane": pane, "error": resp.Error})
		s.emit(append(b, '\n'))
		s.unwatch(pane)
		return false
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		return true
	}
	data, _ := m["data"].(string)
	snapshot, _ := m["snapshot"].(string)
	reset, _ := m["reset"].(bool)
	cols := intValue(m["cols"])
	rows := intValue(m["rows"])
	viewW := floatValue(m["viewW"])
	viewH := floatValue(m["viewH"])
	title, _ := m["title"].(string)
	cwd, _ := m["cwd"].(string)
	offset := int64Value(m["offset"])

	s.mu.Lock()
	cur := s.panes[pane]
	if cur == nil || cur != watching {
		s.mu.Unlock()
		return false
	}
	cur.offset = offset
	cur.snapshot = false
	changed := !cur.sent || cols != cur.lastCols || rows != cur.lastRows ||
		viewW != cur.lastViewW || viewH != cur.lastViewH ||
		title != cur.lastTitle || cwd != cur.lastCwd
	if changed {
		cur.lastCols, cur.lastRows = cols, rows
		cur.lastViewW, cur.lastViewH = viewW, viewH
		cur.lastTitle, cur.lastCwd = title, cwd
		cur.sent = true
	}
	s.mu.Unlock()
	if data == "" && snapshot == "" && !reset && !changed {
		return true
	}
	b, err := json.Marshal(m)
	if err != nil {
		return true
	}
	s.emit(append(b, '\n'))
	return true
}

func (s *streamServer) emit(b []byte) {
	select {
	case s.out <- b:
		return
	default:
	}
	// A client that cannot keep up would lose the text a frame carries, so
	// every watched pane is told to start over from its screen: the next
	// frame is a snapshot instead of the delta that did not fit.
	s.mu.Lock()
	for _, w := range s.panes {
		w.snapshot = true
	}
	s.mu.Unlock()
	select {
	case s.out <- b:
	case <-s.ctx.Done():
	}
}

// rawParams turns a map into the control request's raw parameters.
func rawParams(kv map[string]any) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(kv))
	for k, v := range kv {
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		out[k] = b
	}
	return out
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func int64Value(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func floatValue(v any) float32 {
	switch n := v.(type) {
	case float32:
		return n
	case float64:
		return float32(n)
	case int:
		return float32(n)
	}
	return 0
}
