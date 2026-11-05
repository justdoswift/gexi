package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The files drawer reads what Git already knows: the working tree's changes
// against HEAD, and the text on either side of them. Nothing here writes.

// errNotRepo separates a folder Git does not track from Git failing.
var errNotRepo = errors.New("not a git repository")

// changedFile is one entry of `git status --porcelain`: the path relative to
// the project, the path it came from when it was renamed or copied, and the
// letter the row shows.
type changedFile struct {
	Path    string
	OldPath string
	Status  byte
}

// name is the file's own name, without the folders above it.
func (f changedFile) name() string { return filepath.Base(f.Path) }

// dir is the folder the file sits in, relative to the project, empty at the
// top.
func (f changedFile) dir() string {
	d := filepath.Dir(f.Path)
	if d == "." {
		return ""
	}
	return d
}

// gitChanges lists the working tree's changes in dir. A folder outside a
// repository reports errNotRepo rather than an empty list, so the drawer can
// say why it is empty.
func gitChanges(dir string) ([]changedFile, error) {
	out, err := gitOutput(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseGitStatus(out), nil
}

// parseGitStatus reads porcelain v1's NUL-separated records: "XY path", and
// for a rename or copy "XY new" followed by the old path as its own record.
func parseGitStatus(out []byte) []changedFile {
	var files []changedFile
	next := func() (string, bool) {
		if len(out) == 0 {
			return "", false
		}
		i := bytes.IndexByte(out, 0)
		if i < 0 {
			rec := string(out)
			out = nil
			return rec, true
		}
		rec := string(out[:i])
		out = out[i+1:]
		return rec, true
	}
	for {
		rec, ok := next()
		if !ok || len(rec) < 4 {
			break
		}
		x, y := rec[0], rec[1]
		f := changedFile{Path: rec[3:], Status: changeLetter(x, y)}
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			if old, ok := next(); ok {
				f.OldPath = old
			}
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path)
	})
	return files
}

// changeLetter folds Git's two status letters into the one a row shows: the
// worktree's state when it has one, else the index's.
func changeLetter(x, y byte) byte {
	switch {
	case x == '?' || y == '?':
		return 'U'
	case x == 'U' || y == 'U' || x == 'A' && y == 'A' || x == 'D' && y == 'D':
		return '!'
	case y != ' ':
		return y
	case x != ' ':
		return x
	}
	return 'M'
}

// gitOutput runs Git in dir with a deadline, so a locked index cannot hang
// the drawer's refresh.
func gitOutput(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.ToLower(stderr.String())
		if strings.Contains(msg, "not a git repository") {
			return nil, errNotRepo
		}
		if text := strings.TrimSpace(stderr.String()); text != "" {
			return nil, fmt.Errorf("%v: %s", err, text)
		}
		return nil, err
	}
	return out, nil
}

// gitText is the file's text at HEAD, or in the index when HEAD has none —
// a repository whose first commit is still being made. Missing text is the
// empty string: an added or deleted file has one side of nothing.
func gitText(dir, path string) string {
	rel := filepath.ToSlash(path)
	out, err := gitOutput(dir, "show", "HEAD:"+rel)
	if err != nil {
		out, err = gitOutput(dir, "show", ":"+rel)
		if err != nil {
			return ""
		}
	}
	return string(out)
}

// tooLarge is where a file is shown as a note instead of its text: a diff of
// tens of megabytes is not worth the frame it would cost.
const tooLarge = 4 << 20

// fileDiff is what the drawer shows for a change: the two texts, or why it
// shows a note instead.
type fileDiff struct {
	Old, New string
	Binary   bool
	TooLarge bool
}

// loadDiff reads both sides of a change. The new side is the file on disk, so
// a deleted change has none; the old side is HEAD's text, so an untracked one
// has none.
func loadDiff(dir string, f changedFile) fileDiff {
	var d fileDiff
	if f.Status != 'U' {
		oldPath := f.Path
		if f.OldPath != "" {
			oldPath = f.OldPath
		}
		d.Old = gitText(dir, oldPath)
	}
	if data, err := os.ReadFile(filepath.Join(dir, f.Path)); err == nil {
		d.New = string(data)
	}
	if len(d.Old) > tooLarge || len(d.New) > tooLarge {
		d.TooLarge = true
		d.Old, d.New = "", ""
		return d
	}
	d.Binary = isBinary(d.Old) || isBinary(d.New)
	return d
}

// loadText reads a file for the tree's preview.
func loadText(dir, path string) (text string, binary, large bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return "", false, false, err
	}
	if len(data) > tooLarge {
		return "", false, true, nil
	}
	return string(data), isBinary(string(data)), false, nil
}

// isBinary reports whether text holds a NUL byte, which no source file does.
func isBinary(text string) bool {
	return strings.IndexByte(text, 0) >= 0
}
