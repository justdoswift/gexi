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
	"strconv"
	"strings"
	"time"
)

// The files drawer reads what Git already knows: the working tree's changes
// against HEAD, and the text on either side of them. Nothing here writes.

// errNotRepo separates a folder Git does not track from Git failing.
var errNotRepo = errors.New("not a git repository")

// errNoBase is a checkout whose base branch gexi cannot name: no origin HEAD,
// no main or master.
var errNoBase = errors.New("no base branch")

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
	files, _ := parseGitStatus(out)
	return files, nil
}

// gitBranchChanges lists what the branch would bring to base: everything
// since the merge base, uncommitted work included, and returns that base
// commit so the drawer can read each file's old side. Measuring from the
// merge base keeps work that landed on base after the branch started out of
// the list, which `git diff base` would not.
func gitBranchChanges(dir, base string) ([]changedFile, string, error) {
	if base == "" {
		return nil, "", errNoBase
	}
	mb, err := gitMergeBase(dir, base)
	if err != nil {
		return nil, "", err
	}
	out, err := gitOutput(dir, "diff", "--name-status", "-z", "-M", mb)
	if err != nil {
		return nil, "", err
	}
	files := parseNameStatus(out)
	// A diff leaves untracked files out; they are work the branch carries too.
	if status, err := gitOutput(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"); err == nil {
		untracked, _ := parseGitStatus(status)
		for _, f := range untracked {
			if f.Status == 'U' {
				f.Status = 'A'
				files = append(files, f)
			}
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path)
	})
	return files, mb, nil
}

// gitMergeBase resolves where the branch left base, preferring the remote
// base branch when the repository has one.
func gitMergeBase(dir, base string) (string, error) {
	rev := base
	if _, err := gitOutput(dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base); err == nil {
		rev = "origin/" + base
	}
	out, err := gitOutput(dir, "merge-base", rev, "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parseNameStatus reads `git diff --name-status -z`: a letter and a path per
// record, and a rename or copy carrying the old path before the new one.
func parseNameStatus(out []byte) []changedFile {
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
		status, ok := next()
		if !ok {
			break
		}
		if status == "" {
			continue
		}
		path, ok := next()
		if !ok {
			break
		}
		f := changedFile{Path: path, Status: status[0]}
		if f.Status == 'R' || f.Status == 'C' {
			if newPath, ok := next(); ok {
				f.OldPath, f.Path = f.Path, newPath
			}
		}
		files = append(files, f)
	}
	return files
}

// gitSummary is what a pane header's branch pill shows: the branch the
// project is on, whether the working tree differs, and how many lines that
// difference added and deleted.
type gitSummary struct {
	Branch  string
	Added   int
	Deleted int
	Changed bool
	OK      bool
}

// gitSnapshot reads the status once for both the drawer's list and the
// header's pill: the branch and the changes come from one call, and the
// line counts, which status does not carry, from a second.
func gitSnapshot(dir string) ([]changedFile, gitSummary, error) {
	out, err := gitOutput(dir, "status", "--porcelain=v1", "-z", "-b", "--untracked-files=all")
	if err != nil {
		return nil, gitSummary{}, err
	}
	files, branch := parseGitStatus(out)
	s := gitSummary{Branch: branch, Changed: len(files) > 0, OK: true}
	if s.Changed {
		s.Added, s.Deleted = gitNumstat(dir)
	}
	return files, s, nil
}

// gitNumstat sums the lines Git counts between HEAD and the working tree,
// staged and unstaged alike. A repository whose first commit is being made
// has no HEAD to count against.
func gitNumstat(dir string) (added, deleted int) {
	out, err := gitOutput(dir, "diff", "--numstat", "HEAD")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 2 || f[0] == "-" || f[1] == "-" {
			continue
		}
		a, errA := strconv.Atoi(f[0])
		d, errD := strconv.Atoi(f[1])
		if errA != nil || errD != nil {
			continue
		}
		added += a
		deleted += d
	}
	return added, deleted
}

// parseGitStatus reads porcelain v1's NUL-separated records: "XY path", and
// for a rename or copy "XY new" followed by the old path as its own record.
// With -b the first record is "## branch", which it returns as the branch.
func parseGitStatus(out []byte) ([]changedFile, string) {
	var files []changedFile
	branch := ""
	first := true
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
		if !ok {
			break
		}
		if first {
			first = false
			if strings.HasPrefix(rec, "## ") {
				branch = branchFromHeader(rec)
				continue
			}
		}
		if len(rec) < 4 {
			continue
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
	return files, branch
}

// branchFromHeader reads the name out of git status -b's header: "## main",
// "## main...origin/main [ahead 1]", or a repository without a commit yet.
func branchFromHeader(rec string) string {
	rest := strings.TrimPrefix(rec, "## ")
	if strings.HasPrefix(rest, "No commits yet on ") {
		return strings.TrimPrefix(rest, "No commits yet on ")
	}
	if rest == "HEAD (no branch)" {
		return "HEAD"
	}
	if i := strings.Index(rest, "..."); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, " ["); i >= 0 {
		rest = rest[:i]
	}
	return rest
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

// gitTextAt reads one file as it was at rev, empty when it did not exist
// there.
func gitTextAt(dir, rev, path string) string {
	out, err := gitOutput(dir, "show", rev+":"+filepath.ToSlash(path))
	if err != nil {
		return ""
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
	return loadDiffAt(dir, f, "HEAD")
}

// loadDiffAt reads the two sides against rev instead of the working tree's
// HEAD, so the branch view can show each file as it was where the branch
// left its base.
func loadDiffAt(dir string, f changedFile, rev string) fileDiff {
	var d fileDiff
	if f.Status != 'U' {
		oldPath := f.Path
		if f.OldPath != "" {
			oldPath = f.OldPath
		}
		if rev == "HEAD" {
			d.Old = gitText(dir, oldPath)
		} else if rev != "" {
			d.Old = gitTextAt(dir, rev, oldPath)
		}
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
