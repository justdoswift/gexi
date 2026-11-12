package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
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
	// Broken marks a folder whose git metadata is gone: the remains of a
	// checkout git could not finish deleting. It is still listed, so a
	// leftover can be found and removed instead of sitting invisible.
	Broken bool
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
				// Git deletes a worktree's metadata before the folder, so a
				// folder it failed to delete has no HEAD left to read. The
				// folder still belongs to gexi, and the page is the only
				// place it can be found.
				out = append(out, worktreeScanEntry{
					Path: path, Root: repoRoot, Branch: name.Name(), Broken: true,
				})
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

func (r worktreeRow) projectName() string {
	if r.archived != nil {
		if r.archived.ProjectDir != "" {
			return filepath.Base(filepath.Clean(r.archived.ProjectDir))
		}
		if r.archived.Root != "" {
			return filepath.Base(filepath.Clean(r.archived.Root))
		}
	}
	if r.entry.Root != "" {
		return filepath.Base(filepath.Clean(r.entry.Root))
	}
	return filepath.Base(filepath.Dir(filepath.Clean(r.entry.Path)))
}

func (r worktreeRow) detail() string {
	project := r.projectName()
	if r.archived != nil {
		detail := archivedDetail(*r.archived)
		if project != "" {
			return project + " · " + detail
		}
		return detail
	}
	detail := abbreviate(r.entry.Path)
	if project != "" {
		return project + " · " + detail
	}
	return detail
}

func (r worktreeRow) status() string {
	switch {
	case r.archived != nil:
		if r.archived.Kept {
			return locales.WorktreeStatusKept()
		}
		return locales.WorktreeStatusArchived()
	case r.entry.Broken:
		return locales.WorktreeStatusBroken()
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
		// A leftover whose git metadata is gone cannot name its repository.
		// Match its repo folder against the open projects so the page groups
		// it under the project it came from, not under a hash.
		if e.Root == "" {
			repo := filepath.Base(filepath.Dir(e.Path))
			for _, pr := range w.Projects {
				if worktreeRepoName(pr.Dir) == repo {
					e.Root = pr.Dir
					break
				}
			}
		}
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
	// Group by project first, then keep the live rows ahead of archived ones
	// within it. The page draws one section per project.
	sort.SliceStable(rows, func(i, j int) bool {
		pi, pj := strings.ToLower(rows[i].projectName()), strings.ToLower(rows[j].projectName())
		if pi != pj {
			return pi < pj
		}
		if rows[i].rank() != rows[j].rank() {
			return rows[i].rank() < rows[j].rank()
		}
		return rows[i].branch() < rows[j].branch()
	})
	return rows
}

// worktreeGroup is one project's rows on the Worktrees page.
type worktreeGroup struct {
	name string
	rows []worktreeRow
}

func groupWorktreeRows(rows []worktreeRow) []worktreeGroup {
	var groups []worktreeGroup
	for _, row := range rows {
		name := row.projectName()
		if len(groups) == 0 || groups[len(groups)-1].name != name {
			groups = append(groups, worktreeGroup{name: name})
		}
		groups[len(groups)-1].rows = append(groups[len(groups)-1].rows, row)
	}
	return groups
}

// worktreesPage lists every worktree and keeps the page's cleanup action
// beside them, so finding a checkout and clearing unused ones are one task.
func (w *workspace) worktreesPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsWorktreesMerge())
	settingsCard(c, tk, func() {
		w.settingRowView(c, tk, settingRow{
			title:  locales.SettingsArchiveAfterMerge(),
			detail: locales.SettingsArchiveAfterMergeDetail(),
			on:     w.settings.ArchiveAfterMerge,
			toggle: func() {
				w.settings.ArchiveAfterMerge = !w.settings.ArchiveAfterMerge
				w.settings.save()
			},
		})
	})
	archivedCount, keptCount := 0, 0
	for _, a := range w.archived {
		if a.Kept {
			keptCount++
		} else {
			archivedCount++
		}
	}
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
		if archivedCount > 0 {
			cardDivider(c, tk)
			if clearing := w.archivedClear; clearing != nil && !clearing.kept {
				w.settingProgressView(c, tk,
					locales.WorktreeClearArchived(),
					locales.WorktreeClearArchivedDetail(strconv.Itoa(archivedCount)),
					locales.WorktreeClearProgress(strconv.Itoa(clearing.done), strconv.Itoa(clearing.total)),
				)
			} else {
				w.settingActionView(c, tk,
					locales.WorktreeClearArchived(),
					locales.WorktreeClearArchivedDetail(strconv.Itoa(archivedCount)),
					locales.ArchiveDelete(),
					func() { w.confirmClearArchived(false) },
				)
			}
		}
		if keptCount > 0 {
			cardDivider(c, tk)
			if clearing := w.archivedClear; clearing != nil && clearing.kept {
				w.settingProgressView(c, tk,
					locales.WorktreeClearKept(),
					locales.WorktreeClearKeptDetail(strconv.Itoa(keptCount)),
					locales.WorktreeClearProgress(strconv.Itoa(clearing.done), strconv.Itoa(clearing.total)),
				)
			} else {
				w.settingActionView(c, tk,
					locales.WorktreeClearKept(),
					locales.WorktreeClearKeptDetail(strconv.Itoa(keptCount)),
					locales.ArchiveDelete(),
					func() { w.confirmClearArchived(true) },
				)
			}
		}
	})
	sectionLabel(c, tk, locales.SettingsWorktreesAll())
	rows := w.worktreeRows()
	if len(rows) == 0 {
		settingsCard(c, tk, func() {
			ui.Row(c).Padding(10, 16, 10, 16).Children(func() {
				ui.Text(c, locales.SettingsWorktreesEmpty()).FontSize(12).
					TextColor(tk.MutedForeground)
			})
		})
		return
	}
	for _, group := range groupWorktreeRows(rows) {
		sectionLabel(c, tk, group.name)
		settingsCard(c, tk, func() {
			for i, row := range group.rows {
				if i > 0 {
					cardDivider(c, tk)
				}
				w.worktreeRowView(c, tk, row)
			}
		})
	}
}

