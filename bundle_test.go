package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.0.88", "0.0.85", true},
		{"0.0.85", "0.0.88", false},
		{"0.0.88", "0.0.88", false},
		{"0.0.10", "0.0.9", true},
		{"v0.1.0", "0.0.99", true},
		{"1.0.0", "1.0.0-beta.1", true},
		{"1.0.0-beta.1", "1.0.0", false},
		{"", "0.0.1", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Fatalf("versionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestBundleVersion(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "gexi.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>gexi</string>
	<key>CFBundleShortVersionString</key>
	<string>0.0.88</string>
	<key>CFBundleVersion</key>
	<string>88</string>
</dict>
</plist>
`
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bundle, "Contents", "MacOS", "gexi")
	if got := bundleVersion(exe); got != "0.0.88" {
		t.Fatalf("bundleVersion = %q, want 0.0.88", got)
	}
	if got := bundleVersion(filepath.Join(root, "elsewhere", "gexi")); got != "" {
		t.Fatalf("bundleVersion without a bundle = %q, want empty", got)
	}
	// The key is taken, not the next string that happens to follow.
	if got := plistValue([]byte(plist), "CFBundleVersion"); got != "88" {
		t.Fatalf("plistValue(CFBundleVersion) = %q, want 88", got)
	}
}
