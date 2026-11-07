package main

import "testing"

func TestFileIconName(t *testing.T) {
	cases := map[string]string{
		"main.go":        "file-code",
		"App.tsx":        "file-code",
		"README.md":      "file-text",
		"notes.txt":      "file-text",
		"package.json":   "file-braces",
		"config.yaml":    "file-braces",
		"icon.png":       "file-image",
		"logo.svg":       "file-image",
		"yarn.lock":      "file-lock",
		"run.sh":         "terminal",
		"Dockerfile":     "file-text",
		".gitignore":     "file-text",
		"archive.tar.gz": "file",
		"unknown":        "file",
	}
	for name, want := range cases {
		if got := fileIconName(name); got != want {
			t.Errorf("fileIconName(%q) = %q, want %q", name, got, want)
		}
	}
}
