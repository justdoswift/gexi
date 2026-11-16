package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/egoist/mygo"
)

// updateManifestURL is the manifest `mygo build -upload` publishes for this
// platform: the latest release of the repository in mygo.json's updates.
const updateManifestURL = "https://github.com/justdoswift/gexi/releases/latest/download/update-darwin-arm64.json"

// updateNotesSource serves the feed as it is published, with the release
// notes cut to the versions after the one running.
//
// The manifest's notes hold every version's section (RELEASE_NOTES.md), so
// an app that updates from an older release can show what it skipped, not
// only the newest version. The window has no way to trim them itself, and
// the manifest is static, so the trim has to happen here, where the running
// version is known.
type updateNotesSource struct {
	manifestURL string
}

func (s updateNotesSource) Manifest(ctx context.Context, _ bool) (io.ReadCloser, error) {
	body, err := s.get(ctx, s.manifestURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the update manifest: %w", err)
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("reading the update manifest: %w", err)
	}
	var notes string
	if raw, ok := manifest["notes"]; ok {
		if err := json.Unmarshal(raw, &notes); err != nil {
			return nil, fmt.Errorf("reading the update notes: %w", err)
		}
	}
	trimmed := notesSince(notes, mygo.App.Version())
	if trimmed == notes {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	raw, err := json.Marshal(trimmed)
	if err != nil {
		return nil, err
	}
	manifest["notes"] = raw
	out, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

func (s updateNotesSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	return s.get(ctx, url)
}

func (s updateNotesSource) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// notesSince returns the sections of notes that are newer than current.
// Sections start at a "### <version>" heading, the form release-notes.sh
// writes. Notes without such headings are returned as they are, which keeps
// releases published before the cumulative notes readable.
func notesSince(notes, current string) string {
	lines := strings.Split(notes, "\n")
	sections, heading := 0, -1
	for i, line := range lines {
		if _, ok := sectionVersion(line); ok {
			sections++
			if heading < 0 && sectionVersionIsNewer(line, current) {
				heading = i
			}
		}
	}
	if sections == 0 {
		return notes
	}
	if heading < 0 {
		return ""
	}
	var out []string
	for _, line := range lines[heading:] {
		if v, ok := sectionVersion(line); ok && !versionNewer(v, current) {
			break
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func sectionVersionIsNewer(line, current string) bool {
	v, ok := sectionVersion(line)
	return ok && versionNewer(v, current)
}

// sectionVersion returns the version of a "### <version>" heading, if the
// line is one: "$ ### 0.0.107", or "### [0.0.107] - date" as Keep a
// Changelog writes it.
func sectionVersion(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "### ")
	if !ok {
		return "", false
	}
	v := strings.TrimSpace(rest)
	v = strings.TrimPrefix(v, "[")
	if i := strings.IndexAny(v, "] \t"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimPrefix(v, "v")
	if v == "" || v[0] < '0' || v[0] > '9' || !strings.Contains(v, ".") {
		return "", false
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r == '.', r == '-', r == '+':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		default:
			return "", false
		}
	}
	return v, true
}
