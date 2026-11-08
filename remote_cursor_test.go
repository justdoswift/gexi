package main

import "testing"

// A shell writes its prompt at the top of an empty screen, so a card showing
// the bottom of the host's screen would show nothing at all.
func TestStreamCursorShellPrompt(t *testing.T) {
	var c streamCursor
	c.start(0, 0, 41, 168, false)
	c.feed([]byte("gepeng@mac tmp % "))
	if c.low != 0 {
		t.Fatalf("low = %d, want 0", c.low)
	}
	c.feed([]byte("echo hi\r\nhi\r\ngepeng@mac tmp % "))
	if c.low != 2 {
		t.Fatalf("low = %d, want 2", c.low)
	}
}

// A full-screen program's screen is drawn all the way down, and the shell's
// screen and cursor come back when it leaves.
func TestStreamCursorAltScreenRestores(t *testing.T) {
	var c streamCursor
	c.start(1, 10, 41, 168, false)
	c.feed([]byte("\x1b[?1049h"))
	if c.low != 40 {
		t.Fatalf("alt screen low = %d, want 40", c.low)
	}
	c.feed([]byte("\x1b[?1049l"))
	if c.row != 1 || c.col != 10 || c.low != 0 {
		t.Fatalf("after alt row=%d col=%d low=%d, want 1,10,0", c.row, c.col, c.low)
	}
}

func TestStreamCursorFullScreen(t *testing.T) {
	var c streamCursor
	c.start(38, 2, 41, 168, true)
	if c.low != 40 {
		t.Fatalf("alt screen low = %d, want 40", c.low)
	}
}

func TestStreamCursorMoves(t *testing.T) {
	var c streamCursor
	c.start(0, 0, 41, 168, false)
	c.feed([]byte("\x1b[10;1Htext"))
	if c.low != 9 {
		t.Fatalf("after CUP low = %d, want 9", c.low)
	}
	c.feed([]byte("\x1b[2J$ "))
	if c.low != 0 {
		t.Fatalf("after clear low = %d, want 0", c.low)
	}
}

// A long line wraps, and writing past the bottom scrolls, keeping the
// lowest row where it was.
func TestStreamCursorWrapAndScroll(t *testing.T) {
	var c streamCursor
	c.start(0, 0, 5, 10, false)
	c.feed([]byte("0123456789abc"))
	if c.row != 1 || c.low != 1 {
		t.Fatalf("after wrap row=%d low=%d, want 1,1", c.row, c.low)
	}
	c.feed([]byte("\r\n\r\n\r\n\r\n\r\n"))
	if c.row != 4 || c.low != 1 {
		t.Fatalf("after scroll row=%d low=%d, want 4,1", c.row, c.low)
	}
}

// The snapshot the host sends ends with the cursor's placement, and names the
// alternate screen when a full-screen program shows one.
func TestSnapshotCursor(t *testing.T) {
	snap := []byte("\x1b[?2004h\x1b[Hgepeng@mac tmp % \x1b[1;34H\x1b[0m")
	row, col, alt := snapshotCursor(snap)
	if row != 0 || col != 33 || alt {
		t.Fatalf("got %d,%d,%v want 0,33,false", row, col, alt)
	}
	snap = []byte("\x1b[?1049h\x1b[H\x1b[0m  input\x1b[38;3H\x1b[0m")
	row, col, alt = snapshotCursor(snap)
	if row != 37 || col != 2 || !alt {
		t.Fatalf("got %d,%d,%v want 37,2,true", row, col, alt)
	}
}
