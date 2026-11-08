package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// streamCursor follows the cursor through the output a remote mirror shows,
// and keeps the lowest row that has text on it: a shell writes its prompt at
// the top of an empty screen, while a full-screen program draws its input row
// at the bottom, and a card smaller than the host's screen has to know which
// of the two is the row the user watches.
type streamCursor struct {
	row, col   int
	low        int
	rows, cols int
	savedRow   int
	savedCol   int
	// altRow and altCol are the main screen's cursor while a full-screen
	// program shows the alternate one, which comes back when it leaves.
	altRow int
	altCol int
	alt    bool
	wrap   bool
}

// start places the cursor where the host's snapshot leaves it, at the size the
// stream names. A full-screen program's screen is drawn all the way down; a
// shell's may have one line at the top.
func (c *streamCursor) start(row, col, rows, cols int, alt bool) {
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	c.rows, c.cols = rows, cols
	c.alt = alt
	c.row = clampInt(row, 0, rows-1)
	c.col = clampInt(col, 0, cols-1)
	c.wrap = false
	if alt {
		c.low = rows - 1
		c.altRow, c.altCol = 0, 0
	} else {
		c.low = c.row
	}
}

// resize keeps what the cursor knows of the screen while the host's size
// changes under it.
func (c *streamCursor) resize(rows, cols int) {
	if rows < 1 || cols < 1 {
		return
	}
	c.rows, c.cols = rows, cols
	c.row = clampInt(c.row, 0, rows-1)
	c.col = clampInt(c.col, 0, cols-1)
	c.low = clampInt(c.low, 0, rows-1)
}

func (c *streamCursor) note(row int) {
	if row > c.low {
		c.low = row
	}
}

func (c *streamCursor) down() {
	c.wrap = false
	if c.row < c.rows-1 {
		c.row++
	}
}

// print writes a character of width w at the cursor, wrapping as the screen
// does when the row runs out.
func (c *streamCursor) print(w int) {
	if c.cols < 1 || c.rows < 1 {
		return
	}
	if c.wrap {
		c.wrap = false
		c.row = clampInt(c.row+1, 0, c.rows-1)
		c.col = 0
	}
	if w > c.cols {
		w = c.cols
	}
	if c.col+w > c.cols {
		c.note(c.row)
		c.row = clampInt(c.row+1, 0, c.rows-1)
		c.col = 0
	}
	c.note(c.row)
	c.col += w
	if c.col >= c.cols {
		// The screen holds the wrap until the next character needs it.
		c.col = c.cols - 1
		c.wrap = true
	}
}

// feed follows the output from where start left the cursor.
func (c *streamCursor) feed(b []byte) {
	for i := 0; i < len(b); {
		ch := b[i]
		switch {
		case ch == 0x1b:
			n := c.escape(b[i:])
			if n <= 0 {
				return
			}
			i += n
		case ch == '\r':
			c.col = 0
			c.wrap = false
			i++
		case ch == '\n':
			c.down()
			i++
		case ch == '\b':
			if c.col > 0 {
				c.col--
			}
			c.wrap = false
			i++
		case ch == '\t':
			c.col = clampInt((c.col/8+1)*8, 0, c.cols-1)
			c.wrap = false
			i++
		case ch < 0x20 || ch == 0x7f:
			i++
		default:
			r, n := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && n == 1 {
				i++
				continue
			}
			w := 1
			if isWideRune(r) {
				w = 2
			}
			c.print(w)
			i += n
		}
	}
}

// escape reads the escape sequence at the start of b and reports how many
// bytes it took.
func (c *streamCursor) escape(b []byte) int {
	if len(b) < 2 {
		return len(b)
	}
	switch b[1] {
	case '[':
		i := 2
		private := false
		if i < len(b) && (b[i] == '?' || b[i] == '>' || b[i] == '<' || b[i] == '=') {
			private = b[i] == '?'
			i++
		}
		start := i
		for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
			i++
		}
		if i >= len(b) {
			return len(b)
		}
		c.csi(b[i], parseCSI(b[start:i]), private)
		return i + 1
	case ']', 'P', '^', '_':
		// OSC, DCS, PM and APC run to the bell or the string terminator.
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				return i + 1
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
		}
		return len(b)
	case '(', ')', '*', '+':
		if len(b) < 3 {
			return len(b)
		}
		return 3
	case '7':
		c.savedRow, c.savedCol = c.row, c.col
		return 2
	case '8':
		c.row, c.col = c.savedRow, c.savedCol
		c.wrap = false
		return 2
	case 'D':
		c.down()
		return 2
	case 'M':
		if c.row > 0 {
			c.row--
		}
		c.wrap = false
		return 2
	case 'E':
		c.col = 0
		c.down()
		return 2
	case 'c':
		c.row, c.col, c.low = 0, 0, 0
		return 2
	}
	return 2
}