func (w *workspace) worktreeRowView(c *ui.Context, tk tokens, row worktreeRow) {
	ui.Row(c).Padding(10, 16, 10, 16).Gap(8).Children(func() {
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
		case row.entry.Broken:
			e := row.entry
			outlineButton(c, tk, locales.ArchiveDelete(), func() { w.confirmDeleteBroken(e) })
		default:
			e := row.entry
			outlineButton(c, tk, locales.WorktreeOpen(), func() { w.openWorktreeAt(e) })
		}
	})
}

// confirmDeleteBroken asks before the remains of a checkout git could not
// finish deleting go for good. The native alert blocks, so it runs off the UI
// thread and comes back.
func (w *workspace) confirmDeleteBroken(e worktreeScanEntry) {
	parent := w.settingsWin
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             parent,
			Type:               mygo.MessageWarning,
			Title:              locales.WorktreeDeleteBrokenTitle(),
			Message:            locales.WorktreeDeleteBrokenBody(abbreviate(e.Path)),
			Buttons:            []string{locales.ArchiveDelete(), locales.Cancel()},
			DefaultButton:      1,
			CancelButton:       1,
			DestructiveButtons: []int{0},
		})
		if err != nil || res.Button != 0 {
			return
		}
		w.onMain(func() { w.deleteBrokenWorktree(e) })
	}()
}

// deleteBrokenWorktree removes a leftover folder that is no longer a git
// worktree. It can still hold files, so the row asks before it goes.
func (w *workspace) deleteBrokenWorktree(e worktreeScanEntry) {
	go func() {
		err := forceRemoveDir(e.Path)
		w.onMain(func() {
			w.worktreesScanned = false
			if err != nil {
				w.flash(locales.ArchiveDeleteFailed(e.Branch, err.Error()))
			} else {
				delete(w.worktreeRestores, filepath.Clean(e.Path))
				w.scheduleSave()
				w.flash(locales.ArchiveDeleted(e.Branch))
				_ = os.Remove(filepath.Dir(e.Path))
			}
			w.invalidateSettings()
		})
	}()
}

// archivedClearProgress is one batch deletion from Settings › Worktrees:
// kept says which row started it, and done of total entries have been
// tried.
type archivedClearProgress struct {
	kept  bool
	done  int
	total int
}

// confirmClearArchived asks before every archived entry (kept false) or every
// branch kept by Leave (kept true) is deleted. The alert is the app's own,
// drawn over the settings window like the rest of its dialogs.
func (w *workspace) confirmClearArchived(kept bool) {
	if w.archivedClear != nil || w.clearArchivedOpen {
		return
	}
	var entries []archivedWorktree
	for _, a := range w.archived {
		if a.Kept == kept {
			entries = append(entries, a)
		}
	}
	if len(entries) == 0 {
		return
	}
	body := locales.WorktreeClearArchivedBody(strconv.Itoa(len(entries)))
	if kept {
		body = locales.WorktreeClearKeptBody(strconv.Itoa(len(entries)))
	}
	w.clearArchivedOpen = true
	w.clearArchivedKept = kept
	w.clearArchivedMsg = body
	w.clearArchivedEntries = entries
	w.invalidateSettings()
}

