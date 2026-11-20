package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSidebarRowsAllWearTheMaterial: every row a pointer can land on in the
// sidebar takes the system's material, not a flat fill.
//
// The session rows were converted first and the rest were missed, so the idle
// fold's header and its rows, the search results and the drawer's lists stayed
// opaque blocks in a list made of glass. This reads the sources and fails on a
// hover fill that skips the material, which is the shape the miss took.
func TestSidebarRowsAllWearTheMaterial(t *testing.T) {
	// The flat fills a hovered row may fall back to when the window is not
	// glass. Each must sit inside a liquid(c) branch.
	flat := []string{
		"row.Background(tk.Active)",
		"row.Background(tk.Foreground.Alpha(0.045))",
		"row.Background(tk.Raised)",
	}
	files := []string{"ui.go", "session_state.go", "drawer.go"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, fill := range flat {
			for at := 0; ; {
				i := strings.Index(src[at:], fill)
				if i < 0 {
					break
				}
				i += at
				at = i + len(fill)
				// The lines just before the fill decide whether it is
				// guarded: a hover block that names the material first.
				start := i - 400
				if start < 0 {
					start = 0
				}
				before := src[start:i]
				if strings.Contains(before, "liquid(c)") && strings.Contains(before, "rowSkin(") {
					continue
				}
				line := 1 + strings.Count(src[:i], "\n")
				t.Errorf("%s:%d: %s with no material beside it", name, line, fill)
			}
		}
	}
}
