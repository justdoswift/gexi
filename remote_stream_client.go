package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// The pushed mirror: one connection carries every watched pane's frames, so
// a pane's output reaches this Mac as soon as the host has it instead of
// waiting for this side's next request to travel there. When the connection
// is down the loops fall back to polling, so an old host or a broken stream
// still shows the screen.

var remoteStreamClient = &http.Client{}

var errStreamClosed = errors.New("the remote stream closed")

type remoteStream struct {
	rc     *remoteConn
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	feeds  map[string]*remoteFeed
	sizes  map[string][2]int
	snaps  map[string]bool
	w      *io.PipeWriter
	cancel context.CancelFunc
	dead   bool
}

// remoteFeed queues the frames of one pane in order: a frame carries the
// output since the last one, so a dropped frame would lose text.
type remoteFeed struct {
	mu  sync.Mutex
	q   []remoteStreamView
	sig chan struct{}
}

func (f *remoteFeed) push(v remoteStreamView) {
	f.mu.Lock()
	f.q = append(f.q, v)
	f.mu.Unlock()
	select {
	case f.sig <- struct{}{}:
	default:
	}
}

func (f *remoteFeed) pop() (remoteStreamView, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.q) == 0 {
		return remoteStreamView{}, false
	}
	v := f.q[0]
	f.q = f.q[1:]
	return v, true
}

// streamSession returns the open stream, dialing a new one when the last is
// gone. A failure is not retried for a moment: a host without the endpoint,
// or a link that is down, should not cost a request per frame.
func (rc *remoteConn) streamSession() *remoteStream {
	rc.streamMu.Lock()
	defer rc.streamMu.Unlock()
	if rc.streamConn != nil && !rc.streamConn.isDead() {
		return rc.streamConn
	}
	if time.Now().Before(rc.streamRetry) {
		return nil
	}
	rc.streamRetry = time.Now().Add(3 * time.Second)
	s := &remoteStream{
		rc:    rc,
		done:  make(chan struct{}),
		feeds: map[string]*remoteFeed{},
		sizes: map[string][2]int{},
		snaps: map[string]bool{},
	}
	rc.streamConn = s
	go s.run()
	return s
}

func (s *remoteStream) isDead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dead
}

func (s *remoteStream) run() {
	pr, pw := io.Pipe()
	s.mu.Lock()
	s.w = pw
	s.mu.Unlock()
	client := s.rc.client
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel = cancel
	dead := s.dead
	s.mu.Unlock()
	if dead {
		cancel()
		_ = pr.CloseWithError(errStreamClosed)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.scheme()+client.Address+"/api/stream", pr)
	if err != nil {
		s.fail(err)
		return
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("Authorization", "Bearer "+client.Token)
	resp, err := remoteStreamClient.Do(req)
	if err != nil {
		s.fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.fail(fmt.Errorf("remote: %s", resp.Status))
		return
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var v remoteStreamView
		if err := dec.Decode(&v); err != nil {
			s.fail(err)
			return
		}
		if v.Pane == "" {
			// A beat: the stream is alive, the screen is not saying anything.
			continue
		}
		s.deliver(v)
	}
}

func (s *remoteStream) deliver(v remoteStreamView) {
	s.mu.Lock()
	f := s.feeds[v.Pane]
	s.mu.Unlock()
	if f != nil {
		f.push(v)
	}
}

func (s *remoteStream) feed(id string) *remoteFeed {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.feeds[id]
	if f == nil {
		f = &remoteFeed{sig: make(chan struct{}, 1)}
		s.feeds[id] = f
	}
	return f
}

// watch tells the host this pane is showed at cols by rows. The host is told
// once per change, not once per frame; a watch that could not be written yet
// is sent on the next tick.
func (s *remoteStream) watch(id string, cols, rows int, snapshot bool) {
	if id == "" || cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	last, known := s.sizes[id]
	snap := s.snaps[id]
	send := !known || last != [2]int{cols, rows} || (snapshot && !snap)
	w, dead := s.w, s.dead
	s.mu.Unlock()
	if !send || dead || w == nil {
		return
	}
	b, err := json.Marshal(map[string]any{
		"op":       "watch",
		"pane":     id,
		"cols":     cols,
		"rows":     rows,
		"snapshot": snapshot,
	})
	if err != nil {
		return
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return
	}
	s.mu.Lock()
	s.sizes[id] = [2]int{cols, rows}
	if snapshot {
		s.snaps[id] = true
	}
	s.mu.Unlock()
}

func (s *remoteStream) unwatch(id string) {
	s.mu.Lock()
	delete(s.sizes, id)
	delete(s.snaps, id)
	w, dead := s.w, s.dead
	s.mu.Unlock()
	if dead || w == nil || id == "" {
		return
	}
	b, _ := json.Marshal(map[string]any{"op": "unwatch", "pane": id})
	_, _ = w.Write(append(b, '\n'))
}

// confirmed clears the snapshot ask once a reset frame has come back: the
// pane's mirror has the whole screen now.
func (s *remoteStream) confirmed(id string, reset bool) {
	if !reset {
		return
	}
	s.mu.Lock()
	delete(s.snaps, id)
	s.mu.Unlock()
}

func (s *remoteStream) fail(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.dead = true
		w := s.w
		cancel := s.cancel
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if w != nil {
			_ = w.CloseWithError(err)
		}
		close(s.done)
		s.rc.dropStream(s)
	})
}

func (rc *remoteConn) dropStream(s *remoteStream) {
	rc.streamMu.Lock()
	if rc.streamConn == s {
		rc.streamConn = nil
	}
	rc.streamMu.Unlock()
}

// nextPushedView waits for the host's next frame for the pane. It reports
// false when the stream is not up, and the caller polls instead.
func (rc *remoteConn) nextPushedView(p *pane, stop chan struct{}, cols, rows int, snapshot bool) (remoteStreamView, bool) {
	s := rc.streamSession()
	if s == nil {
		return remoteStreamView{}, false
	}
	f := s.feed(p.remote.id)
	if snapshot {
		// A new mirror starts over from the host's screen.
		s.mu.Lock()
		delete(s.snaps, p.remote.id)
		s.mu.Unlock()
	}
	s.watch(p.remote.id, cols, rows, snapshot)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if v, ok := f.pop(); ok {
			s.confirmed(p.remote.id, v.Reset)
			return v, true
		}
		select {
		case <-stop:
			return remoteStreamView{}, false
		case <-rc.stop:
			return remoteStreamView{}, false
		case <-s.done:
			return remoteStreamView{}, false
		case <-f.sig:
		case <-tick.C:
			// A pane that was resized while quiet still needs its new grid
			// on the host.
			c, r := int(p.remote.wantCols.Load()), int(p.remote.wantRows.Load())
			if c != cols || r != rows {
				cols, rows = c, r
			}
			s.watch(p.remote.id, cols, rows, false)
		}
	}
}
