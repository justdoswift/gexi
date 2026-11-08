package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// The Worktrees page is the one place every checkout gexi knows about shows
// up: running in the background, open in a pane, sitting on disk, or archived
// with only its branch left. A row brings one back rather than leaving the
// user to hunt through the folder.

// worktreeScanEntry is one checkout under the app's worktree folder.
type worktreeScanEntry struct {
	Path   string
	Root   string
	Branch string
}

// scanWorktrees lists every checkout gexi made under its worktree folder. The
// branch and repository root come from the checkout's own git files, so the
// list costs no git process.
func scanWorktrees() []worktreeScanEntry {
	rootDir := worktreesRoot()
	repos, err := os.ReadDir(rootDir)
	if err != nil {
		return nil
	}
	var out []worktreeScanEntry
	for _, repo := range repos {
		if !repo.IsDir() {
			continue
		}
		names, err := os.ReadDir(filepath.Join(rootDir, repo.Name()))
		if err != nil {
			continue
		}
		for _, name := range names {
			if !name.IsDir() {
				continue
			}
			path := filepath.Join(rootDir, repo.Name(), name.Name())
			branch, repoRoot := worktreeCheckoutInfo(path)
			if branch == "" {
				continue
			}
			out = append(out, worktreeScanEntry{Path: path, Root: repoRoot, Branch: branch})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// worktreeCheckoutInfo reads the branch and the repository root of a checkout
// without asking git: a linked worktree's .git file names the directory that
// holds its HEAD and commondir.
func worktreeCheckoutInfo(path string) (branch, repoRoot string) {
	git := filepath.Join(path, ".git")
	info, err := os.Stat(git)
	if err != nil {
		return "", ""
	}
	dir := git
	if !info.IsDir() {
		data, err := os.ReadFile(git)
		if err != nil {
			return "", ""
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
		if !ok {
			return "", ""
		}
		dir = strings.TrimSpace(rest)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(path, dir)
		}
	}
	branch = readGitHead(filepath.Join(dir, "HEAD"))
	if data, err := os.ReadFile(filepath.Join(dir, "commondir")); err == nil {
		common := strings.TrimSpace(string(data))
		if common != "" {
			if !filepath.IsAbs(common) {
				common = filepath.Join(dir, common)
			}
			repoRoot = filepath.Dir(filepath.Clean(common))
		}
	}
	if repoRoot == "" {
		if info.IsDir() {
			repoRoot = path
		} else {
			// <repo>/.git/worktrees/<name>
			repoRoot = filepath.Dir(dir)
			if filepath.Base(repoRoot) == "worktrees" {
				repoRoot = filepath.Dir(filepath.Dir(repoRoot))
			}
		}
	}
	return branch, repoRoot
}

// readGitHead reads a branch name from a HEAD file, or a short commit for a
// detached one.
func readGitHead(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
		return ref
	}
	if len(line) > 7 {
		return line[:7]
	}
	return line
}

// worktrees is the scan, cached until something changes it. Main thread.
func (w *workspace) worktrees() []worktreeScanEntry {
	if !w.worktreesScanned {
		w.worktreesScan = scanWorktrees()
		w.worktreesScanned = true
	}
	return w.worktreesScan
}

// worktreeRow is one line of the page: a checkout, an archived branch, and
// the pane using it, if any.
type worktreeRow struct {
	entry      worktreeScanEntry
	archived   *archivedWorktree
	pane       *pane
	background bool
}

func (r worktreeRow) rank() int {
	switch {
	case r.archived != nil:
		return 3
	case r.background:
		return 0
	case r.pane != nil:
		return 1
	default:
		return 2
	}
}

func (r worktreeRow) branch() string {
	if r.archived != nil {
		return r.archived.Branch
	}
	return r.entry.Branch
}

func (r worktreeRow) detail() string {
	if r.archived != nil {
		return archivedDetail(*r.archived)
	}
	return abbreviate(r.entry.Path)
}

func (r worktreeRow) status() string {
	switch {
	case r.archived != nil:
		return locales.WorktreeStatusArchived()
	case r.background:
		return locales.WorktreeStatusBackground()
	case r.pane != nil:
		return locales.WorktreeStatusOpen()
	default:
		return locales.WorktreeStatusOnDisk()
	}
}

// worktreeRows joins the scan, the archived list, and the panes using a
// checkout, background first and archived last. Main thread.
func (w *workspace) worktreeRows() []worktreeRow {
	archived := map[string]*archivedWorktree{}
	for i := range w.archived {
		archived[filepath.Clean(w.archived[i].Path)] = &w.archived[i]
	}
	type live struct {
		p          *pane
		background bool
	}
	byPath := map[string]live{}
	note := func(p *pane, background bool) {
		if p.Worktree != nil {
			byPath[filepath.Clean(p.Worktree.Path)] = live{p, background}
		}
	}
	for _, p := range w.allPanes() {
		note(p, false)
	}
	for _, p := range w.detached {
		note(p, true)
	}
	inCheckout := func(path string) (live, bool) {
		for _, p := range w.detached {
			if dirWithin(p.dir(), path) || dirWithin(p.initialDir, path) {
				return live{p, true}, true
			}
		}
		for _, p := range w.allPanes() {
			if dirWithin(p.dir(), path) || dirWithin(p.initialDir, path) {
				return live{p, false}, true
			}
		}
		return live{}, false
	}
	var rows []worktreeRow
	for _, e := range w.worktrees() {
		key := filepath.Clean(e.Path)
		if archived[key] != nil {
			continue
		}
		l, ok := byPath[key]
		if !ok {
			l, ok = inCheckout(e.Path)
			if ok {
				byPath[key] = l
			}
		}
		rows = append(rows, worktreeRow{entry: e, pane: l.p, background: l.background})
	}
	for i := range w.archived {
		rows = append(rows, worktreeRow{archived: &w.archived[i]})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank() < rows[j].rank() })
	return rows
}

// worktreesPage lists every worktree and keeps the page's cleanup action
// beside them, so finding a checkout and clearing unused ones are one task.
func (w *workspace) worktreesPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsWorktreesMaintenance())
	settingsCard(c, tk, func() {
		w.settingActionView(c, tk,
			locales.SettingsCleanupWorktrees(),
			locales.SettingsCleanupWorktreesDetail(),
			locales.SettingsCleanupWorktreesAction(),
			func() {
				w.cleanupOrphanWorktrees()
				w.worktreesScanned = false
				w.invalidateSettings()
			},
		)
	})
	sectionLabel(c, tk, locales.SettingsWorktreesAll())
	settingsCard(c, tk, func() {
		rows := w.worktreeRows()
		if len(rows) == 0 {
			ui.Row(c).Padding(12, 16, 12, 16).Children(func() {
				ui.Text(c, locales.SettingsWorktreesEmpty()).FontSize(12).
					TextColor(tk.MutedForeground)
			})
			return
		}
		for i, row := range rows {
			if i > 0 {
				cardDivider(c, tk)
			}
			w.worktreeRowView(c, tk, row)
		}
	})
}

