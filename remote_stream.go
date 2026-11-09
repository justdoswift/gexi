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
	// streamTick is how often the watched panes are read: the terminal may
	// have output any moment, and a frame is only written when it does.
	streamTick = 16 * time.Millisecond
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
		panes:   map[string]*streamWatch{},
		out:     make(chan []byte, 512),
	}
	go s.pump(ctx)
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
	defer s.mu.Unlock()
	w := s.panes[pane]
	if w == nil {
		if len(s.panes) >= streamMaxPanes {
			return
		}
		w = &streamWatch{id: pane, snapshot: true}
		s.panes[pane] = w
	}
	if cols > 0 && rows > 0 {
		w.cols, w.rows = cols, rows
	}
	if snapshot {
		w.snapshot = true
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
}

func (s *streamServer) writes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// watches copies the watched panes so a read does not hold the lock the
// client's watch requests need.
func (s *streamServer) watches() []*streamWatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*streamWatch, 0, len(s.panes))
	for _, w := range s.panes {
		c := *w
		out = append(out, &c)
	}
	return out
}

func (s *streamServer) pump(ctx context.Context) {
	tick := time.NewTicker(streamTick)
	defer tick.Stop()
	beat := time.NewTicker(streamBeat)
	defer beat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-beat.C:
			s.emit([]byte("{\"beat\":true}\n"))
		case <-tick.C:
			if !s.writes() {
				return
			}
			ws := s.watches()
			for _, w := range ws {
				s.frame(w)
			}
		}
	}
}

// frame reads one watched pane and writes the frame when it has output, a
// new screen, or a size the client has not been told.
func (s *streamServer) frame(w *streamWatch) {
	params := map[string]any{"pane": w.id, "offset": w.offset}
	if w.cols > 0 && w.rows > 0 {
		params["cols"], params["rows"] = w.cols, w.rows
	}
	if w.snapshot {
		params["snapshot"] = true
	}
	resp := s.backend.remoteDispatch(controlRequest{
		ID:     newID(),
		Method: "pane.stream",
		Params: rawParams(params),
	})
	if !resp.OK {
		b, _ := json.Marshal(map[string]any{"pane": w.id, "error": resp.Error})
		s.emit(append(b, '\n'))
		s.unwatch(w.id)
		return
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		return
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
	cur := s.panes[w.id]
	if cur != nil {
		cur.offset = offset
		cur.snapshot = false
	}
	changed := cur != nil && (!cur.sent || cols != cur.lastCols || rows != cur.lastRows ||
		viewW != cur.lastViewW || viewH != cur.lastViewH ||
		title != cur.lastTitle || cwd != cur.lastCwd)
	if changed {
		cur.lastCols, cur.lastRows = cols, rows
		cur.lastViewW, cur.lastViewH = viewW, viewH
		cur.lastTitle, cur.lastCwd = title, cwd
		cur.sent = true
	}
	s.mu.Unlock()
	if data == "" && snapshot == "" && !reset && !changed {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	s.emit(append(b, '\n'))
}

func (s *streamServer) emit(b []byte) {
	select {
	case s.out <- b:
	default:
		// A client that cannot keep up loses frames, not the link: the
		// next one carries an offset it can start over from.
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
