package main

import (
	"strings"
	"testing"
)

// The interface starts on the system's font, which has the weights the
// chrome asks for, and follows its own setting rather than the terminal's.
func TestInterfaceFontFollowsSettings(t *testing.T) {
	w := &workspace{settings: defaultSettings()}
	if got := w.interfaceFont(); got != "" {
		t.Fatalf("default interface font is %q, want the system's", got)
	}
	w.settings.TerminalFont = "Menlo"
	if got := w.interfaceFont(); got != "" {
		t.Fatalf("terminal font changed the interface to %q", got)
	}
	w.settings.InterfaceFont = "PingFang SC"
	if got := w.interfaceFont(); got != "PingFang SC" {
		t.Fatalf("interface font is %q, want PingFang SC", got)
	}
}

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
	if !strings.Contains(f.Family, "PingFang SC") {
		t.Fatalf("family is %q, want a Chinese sans before the system's serif fallback", f.Family)
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