func (w *workspace) worktreeRowView(c *ui.Context, tk tokens, row worktreeRow) {
	ui.Row(c).Padding(12, 16, 12, 16).Gap(8).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, row.branch()).FontSize(13).TextColor(tk.Foreground)
				ui.Text(c, row.status()).FontSize(11).TextColor(tk.MutedForeground)
			})
			ui.Text(c, row.detail()).FontSize(11).
				TextColor(tk.MutedForeground).SingleLine()
		})
		ui.Spacer(c)
		switch {
		case row.archived != nil:
			a := *row.archived
			outlineButton(c, tk, locales.ArchiveRestore(), func() {
				w.restoreArchivedWorktree(a)
			})
			outlineButton(c, tk, locales.ArchiveDelete(), func() {
				w.confirmDeleteArchived(a)
			})
		case row.background:
			p := row.pane
			outlineButton(c, tk, locales.WorktreeRestore(), func() { w.reattachPane(p) })
		case row.pane != nil:
			p := row.pane
			outlineButton(c, tk, locales.WorktreeShow(), func() { w.revealPane(p) })
		default:
			e := row.entry
			outlineButton(c, tk, locales.WorktreeOpen(), func() { w.openWorktreeAt(e) })
		}
	})
}

// revealPane brings an open pane to the front, with the settings window out
// of the way. Main thread.
func (w *workspace) revealPane(p *pane) {
	if p == nil || w.paneByID(p.ID) != p {
		return
	}
	if s := w.sessionOf(p); s != nil {
		s.zoomedPane = ""
	}
	w.syncStageMirrors()
	w.focusPane(p)
	w.dismissSettings()
	if w.win != nil {
		w.win.Show()
		w.win.Focus()
	}
}

// openWorktreeAt starts a session in a checkout that no pane uses, with the
// worktree attached so its header reads as before. Main thread.
func (w *workspace) openWorktreeAt(e worktreeScanEntry) {
	root := e.Root
	if root == "" {
		root = e.Path
	}
	if info, err := os.Stat(e.Path); err != nil || !info.IsDir() {
		w.flash(locales.WorktreeOpenFailed(abbreviate(e.Path)))
		w.worktreesScanned = false
		w.invalidateSettings()
		return
	}
	pr := w.projectFor(filepath.Clean(root))
	s, err := w.newSession(pr, nil, e.Path)
	if err != nil {
		w.flash(locales.WorktreeError(err.Error()))
		return
	}
	if ps := s.Root.panes(); len(ps) > 0 {
		ps[0].Worktree = &paneWorktree{
			Root:   filepath.Clean(root),
			Path:   e.Path,
			Branch: e.Branch,
		}
		w.refreshWorktree(ps[0])
	}
	w.worktreesScanned = false
	w.dismissSettings()
	if w.win != nil {
		w.win.Show()
		w.win.Focus()
	}
}