// clearArchivedDialog is the batch deletion's confirmation, over the page
// that asked for it. Cancel holds Return, since Enter must not delete a
// batch of branches.
func (w *workspace) clearArchivedDialog(c *ui.Context, tk tokens) {
	title := locales.WorktreeClearArchivedTitle()
	if w.clearArchivedKept {
		title = locales.WorktreeClearKeptTitle()
	}
	entries, kept := w.clearArchivedEntries, w.clearArchivedKept
	dialogAlert(c, tk, &w.clearArchivedOpen, 430, title, w.clearArchivedMsg,
		dialogAction{label: locales.Cancel(), kind: dialogQuiet, auto: true, run: func() {
			w.clearArchivedOpen = false
			w.clearArchivedEntries = nil
		}},
		dialogAction{label: locales.ArchiveDelete(), kind: dialogDanger, run: func() {
			w.clearArchivedOpen = false
			w.clearArchivedEntries = nil
			w.clearArchived(entries, kept)
		}},
	)
}

// clearArchived deletes the branches behind the entries and drops the ones
// that went. Entries whose branch could not be deleted stay on the page. Each
// entry reports back to the page as it finishes, so a batch that takes a
// while says how far it has got.
func (w *workspace) clearArchived(entries []archivedWorktree, kept bool) {
	if w.archivedClear != nil || len(entries) == 0 {
		return
	}
	w.archivedClear = &archivedClearProgress{kept: kept, total: len(entries)}
	w.invalidateSettings()
	go func() {
		var done []string
		for i := range entries {
			if deleteArchived(&entries[i]) == nil {
				done = append(done, entries[i].ID)
			}
			w.onMain(func() {
				if w.archivedClear == nil {
					return
				}
				w.archivedClear.done++
				w.refreshSettings()
			})
		}
		w.onMain(func() {
			w.archivedClear = nil
			removed := map[string]bool{}
			for _, id := range done {
				removed[id] = true
			}
			for i := range entries {
				if removed[entries[i].ID] {
					delete(w.worktreeRestores, filepath.Clean(entries[i].Path))
				}
			}
			w.archived = dropArchivedIDs(w.archived, removed)
			w.worktreesScanned = false
			w.scheduleSave()
			w.refreshSettings()
			if len(done) > 0 {
				w.flash(locales.WorktreeArchivedCleared(strconv.Itoa(len(done))))
			} else {
				w.flash(locales.WorktreeCleanupNothing())
			}
		})
	}()
}

// dropArchivedIDs removes the entries whose branches were deleted. Entries
// whose deletion failed keep their place on the page.
func dropArchivedIDs(entries []archivedWorktree, removed map[string]bool) []archivedWorktree {
	kept := entries[:0]
	for _, a := range entries {
		if !removed[a.ID] {
			kept = append(kept, a)
		}
	}
	return kept
}

// worktreeRoots is every repository whose git worktree metadata this window
// should prune: the open projects and the repositories of scanned checkouts.
func (w *workspace) worktreeRoots() []string {
	seen := map[string]bool{}
	var roots []string
	add := func(root string) {
		if root == "" || seen[filepath.Clean(root)] {
			return
		}
		seen[filepath.Clean(root)] = true
		roots = append(roots, root)
	}
	for _, pr := range w.Projects {
		add(pr.Dir)
	}
	for _, e := range w.worktrees() {
		add(e.Root)
	}
	return roots
}

// pruneWorktrees drops git's records of checkouts that were deleted outside
// gexi. Without it, a folder removed by hand leaves a worktree entry the page
// cannot see and the cleanup cannot reach.
func (w *workspace) pruneWorktrees(roots []string) {
	for _, root := range roots {
		_, _ = worktreeGit(root, "worktree", "prune")
	}
}

// revealPane brings an open pane to the front, with the settings window out
// of the way. Main thread.
func (w *workspace) revealPane(p *pane) {
	if p == nil || w.paneByID(p.ID) != p {
		return
	}
	if s := w.sessionOf(p); s != nil && s.zoomedPane != "" && s.zoomedPane != p.ID {
		// The pane to show is behind a zoomed one, so the session has to
		// come back to its whole layout. A click on the zoomed pane keeps
		// the stage as it was.
		s.zoomedPane = ""
		w.syncStageMirrors()
	}
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
	key := filepath.Clean(e.Path)
	command := w.worktreeRestores[key]
	s, err := w.newSession(pr, command, e.Path)
	if err != nil {
		w.flash(locales.WorktreeError(err.Error()))
		return
	}
	if _, ok := w.worktreeRestores[key]; ok {
		delete(w.worktreeRestores, key)
		w.scheduleSave()
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
