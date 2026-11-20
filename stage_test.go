package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

func paneIDs(ps []*pane) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// TestMirrorConnQueuesDeltas: frames are deltas and must arrive in order,
// even when a read takes only part of one.
func TestMirrorConnQueuesDeltas(t *testing.T) {
	c := newMirrorConn()
	c.push([]byte("abc"))
	c.push([]byte("de"))

	buf := make([]byte, 2)
	if n, err := c.Read(buf); err != nil || string(buf[:n]) != "ab" {
		t.Fatalf("first read: %q, %v", buf[:n], err)
	}
	if n, err := c.Read(buf); err != nil || string(buf[:n]) != "c" {
		t.Fatalf("second read: %q, %v", buf[:n], err)
	}
	if n, err := c.Read(buf); err != nil || string(buf[:n]) != "de" {
		t.Fatalf("third read: %q, %v", buf[:n], err)
	}

	_ = c.Close()
	if _, err := c.Read(buf); err != io.EOF {
		t.Fatalf("read after close: %v", err)
	}
}

type probeConn struct{ r *io.PipeReader }

func (c *probeConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *probeConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *probeConn) Close() error                { return c.r.Close() }

func probeTerminal(t *testing.T) (*terminal.Terminal, *io.PipeWriter) {
	t.Helper()
	r, w := io.Pipe()
	term, err := terminal.New(terminal.Options{
		Conn:         &probeConn{r: r},
		ResizeByConn: true,
		Font: terminal.Font{
			Family: "JetBrains Mono Nerd Font Mono, monospace",
			Size:   12,
			Weight: 400,
		},
	})
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	return term, w
}

// TestStageMirrorShowsThePane is the rail's whole premise: a pane's screen,
// mirrored into a second terminal, reads the same there.
func TestStageMirrorShowsThePane(t *testing.T) {
	src, in := probeTerminal(t)
	src.Resize(40, 10)
	if _, err := in.Write([]byte("hello\r\nworld\r\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	p := &pane{ID: "p1", term: src}
	w := &workspace{}
	w.startStageMirror(p)
	t.Cleanup(func() { w.stopStageMirror(p) })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.stageMirror != nil && strings.Contains(p.stageMirror.sink.Text(), "hello") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the mirror never showed the pane: %q", p.stageMirror.sink.Text())
}

// TestStageMirrorSizeOfAPaneNeverLaidOut: a pane a zoomed session restores
// straight into the rail has no view of its own, so its grid at a measured
// cell size stands in. Sized by the card instead, the mirror drew at the
// font's full size and the card showed a magnified crop.
func TestStageMirrorSizeOfAPaneNeverLaidOut(t *testing.T) {
	src, _ := probeTerminal(t)
	src.Resize(80, 24)
	p := &pane{ID: "p1", term: src}
	w := &workspace{paneCellW: 7.5, paneCellH: 19.1}

	gotW, gotH := w.stageMirrorSize(p)
	if wantW := float32(24) + 7.5*float32(80); gotW != wantW {
		t.Fatalf("width %v, want %v", gotW, wantW)
	}
	if wantH := float32(20) + 19.1*float32(24); gotH != wantH {
		t.Fatalf("height %v, want %v", gotH, wantH)
	}

	// A pane whose view has been laid out keeps the size that view had.
	p.viewW, p.viewH = 569, 784.5
	if gotW, gotH := w.stageMirrorSize(p); gotW != 569 || gotH != 784.5 {
		t.Fatalf("laid-out pane sized %vx%v", gotW, gotH)
	}

	// With no cell measured yet the fallback is the card, never zero.
	if gotW, gotH := (&workspace{}).stageMirrorSize(p); gotW <= 0 || gotH <= 0 {
		t.Fatalf("unmeasured pane sized %vx%v", gotW, gotH)
	}
}

// TestStageRailPanesFollowTheSplit keeps the cards in the layout's order and
// leaves the stage out; a single pane has no rail at all.
func TestStageRailPanesFollowTheSplit(t *testing.T) {
	a, b, c := &pane{ID: "a"}, &pane{ID: "b"}, &pane{ID: "c"}
	s := &session{ID: "s", Root: &splitNode{
		axis:   axisHorizontal,
		ratio:  50,
		first:  leaf(a),
		second: &splitNode{axis: axisVertical, ratio: 50, first: leaf(b), second: leaf(c)},
	}}
	w := &workspace{}
	if got := w.stageRailPanes(s); got != nil {
		t.Fatalf("unzoomed rail: %v", paneIDs(got))
	}

	s.zoomedPane = "b"
	got := w.stageRailPanes(s)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("rail panes: %v", paneIDs(got))
	}

	one := &session{ID: "one", Root: leaf(a)}
	one.zoomedPane = "a"
	if got := w.stageRailPanes(one); got != nil {
		t.Fatalf("a lone pane has a rail: %v", paneIDs(got))
	}
}

// TestStagePaneSwapsTheStage: a card takes the stage and the pane it replaces
// joins the rail, focus included.
func TestStagePaneSwapsTheStage(t *testing.T) {
	a, b := &pane{ID: "a"}, &pane{ID: "b"}
	s := &session{ID: "s", Root: &splitNode{
		axis: axisHorizontal, ratio: 50, first: leaf(a), second: leaf(b),
	}}
	s.zoomedPane = "a"
	w := &workspace{
		Projects:    []*project{{Sessions: []*session{s}}},
		FocusedPane: "a",
	}

	w.stagePane(b)
	if s.zoomedPane != "b" || w.FocusedPane != "b" || w.focusReq != "b" {
		t.Fatalf("staged %q, focus %q/%q", s.zoomedPane, w.FocusedPane, w.focusReq)
	}
	got := w.stageRailPanes(s)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("rail after the swap: %v", paneIDs(got))
	}
}

// TestStageRailHidesUnderTheDrawer: both live on the right edge, and the
// drawer is the one that covers it.
func TestStageRailHidesUnderTheDrawer(t *testing.T) {
	a, b := &pane{ID: "a"}, &pane{ID: "b"}
	s := &session{ID: "s", Root: &splitNode{
		axis: axisHorizontal, ratio: 50, first: leaf(a), second: leaf(b),
	}}
	s.zoomedPane = "a"
	w := &workspace{}
	if !w.stageRailVisible(s) {
		t.Fatal("the rail is hidden while idle")
	}
	w.drawer.open = true
	if w.stageRailVisible(s) {
		t.Fatal("the rail draws under the open drawer")
	}
}
