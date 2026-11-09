package main

import (
	"testing"
	"time"
)

// TestWaitOutputWakesOnWrite: a mirror parked on a pane is released by the
// next chunk the program writes, which is what turns a long poll into a
// push.
func TestWaitOutputWakesOnWrite(t *testing.T) {
	c := newPTYConn()
	wake, cancel := c.waitOutput()
	defer cancel()

	c.cutMu.Lock()
	c.log([]byte("hello"))
	c.cutMu.Unlock()

	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("a write did not wake the parked mirror")
	}
}

// TestWaitOutputCancelDropsTheWaiter: a poll that leaves before any output
// must not leave a channel behind, and a later write must not panic on it.
func TestWaitOutputCancelDropsTheWaiter(t *testing.T) {
	c := newPTYConn()
	_, cancel := c.waitOutput()
	cancel()

	c.cutMu.Lock()
	c.log([]byte("hello"))
	c.cutMu.Unlock()

	c.waitMu.Lock()
	left := len(c.waiters)
	c.waitMu.Unlock()
	if left != 0 {
		t.Fatalf("%d waiters left behind", left)
	}
}

func TestStreamIdle(t *testing.T) {
	cases := []struct {
		name   string
		result any
		idle   bool
	}{
		{"nothing", map[string]any{"data": "", "offset": 12.0}, true},
		{"output", map[string]any{"data": "aGk="}, false},
		{"reset", map[string]any{"data": "", "reset": true}, false},
		{"not a frame", "nope", false},
	}
	for _, c := range cases {
		if got := streamIdle(c.result); got != c.idle {
			t.Errorf("%s: streamIdle = %v, want %v", c.name, got, c.idle)
		}
	}
}
