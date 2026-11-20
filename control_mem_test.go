package main

import (
	"testing"
)

// TestMemStatsReportsWhatTheProcessHolds: the memory report answers with the
// runtime's own numbers and the window's counts.
//
// It exists because a memory reading of the app is mostly things the app does
// not own: three Metal drawables at 17 MB each, the GPU accelerator, and the
// frameworks AppKit maps in. Without a way to ask what the Go heap holds, the
// only number available is the one that cannot be moved.
func TestMemStatsReportsWhatTheProcessHolds(t *testing.T) {
	w := &workspace{InstanceID: "test", settings: defaultSettings()}
	got, err := w.dispatch(controlRequest{Method: "mem.stats"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("mem.stats answered %T", got)
	}
	for _, key := range []string{
		"heapAllocMB", "heapInuseMB", "heapIdleMB", "heapSysMB",
		"stackMB", "sysMB", "numGC", "numGoroutine",
		"panes", "livePanes", "mirrors", "sessions",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("mem.stats has no %q", key)
		}
	}
	// The heap the runtime has taken cannot be smaller than what is live in
	// it, and neither can be negative.
	alloc, _ := m["heapAllocMB"].(float64)
	sys, _ := m["heapSysMB"].(float64)
	if alloc < 0 || sys < 0 {
		t.Fatalf("negative heap: alloc %v sys %v", alloc, sys)
	}
	if sys < alloc {
		t.Fatalf("heap sys %v below heap alloc %v", sys, alloc)
	}
	if n, _ := m["numGoroutine"].(int); n < 1 {
		t.Fatalf("goroutines: %v", n)
	}
}
