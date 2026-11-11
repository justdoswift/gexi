package main

import (
	"strings"
	"testing"
)

// The terminal takes the family and size the settings name, and the
// embedded font at 12 until the user picks others.
func TestTerminalFontFollowsSettings(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	f := w.terminalTextFont()
	if f.Size != terminalFontSizeDefault {
		t.Fatalf("default size is %v, want %v", f.Size, terminalFontSizeDefault)
	}
	if !strings.HasPrefix(f.Family, terminalFont+",") {
		t.Fatalf("default family is %q, want the embedded one first", f.Family)
	}

	w.settings.TerminalFont = "Menlo"
	w.settings.TerminalFontSize = 16
	f = w.terminalTextFont()
	if f.Size != 16 {
		t.Fatalf("size is %v, want 16", f.Size)
	}
	if !strings.HasPrefix(f.Family, "Menlo,") {
		t.Fatalf("family is %q, want Menlo first with fallbacks", f.Family)
	}
}
