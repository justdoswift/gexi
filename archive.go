package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justdoswift/gexi/locales"
)

// archivedWorktree is a merged worktree the user cleared away with Archive.
// Its checkout is gone, but the branch stays, so the entry can be restored
// until the user deletes it for good. Entries live in workspace.json beside
// the layout, and Settings › Archived is where they are seen.
type archivedWorktree struct {
	ID         string    `json:"id"`
	ProjectDir string    `json:"projectDir,omitempty"`
	Root       string    `json:"root"`
	Path       string    `json:"path"`
	Branch     string    `json:"branch"`
	Base       string    `json:"base,omitempty"`
	PR         int       `json:"pr,omitempty"`
	URL        string    `json:"url,omitempty"`
	ArchivedAt time.Time `json:"archivedAt"`
}

// archiveWorktree records the worktree and removes its checkout, keeping the
// branch so Restore can bring it back. Main thread.
func (w *workspace) archiveWorktree(wt *paneWorktree, projectDir string) {
	stopPRTimer(wt)
	entry := archivedWorktree{
		ID:         newID(),
		ProjectDir: projectDir,
		Root:       wt.Root,
		Path:       wt.Path,
		Branch:     wt.Branch,
		Base:       wt.Base,
		PR:         wt.PR,
		URL:        wt.URL,
		ArchivedAt: time.Now(),
	}
	w.archived = append([]archivedWorktree{entry}, w.archived...)
	w.scheduleSave()
	w.invalidateSettings()
	go func() {
		if info, err := os.Stat(wt.Path); err == nil && info.IsDir() {
			// --force: a merged worktree has nothing worth keeping even if
			// the user left edits in it.
			if _, err := worktreeGit(wt.Root, "worktree", "remove", "--force", wt.Path); err != nil {
				w.onMain(func() { w.flash(locales.WorktreeError(err.Error())) })
				return
			}
		}
		_, _ = worktreeGit(wt.Root, "worktree", "prune")
	}()
}

// dropArchived forgets one archived entry, without touching the repository.
// Main thread.
func (w *workspace) dropArchived(id string) {
	for i, a := range w.archived {
		if a.ID == id {
			w.archived = append(w.archived[:i], w.archived[i+1:]...)
			w.scheduleSave()
			return
		}
	}
}

// restoreArchivedWorktree puts the checkout back and opens a session in it.
// Git runs off the UI thread.
func (w *workspace) restoreArchivedWorktree(a archivedWorktree) {
	go func() {
		err := restoreWorktree(&a)
		w.onMain(func() {
			if err != nil {
				w.flash(locales.ArchiveRestoreFailed(a.Branch, err.Error()))
				w.invalidateSettings()
				return
			}
			if w.openArchivedSession(a) {
				w.dropArchived(a.ID)
				w.flash(locales.ArchiveRestored(a.Branch))
			}
			w.invalidate()
			w.invalidateSettings()
		})
	}()
}

// restoreWorktree checks the branch out again at its old path. A checkout
// that is already there is left alone.
func restoreWorktree(a *archivedWorktree) error {
	if info, err := os.Stat(a.Root); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is gone", a.Root)
	}
	if info, err := os.Stat(a.Path); err == nil && info.IsDir() {
		if currentBranch(a.Path) == a.Branch {
			return nil
		}
		return fmt.Errorf("%s is already in use", a.Path)
	}
	if _, err := worktreeGit(a.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+a.Branch); err != nil {
		return fmt.Errorf("branch %s no longer exists", a.Branch)
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return err
	}
	if _, err := worktreeGit(a.Root, "worktree", "add", a.Path, a.Branch); err != nil {
		return err
	}
	return nil
}

// openArchivedSession opens the project's session in the restored checkout,
// with the worktree attached again so its header reads as before. It reports
// whether the session is open. Main thread.
func (w *workspace) openArchivedSession(a archivedWorktree) bool {
	dir := a.ProjectDir
	if dir == "" {
		dir = a.Root
	}
	pr := w.projectFor(filepath.Clean(dir))
	s, err := w.newSession(pr, nil, a.Path)
	if err != nil {
		w.flash(locales.WorktreeError(err.Error()))
		return false
	}
	ps := s.Root.panes()
	if len(ps) == 0 {
		return false
	}
	ps[0].Worktree = &paneWorktree{
		Root:   a.Root,
		Path:   a.Path,
		Branch: a.Branch,
		Base:   a.Base,
		PR:     a.PR,
		URL:    a.URL,
		State:  worktreeMerged,
	}
	if w.win != nil {
		w.win.Focus()
	}
	return true
}

// deleteArchivedWorktree removes an archived entry for good. Git runs off
// the UI thread.
func (w *workspace) deleteArchivedWorktree(a archivedWorktree) {
	go func() {
		err := deleteArchived(&a)
		w.onMain(func() {
			if err != nil {
				w.flash(locales.ArchiveDeleteFailed(a.Branch, err.Error()))
			} else {
				w.dropArchived(a.ID)
				w.flash(locales.ArchiveDeleted(a.Branch))
			}
			w.invalidateSettings()
		})
	}()
}

// deleteArchived removes what is left of an archived worktree: the checkout
// if it is somehow still there, the local branch, and the remote branch.
// Nothing left to remove is success.
func deleteArchived(a *archivedWorktree) error {
	if info, err := os.Stat(a.Root); err != nil || !info.IsDir() {
		return nil
	}
	if info, err := os.Stat(a.Path); err == nil && info.IsDir() {
		return removeWorktree(&paneWorktree{
			Root: a.Root, Path: a.Path, Branch: a.Branch, Base: a.Base,
		})
	}
	_, _ = worktreeGit(a.Root, "worktree", "prune")
	if _, err := worktreeGit(a.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+a.Branch); err != nil {
		return nil
	}
	if _, err := worktreeGit(a.Root, "branch", "-D", a.Branch); err != nil {
		return err
	}
	_, _ = worktreeGit(a.Root, "push", "origin", "--delete", a.Branch)
	return nil
}

// archivedDetail is the second line of an archived row: where the checkout
// was and when it was archived.
func archivedDetail(a archivedWorktree) string {
	detail := abbreviate(a.Path)
	if !a.ArchivedAt.IsZero() {
		detail += " · " + a.ArchivedAt.Local().Format("2006-01-02")
	}
	return strings.TrimSpace(detail)
}
