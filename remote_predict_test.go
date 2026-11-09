package main

import (
	"strings"
	"testing"
	"time"
)

// The prediction is the text drawn ahead of the host: it must cover what a
// program echoes, and nothing that moves the screen.
func TestPredictText(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"a", true},
		{"hello ", true},
		{"你好", true},
		{"\x1b[A", false},
		{"a\r", false},
		{"\t", false},
		{"\x7f", false},
		{"", false},
		{strings.Repeat("x", remotePredictionLimit+1), false},
	}
	for _, c := range cases {
		if got := predictText([]byte(c.in)); got != c.want {
			t.Errorf("predictText(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPredictionShowsAtTheCursorUntilItMoves(t *testing.T) {
	p := &remotePaneLink{}
	p.cursorRow.Store(5)
	p.cursorCol.Store(12)

	if !p.addPrediction([]byte("ab")) {
		t.Fatal("printable keys should be predicted")
	}
	text, row, col := p.prediction()
	if text != "ab" || row != 5 || col != 12 {
		t.Fatalf("prediction = %q at %d,%d, want %q at the cursor", text, row, col, "ab")
	}

	// The host echoed the keys: its cursor moved, so the text is no longer
	// ahead of it and must not be drawn twice.
	p.cursorCol.Store(14)
	if text, _, _ := p.prediction(); text != "" {
		t.Fatalf("prediction %q survived the cursor moving, want it cleared", text)
	}
}

func TestPredictionEndsOnAControlKey(t *testing.T) {
	p := &remotePaneLink{}
	p.addPrediction([]byte("abc"))
	if !p.clearPrediction() {
		t.Fatal("there should have been a prediction to clear")
	}
	if text, _, _ := p.prediction(); text != "" {
		t.Fatalf("prediction %q after clear, want none", text)
	}
	if p.clearPrediction() {
		t.Fatal("clearing twice should report there was nothing left")
	}

	p.addPrediction([]byte("abc"))
	p.addPrediction([]byte("\r"))
	if text, _, _ := p.prediction(); text != "" {
		t.Fatalf("Enter left prediction %q, want none", text)
	}
}

func TestPredictionExpires(t *testing.T) {
	p := &remotePaneLink{}
	p.addPrediction([]byte("abc"))
	p.pendingMu.Lock()
	p.pendingAt = time.Now().Add(-remotePredictionTTL - time.Second)
	p.pendingMu.Unlock()
	if text, _, _ := p.prediction(); text != "" {
		t.Fatalf("stale prediction %q, want none", text)
	}
}