func (c *streamCursor) csi(final byte, p []int, private bool) {
	n := 1
	if len(p) > 0 && p[0] > 0 {
		n = p[0]
	}
	switch final {
	case 'H', 'f':
		row, col := 1, 1
		if len(p) > 0 && p[0] > 0 {
			row = p[0]
		}
		if len(p) > 1 && p[1] > 0 {
			col = p[1]
		}
		c.row = clampInt(row-1, 0, c.rows-1)
		c.col = clampInt(col-1, 0, c.cols-1)
		c.wrap = false
	case 'A':
		c.row = clampInt(c.row-n, 0, c.rows-1)
	case 'B', 'e':
		c.row = clampInt(c.row+n, 0, c.rows-1)
	case 'C', 'a':
		c.col = clampInt(c.col+n, 0, c.cols-1)
	case 'D':
		c.col = clampInt(c.col-n, 0, c.cols-1)
	case 'E':
		c.row = clampInt(c.row+n, 0, c.rows-1)
		c.col = 0
	case 'F':
		c.row = clampInt(c.row-n, 0, c.rows-1)
		c.col = 0
	case 'G', '`':
		c.col = clampInt(n-1, 0, c.cols-1)
	case 'd':
		c.row = clampInt(n-1, 0, c.rows-1)
	case 'J':
		if len(p) > 0 && p[0] == 2 {
			c.row, c.col, c.low = 0, 0, 0
			c.wrap = false
		}
	case 'h', 'l':
		if !private {
			return
		}
		for _, m := range p {
			if m != 1049 && m != 1047 && m != 47 {
				continue
			}
			on := final == 'h'
			if on == c.alt {
				continue
			}
			c.alt = on
			c.wrap = false
			if on {
				c.altRow, c.altCol = c.row, c.col
				c.row, c.col = 0, 0
				c.low = c.rows - 1
			} else {
				c.row = clampInt(c.altRow, 0, c.rows-1)
				c.col = clampInt(c.altCol, 0, c.cols-1)
				c.low = 0
			}
		}
	}
}

// parseCSI reads the numeric parameters of a CSI sequence, with an empty
// parameter counting as zero, which every caller reads as its default.
func parseCSI(b []byte) []int {
	if len(b) == 0 {
		return nil
	}
	parts := strings.Split(string(b), ";")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, _ := strconv.Atoi(part)
		out = append(out, n)
	}
	return out
}

// snapshotCursor reads the cursor the host's snapshot ends at, and whether
// the screen it starts over has the alternate screen: the formatter writes
// the cursor last, and padRows keeps it there.
func snapshotCursor(b []byte) (row, col int, alt bool) {
	alt = strings.Contains(string(b), "\x1b[?1049h") ||
		strings.Contains(string(b), "\x1b[?1047h") ||
		strings.Contains(string(b), "\x1b[?47h")
	if r, cc, ok := lastCUP(b); ok {
		row, col = r, cc
	}
	return row, col, alt
}

// lastCUP finds the last absolute cursor placement in a VT snapshot.
func lastCUP(b []byte) (row, col int, ok bool) {
	for i := len(b) - 1; i >= 2; i-- {
		if b[i] != 'H' && b[i] != 'f' {
			continue
		}
		j := i - 1
		for j >= 2 && (b[j] >= '0' && b[j] <= '9' || b[j] == ';') {
			j--
		}
		if j < 1 || b[j] != '[' || b[j-1] != 0x1b {
			continue
		}
		r, c := 1, 1
		params := strings.SplitN(string(b[j+1:i]), ";", 3)
		if len(params) > 0 && params[0] != "" {
			r, _ = strconv.Atoi(params[0])
		}
		if len(params) > 1 && params[1] != "" {
			c, _ = strconv.Atoi(params[1])
		}
		if r < 1 {
			r = 1
		}
		if c < 1 {
			c = 1
		}
		return r - 1, c - 1, true
	}
	return 0, 0, false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// isWideRune reports whether a rune takes two cells, as East Asian text and
// the emoji a terminal draws wide do.
func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}
