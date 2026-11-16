package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cumulativeNotes = `### 0.0.108

- 闲置会话改为右键恢复。

### 0.0.107

- 项目流程改成锚点加卡片。

### 0.0.106

- 侧栏搜索移入工具栏。

### 0.0.105

- 窗格卡片只保留一层边框。
`

func TestNotesSince(t *testing.T) {
	tests := []struct {
		name    string
		current string
		want    []string
	}{
		{"all newer versions", "0.0.105", []string{"0.0.108", "0.0.107", "0.0.106"}},
		{"only the versions skipped", "0.0.106", []string{"0.0.108", "0.0.107"}},
		{"the newest alone", "0.0.107", []string{"0.0.108"}},
		{"nothing newer", "0.0.108", nil},
		{"numeric fields", "0.0.9", []string{"0.0.108", "0.0.107", "0.0.106", "0.0.105"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notesSince(cumulativeNotes, tt.current)
			for _, v := range tt.want {
				if !strings.Contains(got, "### "+v) {
					t.Errorf("notes since %s lack %s:\n%s", tt.current, v, got)
				}
			}
			if want := len(tt.want); want == 0 && got != "" {
				t.Errorf("notes since %s are %q, want none", tt.current, got)
			}
			for _, v := range []string{"0.0.108", "0.0.107", "0.0.106", "0.0.105"} {
				if containsVersion(tt.want, v) {
					continue
				}
				if strings.Contains(got, "### "+v) {
					t.Errorf("notes since %s still have %s:\n%s", tt.current, v, got)
				}
			}
		})
	}
}

func containsVersion(list []string, v string) bool {
	for _, have := range list {
		if have == v {
			return true
		}
	}
	return false
}

// TestNotesSinceWithoutVersions keeps the notes of releases published before
// the cumulative file: they have no section headings, and are shown whole.
func TestNotesSinceWithoutVersions(t *testing.T) {
	notes := "侧栏底部「闲置」里的会话，左键点一下不再直接恢复。"
	if got := notesSince(notes, "0.0.100"); got != notes {
		t.Fatalf("notes are %q, want them unchanged", got)
	}
}

func TestNotesSinceKeepAChangelog(t *testing.T) {
	notes := "### [0.0.108] - 2026-11-16\n\n- 新东西\n\n### [0.0.107] - 2026-11-15\n\n- 旧东西\n"
	got := notesSince(notes, "0.0.107")
	if !strings.Contains(got, "0.0.108") || strings.Contains(got, "0.0.107") {
		t.Fatalf("notes since 0.0.107 are %q", got)
	}
}

// TestUpdateNotesSource trims the feed's notes, leaving the rest of the
// manifest as published.
func TestUpdateNotesSource(t *testing.T) {
	var served string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			io.WriteString(w, served)
		case "/archive.tar.gz":
			io.WriteString(w, "archive")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	served = `{"version":"0.0.108","notes":"` + strings.ReplaceAll(cumulativeNotes, "\n", `\n`) + `","url":"` + srv.URL + `/archive.tar.gz","signature":"sig"}`
	src := updateNotesSource{manifestURL: srv.URL + "/manifest.json"}

	body, err := src.Manifest(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version   string `json:"version"`
		Notes     string `json:"notes"`
		URL       string `json:"url"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "0.0.108" || manifest.Signature != "sig" || manifest.URL != srv.URL+"/archive.tar.gz" {
		t.Fatalf("manifest changed beyond its notes: %+v", manifest)
	}
	// The test binary runs as a development build, whose version is not a
	// release; every section is newer than it, so all of them stay.
	if !strings.Contains(manifest.Notes, "### 0.0.108") {
		t.Fatalf("notes are %q", manifest.Notes)
	}

	archive, err := src.Open(context.Background(), manifest.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if got, _ := io.ReadAll(archive); string(got) != "archive" {
		t.Fatalf("archive is %q", got)
	}
}
