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

// mergeViews folds the frames that queued behind the first into it: the
// output of a terminal is a byte stream, so the chunks join in order, and a
// frame that starts the mirror over replaces what came before it.
func mergeViews(a, b remoteStreamView) remoteStreamView {
	if b.Reset {
		// b starts the mirror over, so what a carried is already on b's
		// screen.
		return b
	}
	a.Data += b.Data
	a.Offset = b.Offset
	a.Cols, a.Rows = b.Cols, b.Rows
	a.ViewW, a.ViewH = b.ViewW, b.ViewH
	a.Title, a.Cwd = b.Title, b.Cwd
	return a
}

type remoteStream struct {
	rc    *remoteConn
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	feeds map[string]*remoteFeed
	snaps map[string]bool
	// sent is which panes the host was told about: a watch that could not be
	// written yet goes out on the next tick.
	sent   map[string]bool
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
		snaps: map[string]bool{},
		sent:  map[string]bool{},
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

// watch tells the host this pane is showed: the mirror starts over from the
// host's screen, and nothing about this window's layout goes with it.
func (s *remoteStream) watch(id string, snapshot bool) {
	if id == "" {
		return
	}
	s.mu.Lock()
	snap := s.snaps[id]
	send := !s.sent[id] || (snapshot && !snap)
	w, dead := s.w, s.dead
	s.mu.Unlock()
	if !send || dead || w == nil {
		return
	}
	b, err := json.Marshal(map[string]any{
		"op":       "watch",
		"pane":     id,
		"snapshot": snapshot,
	})
	if err != nil {
		return
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return
	}
	s.mu.Lock()
	s.sent[id] = true
	if snapshot {
		s.snaps[id] = true
	}
	s.mu.Unlock()
}

func (s *remoteStream) unwatch(id string) {
	s.mu.Lock()
	delete(s.snaps, id)
	delete(s.sent, id)
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
	delete(s.sent, id)
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
func (rc *remoteConn) nextPushedView(p *pane, stop chan struct{}, snapshot bool) (remoteStreamView, bool) {
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
	s.watch(p.remote.id, snapshot)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if v, ok := f.pop(); ok {
			// Whatever piled up behind it goes in the same write: one
			// repaint covers the whole burst.
			for {
				next, more := f.pop()
				if !more {
					break
				}
				v = mergeViews(v, next)
			}
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
			s.watch(p.remote.id, false)
		}
	}
}
