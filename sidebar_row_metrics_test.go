package main

import (
	"os"
	"strings"
	"testing"
)

// TestSidebarRowsShareTheirMetrics: a row of the sidebar is built from the
// same metrics wherever it is, so the column reads as one list.
//
// Each site wrote its own numbers and they drifted: a header was 28 tall with
// a gap of 5 and no hairline while a session row was 32 with a gap of 8 and
// one, the fold's rows kept the 6pt card while a session row is a capsule, and
// a session's name was 13 where a header's was 11.5. Nothing lined up.
//
// The guard is a source check rather than a render check: the headless
// renderer draws no material, so the thing to hold is that every builder asks
// the shared constants instead of writing a number of its own.
func TestSidebarRowsShareTheirMetrics(t *testing.T) {
	// The functions that build a row of the sidebar, and the file each is in.
	sites := []struct{ file, fn string }{
		{"ui.go", "func (w *workspace) projectRow("},
		{"ui.go", "func (w *workspace) remoteProjectRow("},
		{"ui.go", "func (w *workspace) sessionRow("},
		{"session_state.go", "func (w *workspace) idleSessionSection("},
		{"session_state.go", "func (w *workspace) idleSessionRow("},
	}

	mustUse := []string{
		"sidebarRowRadius()",
		"MarginY(rowMarginY)",
		"Border(1, ui.Transparent)",
		"sidebarRowFont",
	}
	heights := []string{"sidebarRowHeight", "sidebarHeaderHeight"}
	gaps := []string{"sidebarRowGap", "sidebarHeaderGap"}
	insets := []string{"sidebarRowInset", "sidebarRowEnd"}

	for _, site := range sites {
		data, err := os.ReadFile(site.file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		at := strings.Index(src, site.fn)
		if at < 0 {
			t.Errorf("%s: no %s", site.file, site.fn)
			continue
		}
		// The function body: from its opening brace to the line that closes
		// it at the same indent.
		rest := src[at:]
		end := strings.Index(rest, "\n}\n")
		if end < 0 {
			end = len(rest)
		}
		body := rest[:end]

		where := site.file + " " + strings.TrimPrefix(strings.TrimSuffix(site.fn, "("), "func (w *workspace) ")
		for _, want := range mustUse {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %s", where, want)
			}
		}
		if !containsAny(body, heights) {
			t.Errorf("%s: names no shared height", where)
		}
		if !containsAny(body, gaps) {
			t.Errorf("%s: names no shared gap", where)
		}
		if !containsAny(body, insets) {
			t.Errorf("%s: names no shared inset", where)
		}
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
