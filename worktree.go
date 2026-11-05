package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A session may own a git worktree of its project: its own branch and folder,
// so several sessions can work on the same repository at once. Conductor's
// workspaces are the model. The branch starts with a city name, and the agent
// renames it from the user's first request; the pane header then offers PR and
// Merge.

type paneWorktree struct {
	Root   string `json:"root"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	PR     int    `json:"pr,omitempty"`
	URL    string `json:"url,omitempty"`
	State  string `json:"state,omitempty"`
	// Naming is true until the first prompt has been used to rename the
	// placeholder branch.
	Naming bool `json:"naming,omitempty"`

	// busy is the operation the header button waits on: "pr" or "merge".
	// checked throttles the gh lookups when a session is selected.
	busy    string
	checked time.Time
}

const (
	worktreeBusyCreate = "create"
	worktreeBusyPR     = "pr"
	worktreeBusyMerge  = "merge"
	worktreeMerged     = "merged"
)

// worktreeCities are the placeholder names, as Conductor uses cities: short,
// memorable, and never mistaken for a task.
var worktreeCities = []string{
	"kyoto", "lisbon", "osaka", "porto", "taipei", "seoul", "helsinki",
	"prague", "vienna", "zurich", "bergen", "tallinn", "riga", "vilnius",
	"warsaw", "krakow", "budapest", "athens", "sofia", "dublin", "edinburgh",
	"glasgow", "bristol", "oxford", "cambridge", "bruges", "ghent", "antwerp",
	"utrecht", "delft", "leiden", "lille", "lyon", "nantes", "bordeaux",
	"toulouse", "nice", "genoa", "bologna", "florence", "naples", "valencia",
	"seville", "granada", "bilbao", "malaga", "faro", "split", "dubrovnik",
}

func worktreesRoot() string {
	return filepath.Join(supportDir(), "worktrees")
}

// worktreeCreateMu serializes naming and adding worktrees, so two panes
// clicked at once cannot pick the same city and race for the same branch.
var worktreeCreateMu sync.Mutex

// createWorktree branches from the latest origin base branch, as Conductor
// does, and gives the branch a placeholder city name.
func createWorktree(projectDir string) (*paneWorktree, error) {
	worktreeCreateMu.Lock()
	defer worktreeCreateMu.Unlock()
	root, ok := gitTopLevel(projectDir)
	if !ok || !sameDir(root, projectDir) {
		return nil, nil
	}
	base := detectBaseBranch(root)
	if base == "" {
		return nil, nil
	}
	fetchBase(root, base)

	start := base
	if _, err := worktreeGit(root, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
		start = "origin/" + base
	}
	name := uniqueWorktreeName(root)
	path := filepath.Join(worktreesRoot(), worktreeRepoName(root), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// --no-track keeps the branch from pushing to the base it started from.
	if _, err := worktreeGit(root, "worktree", "add", "--no-track", "-b", name, path, start); err != nil {
		return nil, err
	}
	return &paneWorktree{
		Root:   root,
		Path:   path,
		Branch: name,
		Base:   base,
		Naming: true,
	}, nil
}

// sameDir compares two paths as folders, so /tmp and /private/tmp are one.
func sameDir(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func gitTopLevel(dir string) (string, bool) {
	out, err := worktreeGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	top := strings.TrimSpace(out)
	if top == "" {
		return "", false
	}
	return filepath.Clean(top), true
}

// detectBaseBranch asks origin what its default branch is, then falls back to
// the two names almost every repository uses.
func detectBaseBranch(root string) string {
	if out, err := worktreeGit(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(out), "origin/"); name != "" {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := worktreeGit(root, "rev-parse", "--verify", "--quiet", name); err == nil {
			return name
		}
	}
	return ""
}

// fetchBase brings the base branch up to date before branching. It is best
// effort: a slow or missing network must not stop the session from opening.
func fetchBase(root, base string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "fetch", "--quiet", "origin", base)
	_ = cmd.Run()
}

func randomCityName() string {
	return worktreeCities[rand.IntN(len(worktreeCities))]
}

// uniqueWorktreeName avoids a local or remote branch, and a folder left by an
// older worktree.
func uniqueWorktreeName(root string) string {
	dir := filepath.Join(worktreesRoot(), worktreeRepoName(root))
	for i := 0; i < 60; i++ {
		name := randomCityName()
		if i > 0 {
			name += "-" + strconv.Itoa(i+1)
		}
		if branchExists(root, "refs/heads/"+name) || branchExists(root, "refs/remotes/origin/"+name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			continue
		}
		return name
	}
	return "worktree-" + newID()[:8]
}

func branchExists(root, ref string) bool {
	_, err := worktreeGit(root, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// worktreeRepoName keeps two repositories with the same folder name apart.
func worktreeRepoName(root string) string {
	base := strings.TrimSpace(filepath.Base(root))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, base)
	if base == "" {
		base = "repo"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(root))
	return fmt.Sprintf("%s-%08x", base, h.Sum32())
}

// worktreeGit runs git and returns stdout as text, sharing the drawer's
// deadline and error messages.
func worktreeGit(dir string, args ...string) (string, error) {
	out, err := gitOutput(dir, args...)
	return string(out), err
}

func ghOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

// worktreeIsClean refuses to open or merge a PR over changes the commit does
// not carry, which would otherwise be silently left behind.
func worktreeIsClean(wt *paneWorktree) (bool, error) {
	out, err := worktreeGit(wt.Path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

func commitsAhead(wt *paneWorktree) (int, error) {
	for _, base := range []string{"origin/" + wt.Base, wt.Base} {
		out, err := worktreeGit(wt.Path, "rev-list", "--count", base+"..HEAD")
		if err == nil {
			n, _ := strconv.Atoi(strings.TrimSpace(out))
			return n, nil
		}
	}
	return 0, fmt.Errorf("cannot find %s", wt.Base)
}

func currentBranch(dir string) string {
	out, err := worktreeGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(out)
	if branch == "HEAD" {
		return ""
	}
	return branch
}

func hasOriginRemote(dir string) bool {
	_, err := worktreeGit(dir, "remote", "get-url", "origin")
	return err == nil
}

func lookupPR(dir, branch string) (int, string, string, error) {
	out, err := ghOutput(dir, "pr", "view", branch, "--json", "number,url,state")
	if err != nil {
		return 0, "", "", err
	}
	var v struct {
		Number int
		URL    string
		State  string
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return 0, "", "", err
	}
	return v.Number, v.URL, strings.ToLower(v.State), nil
}

// createPR pushes the worktree branch and opens its pull request. When one is
// already open, it returns that instead. The branch is read from git, since
// the agent may have renamed it since the last turn ended.
func createPR(wt *paneWorktree) (int, string, string, string, error) {
	branch := currentBranch(wt.Path)
	if branch == "" {
		branch = wt.Branch
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return 0, "", "", branch, errors.New("gh is not installed")
	}
	// Local checks come before gh: a repository without a GitHub remote can
	// leave gh waiting on the network, and a missing commit is a fact git
	// already knows.
	clean, err := worktreeIsClean(wt)
	if err != nil {
		return 0, "", "", branch, err
	}
	if !clean {
		return 0, "", "", branch, errors.New("commit the changes first")
	}
	ahead, err := commitsAhead(wt)
	if err != nil {
		return 0, "", "", branch, err
	}
	if ahead == 0 {
		return 0, "", "", branch, errors.New("there are no commits to open a pull request with")
	}
	if hasOriginRemote(wt.Path) {
		if n, url, state, err := lookupPR(wt.Path, branch); err == nil && n != 0 {
			return n, url, state, branch, nil
		}
	}
	if _, err := worktreeGit(wt.Path, "push", "-u", "origin", branch); err != nil {
		return 0, "", "", branch, err
	}
	if _, err := ghOutput(wt.Path, "pr", "create", "--fill", "--base", wt.Base, "--head", branch); err != nil {
		return 0, "", "", branch, err
	}
	n, url, state, err := lookupPR(wt.Path, branch)
	return n, url, state, branch, err
}

func mergePR(wt *paneWorktree) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh is not installed")
	}
	clean, err := worktreeIsClean(wt)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("commit the changes first")
	}
	_, err = ghOutput(wt.Path, "pr", "merge", strconv.Itoa(wt.PR), "--merge")
	return err
}

// syncLocalBase brings the user's checkout of the base branch up to date after
// the PR merged on GitHub. When the checkout is on another branch, the local
// base ref moves instead.
func syncLocalBase(wt *paneWorktree) error {
	if _, err := worktreeGit(wt.Root, "fetch", "--quiet", "origin", wt.Base); err != nil {
		return err
	}
	if cur, err := worktreeGit(wt.Root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		if strings.TrimSpace(cur) == wt.Base {
			_, err := worktreeGit(wt.Root, "merge", "--ff-only", "origin/"+wt.Base)
			return err
		}
	}
	_, err := worktreeGit(wt.Root, "fetch", "--quiet", "origin", wt.Base+":"+wt.Base)
	return err
}

func removeWorktree(wt *paneWorktree) error {
	if _, err := worktreeGit(wt.Root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = worktreeGit(wt.Root, "worktree", "prune")
	_, _ = worktreeGit(wt.Root, "branch", "-D", wt.Branch)
	_, _ = worktreeGit(wt.Root, "push", "origin", "--delete", wt.Branch)
	return nil
}

// createPullRequest starts the PR in the background; the header button shows
// the operation while it runs.
func (w *workspace) createPullRequest(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.State == worktreeMerged || wt.PR != 0 {
		return
	}
	wt.busy = worktreeBusyPR
	w.invalidate()
	snapshot := *wt
	go func() {
		n, url, state, branch, err := createPR(&snapshot)
		w.onMain(func() {
			wt.busy = ""
			if err != nil {
				w.flash(locales.WorktreeError(err.Error()))
			} else {
				wt.PR, wt.URL, wt.State, wt.Branch = n, url, state, branch
				w.flash(locales.WorktreePROpened())
			}
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

// mergePullRequest merges the PR on GitHub, then fast-forwards the local base
// branch and closes the pane with its worktree. A local sync that fails leaves
// the worktree in place, so nothing is lost.
func (w *workspace) mergePullRequest(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.PR == 0 || wt.State == worktreeMerged {
		return
	}
	wt.busy = worktreeBusyMerge
	w.invalidate()
	snapshot := *wt
	go func() {
		if err := mergePR(&snapshot); err != nil {
			w.onMain(func() {
				wt.busy = ""
				w.flash(locales.WorktreeError(err.Error()))
				w.invalidate()
			})
			return
		}
		syncErr := syncLocalBase(&snapshot)
		w.onMain(func() {
			wt.busy = ""
			wt.State = worktreeMerged
			if syncErr != nil {
				w.flash(locales.WorktreeMergedLocalFailed(wt.Base, syncErr.Error()))
			} else {
				w.flash(locales.WorktreeMergedInto(wt.Base))
				w.cleanupMergedWorktree(p)
			}
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

// cleanupMergedWorktree hangs up the pane and any split that shares its
// checkout, then removes the checkout. The branch is already merged, so
// nothing is lost.
func (w *workspace) cleanupMergedWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil {
		return
	}
	s := w.sessionOf(p)
	if s != nil && s.Root != nil {
		for _, other := range s.Root.panes() {
			if !inWorktree(other, wt) {
				continue
			}
			if w.paneByID(other.ID) != nil {
				w.closePane(other)
			}
		}
	} else if w.paneByID(p.ID) != nil {
		w.closePane(p)
	}
	go func() {
		if err := removeWorktree(wt); err != nil {
			w.onMain(func() { w.flash(locales.WorktreeError(err.Error())) })
		}
	}()
}

func inWorktree(p *pane, wt *paneWorktree) bool {
	for _, dir := range []string{p.dir(), p.initialDir} {
		if dir == wt.Path || strings.HasPrefix(dir, wt.Path+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// refreshWorktree picks up a branch the agent renamed and, now and then, a PR
// state that changed outside Gexi.
func (w *workspace) refreshWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil {
		return
	}
	path := wt.Path
	go func() {
		out, err := worktreeGit(path, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return
		}
		branch := strings.TrimSpace(out)
		w.onMain(func() {
			if p.Worktree == nil || branch == "" || branch == "HEAD" || branch == p.Worktree.Branch {
				return
			}
			p.Worktree.Branch = branch
			p.Worktree.Naming = false
			w.invalidate()
			w.scheduleSave()
		})
	}()
	if wt.PR != 0 && wt.State != worktreeMerged && time.Since(wt.checked) > 30*time.Second {
		wt.checked = time.Now()
		prPath, prBranch := wt.Path, wt.Branch
		go func() {
			n, url, state, err := lookupPR(prPath, prBranch)
			if err != nil {
				return
			}
			w.onMain(func() {
				if wt.PR == 0 {
					return
				}
				wt.PR, wt.URL, wt.State = n, url, state
				w.invalidate()
				w.scheduleSave()
			})
		}()
	}
}

// createPaneWorktree makes the worktree the W button asks for, then restarts
// the pane in it. Git runs off the UI thread.
func (w *workspace) createPaneWorktree(p *pane) {
	if p.Worktree != nil || p.worktreeBusy != "" {
		return
	}
	s := w.sessionOf(p)
	pr := w.projectOf(s)
	if pr == nil || pr.RemoteHost != "" {
		return
	}
	if p.agent != nil {
		w.flash(locales.WorktreeAgentRunning())
		return
	}
	p.worktreeBusy = worktreeBusyCreate
	w.invalidate()
	dir := pr.Dir
	go func() {
		wt, err := createWorktree(dir)
		w.onMain(func() {
			if w.paneByID(p.ID) != p {
				// The pane closed while git worked.
				if wt != nil {
					go func() { _ = removeWorktree(wt) }()
				}
				return
			}
			p.worktreeBusy = ""
			switch {
			case err != nil:
				w.flash(locales.WorktreeError(err.Error()))
			case wt == nil:
				w.flash(locales.WorktreeNotRepo())
			default:
				w.restartPaneInWorktree(p, wt)
				return
			}
			w.invalidate()
		})
	}()
}

// restartPaneInWorktree swaps the pane's terminal for a fresh shell in the
// worktree, keeping its slot, id, and focus.
func (w *workspace) restartPaneInWorktree(p *pane, wt *paneWorktree) {
	s := w.sessionOf(p)
	if s == nil || s.Root == nil || w.paneByID(p.ID) != p {
		go func() { _ = removeWorktree(wt) }()
		return
	}
	if p.agent != nil {
		w.flash(locales.WorktreeAgentRunning())
		go func() { _ = removeWorktree(wt) }()
		return
	}
	np, err := newPane(w, p.ID, nil, wt.Path, "")
	if err != nil {
		w.flash(locales.WorktreeError(err.Error()))
		go func() { _ = removeWorktree(wt) }()
		return
	}
	np.Worktree = wt
	s.Root = s.Root.replacing(p.ID, leaf(np))
	p.close()
	w.FocusedPane = np.ID
	w.focusReq = np.ID
	w.flash(locales.WorktreeCreated())
	w.invalidate()
	w.scheduleSave()
}

// worktreeNamingContext is the one-time note that tells the agent its branch
// is a placeholder. Claude Code adds a UserPromptSubmit hook's stdout to the
// prompt, so this reaches the model with the first request.
func (w *workspace) worktreeNamingContext(p *pane) string {
	wt := p.Worktree
	if wt == nil || !wt.Naming {
		return ""
	}
	wt.Naming = false
	w.scheduleSave()
	return fmt.Sprintf(
		"Gexi workspace: this session runs in the git worktree %s on branch %q. "+
			"The branch name is a placeholder. Once the request is clear, rename it once "+
			"with `git branch -m <new-name>` to a short kebab-case name for this task "+
			"(for example `fix-login-redirect`), then continue; leave the worktree path unchanged.",
		wt.Path, wt.Branch)
}

// paneWorktreeButton is the W control in the pane header. W makes a worktree
// for the pane; once it has one, the same slot shows PR / Merge instead.
func (w *workspace) paneWorktreeButton(c *ui.Context, tk tokens, p *pane) {
	if p.Worktree == nil {
		if s := w.sessionOf(p); s != nil {
			if pr := w.projectOf(s); pr != nil && pr.RemoteHost != "" {
				return
			}
		}
		label := "W"
		action := func() { w.createPaneWorktree(p) }
		hint := locales.WorktreeCreateHint()
		busy := ""
		if p.worktreeBusy != "" {
			busy = locales.WorktreeCreating()
			action = func() {}
		}
		el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(6).
			Center().Cursor(ui.CursorPointer).Tooltip(hint)
		if el.Hovered() {
			el = el.Background(tk.Control)
		}
		el.OnClick(action).Children(func() {
			if busy != "" {
				worktreeSpinner(c, tk, busy)
				return
			}
			ui.Text(c, label).FontSize(11).FontWeight(600).
				TextColor(tk.Foreground).OnClick(action)
		})
		return
	}
	w.worktreeButton(c, tk, p)
}

// worktreeButton is the PR / Merge control in the pane header, left of the
// pane actions. It appears once the pane owns a worktree.
func (w *workspace) worktreeButton(c *ui.Context, tk tokens, p *pane) {
	wt := p.Worktree
	label := locales.WorktreePR()
	action := func() { w.createPullRequest(p) }
	color := tk.Foreground
	busy := ""
	switch {
	case wt.State == worktreeMerged:
		label = locales.WorktreeMerged()
		color = tk.MutedForeground
		action = func() {
			if wt.URL != "" {
				openPath(wt.URL)
			}
		}
	case wt.busy == worktreeBusyPR:
		busy = locales.WorktreePROpening()
		action = func() {}
	case wt.busy == worktreeBusyMerge:
		busy = locales.WorktreeMerging()
		action = func() {}
	case wt.PR != 0:
		label = locales.WorktreeMerge()
		action = func() { w.mergePullRequest(p) }
	}
	hint := wt.Branch + " → " + wt.Base
	if wt.URL != "" {
		hint = wt.URL
	}
	el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(6).
		Center().Cursor(ui.CursorPointer).Tooltip(hint)
	if el.Hovered() {
		el = el.Background(tk.Control)
	}
	el.OnClick(action).Children(func() {
		if busy != "" {
			worktreeSpinner(c, tk, busy)
			return
		}
		ui.Text(c, label).FontSize(11).FontWeight(600).
			TextColor(color).OnClick(action)
	})
}

// worktreeSpinner is a header button at work: the theme's spinner where the
// label was, so the wait reads as work rather than a truncated word. label
// names the operation for assistive technology.
func worktreeSpinner(c *ui.Context, tk tokens, label string) {
	ui.Spinner(c).Size(12, 12).TextColor(tk.MutedForeground).Label(label)
}
