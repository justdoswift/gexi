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
// renames it from the user's first request; the pane header then offers PR,
// which opens the pull request and a menu to merge it.

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

	// busy is the operation the header button waits on: "pr", "check", or
	// "merge". checked throttles the gh lookups when a session is selected.
	busy       string
	checked    time.Time
	mergeable  string
	review     string
	checks     string
	mergeState string
	// prPolls counts the refresh timers spent on a pull request that is not
	// ready yet; prTimer is the one waiting.
	prPolls int
	prTimer *time.Timer
}

const (
	worktreeBusyCreate = "create"
	worktreeBusyLeave  = "leave"
	worktreeBusyCheck  = "check"
	worktreeBusyPR     = "pr"
	worktreeBusyMerge  = "merge"
	worktreeMerged     = "merged"
)

// worktreeNames are the placeholder branch names: Dota 2 heroes, items, and
// abilities, distinctive enough never to be mistaken for a task.
var worktreeNames = []string{
	"anti-mage", "axe", "bane", "bloodseeker", "bounty-hunter", "brewmaster",
	"bristleback", "broodmother", "centaur-warrunner", "chaos-knight", "chen",
	"clinkz", "clockwerk", "crystal-maiden", "dark-seer", "dark-willow",
	"dawnbreaker", "dazzle", "death-prophet", "disruptor", "doom",
	"dragon-knight", "drow-ranger", "earth-spirit", "earthshaker",
	"elder-titan", "ember-spirit", "enchantress", "enigma", "faceless-void",
	"grimstroke", "gyrocopter", "hoodwink", "huskar", "invoker", "io",
	"jakiro", "juggernaut", "keeper-of-the-light", "kez", "kunkka",
	"legion-commander", "leshrac", "lich", "lifestealer", "lina", "lion",
	"lone-druid", "luna", "lycan", "magnus", "marci", "mars", "medusa",
	"meepo", "mirana", "monkey-king", "morphling", "muerta", "naga-siren",
	"natures-prophet", "necrophos", "night-stalker", "nyx-assassin",
	"ogre-magi", "omniknight", "oracle", "outworld-destroyer", "pangolier",
	"phantom-assassin", "phantom-lancer", "phoenix", "primal-beast", "puck",
	"pudge", "pugna", "queen-of-pain", "razor", "riki", "ringmaster",
	"rubick", "sand-king", "shadow-demon", "shadow-fiend", "shadow-shaman",
	"silencer", "skywrath-mage", "slardar", "slark", "snapfire", "sniper",
	"spectre", "spirit-breaker", "storm-spirit", "sven", "techies",
	"templar-assassin", "terrorblade", "tidehunter", "timbersaw", "tinker",
	"tiny", "treant-protector", "troll-warlord", "tusk", "underlord",
	"undying", "ursa", "vengeful-spirit", "venomancer", "viper", "visage",
	"void-spirit", "warlock", "weaver", "windranger", "winter-wyvern",
	"witch-doctor", "wraith-king", "zeus",

	"abyssal-blade", "aeon-disk", "aghanims-scepter", "assault-cuirass",
	"blink-dagger", "black-king-bar", "bloodthorn", "butterfly",
	"crimson-guard", "diffusal-blade", "divine-rapier", "echo-sabre",
	"euls-scepter", "force-staff", "ghost-scepter", "glimmer-cape",
	"guardian-greaves", "hand-of-midas", "harpoon", "heart-of-tarrasque",
	"hurricane-pike", "khanda", "lotus-orb", "mage-slayer", "manta-style",
	"orchid-malevolence", "phylactery", "pipe-of-insight", "radiance",
	"refresher-orb", "sange-and-yasha", "shivas-guard", "skadi",
	"travel-boots", "witch-blade",

	"black-hole", "chain-frost", "chronosphere", "culling-blade",
	"dream-coil", "echo-slam", "finger-of-death", "fissure", "frozen-touch",
	"global-silence", "laguna-blade", "macropyre", "meat-hook", "omnislash",
	"ravage", "requiem-of-souls", "reverse-polarity", "sonic-wave",
	"thundergods-wrath", "time-lock", "vac-wall", "wall-of-replica",
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

func randomWorktreeName() string {
	return worktreeNames[rand.IntN(len(worktreeNames))]
}

// uniqueWorktreeName avoids a local or remote branch, and a folder left by an
// older worktree.
func uniqueWorktreeName(root string) string {
	dir := filepath.Join(worktreesRoot(), worktreeRepoName(root))
	for i := 0; i < 60; i++ {
		name := randomWorktreeName()
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

// errGhMissing says the GitHub CLI, which the PR and Merge buttons drive, is
// not installed. It has its own toast, since it is the one failure the user
// can fix from here.
var errGhMissing = errors.New("gh is not installed")

// ghInstallURL is where GitHub CLI is downloaded, with the command for every
// package manager that installs it.
const ghInstallURL = "https://cli.github.com/"

// worktreeFailed reports a worktree operation's error. A missing gh gets a
// toast with a button to its install page, since gh's own absence says
// nothing about what to do.
func (w *workspace) worktreeFailed(err error) {
	if errors.Is(err, errGhMissing) {
		w.flashAction(locales.WorktreeGhMissing(), locales.WorktreeGhInstall(), func() {
			openPath(ghInstallURL)
		})
		return
	}
	w.flash(locales.WorktreeError(err.Error()))
}

// ghBinary is gh's path. An app opened from Finder inherits launchd's short
// PATH, which misses a gh installed beside the user's other tools, though the
// panes' login shell finds it; the usual install folders are asked as well.
var ghBinary = sync.OnceValue(func() string {
	if path, err := exec.LookPath("gh"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local/bin"),
		filepath.Join(home, "bin"),
	} {
		path := filepath.Join(dir, "gh")
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path
		}
	}
	return ""
})

func ghOutput(dir string, args ...string) (string, error) {
	gh := ghBinary()
	if gh == "" {
		return "", errGhMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, args...)
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

// prInfo is what the header needs to judge a pull request: the identity, and
// whether GitHub would merge it now.
type prInfo struct {
	Number     int
	URL        string
	State      string
	Mergeable  string
	Review     string
	MergeState string
	Checks     string // passing, pending, failing, none
}

type prCheck struct {
	Conclusion string
	Status     string
	State      string
}

func lookupPR(dir, branch string) (prInfo, error) {
	out, err := ghOutput(dir, "pr", "view", branch, "--json",
		"number,url,state,mergeable,reviewDecision,mergeStateStatus,statusCheckRollup")
	if err != nil {
		return prInfo{}, err
	}
	var v struct {
		Number            int
		URL               string
		State             string
		Mergeable         string
		ReviewDecision    string
		MergeStateStatus  string
		StatusCheckRollup []prCheck
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return prInfo{}, err
	}
	return prInfo{
		Number:     v.Number,
		URL:        v.URL,
		State:      strings.ToLower(v.State),
		Mergeable:  v.Mergeable,
		Review:     v.ReviewDecision,
		MergeState: v.MergeStateStatus,
		Checks:     summarizeChecks(v.StatusCheckRollup),
	}, nil
}

// summarizeChecks folds GitHub's check runs and status contexts into one
// state. A failure outranks a wait; no checks at all is not a blocker.
func summarizeChecks(checks []prCheck) string {
	failing, pending := false, false
	for _, c := range checks {
		state := strings.ToUpper(c.Conclusion)
		if state == "" {
			state = strings.ToUpper(c.State)
		}
		if state == "" {
			state = strings.ToUpper(c.Status)
		}
		switch state {
		case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
			failing = true
		case "PENDING", "EXPECTED", "QUEUED", "IN_PROGRESS", "WAITING", "REQUESTED":
			pending = true
		case "SUCCESS", "NEUTRAL", "SKIPPED", "COMPLETED":
		default:
			if state != "" {
				pending = true
			}
		}
	}
	switch {
	case failing:
		return "failing"
	case pending:
		return "pending"
	case len(checks) > 0:
		return "passing"
	default:
		return "none"
	}
}

func applyPRStatus(wt *paneWorktree, info prInfo) {
	wt.PR = info.Number
	wt.URL = info.URL
	wt.State = info.State
	wt.mergeable = info.Mergeable
	wt.review = info.Review
	wt.checks = info.Checks
	wt.mergeState = info.MergeState
}

// createPR pushes the worktree branch and opens its pull request. When one is
// already open, it returns that instead. The branch is read from git, since
// the agent may have renamed it since the last turn ended.
func createPR(wt *paneWorktree) (prInfo, string, error) {
	branch := currentBranch(wt.Path)
	if branch == "" {
		branch = wt.Branch
	}
	if ghBinary() == "" {
		return prInfo{}, branch, errGhMissing
	}
	// Local checks come before gh: a repository without a GitHub remote can
	// leave gh waiting on the network, and a missing commit is a fact git
	// already knows.
	clean, err := worktreeIsClean(wt)
	if err != nil {
		return prInfo{}, branch, err
	}
	if !clean {
		return prInfo{}, branch, errors.New("commit the changes first")
	}
	ahead, err := commitsAhead(wt)
	if err != nil {
		return prInfo{}, branch, err
	}
	if ahead == 0 {
		return prInfo{}, branch, errors.New("there are no commits to open a pull request with")
	}
	if hasOriginRemote(wt.Path) {
		if info, err := lookupPR(wt.Path, branch); err == nil && info.Number != 0 {
			return info, branch, nil
		}
	}
	if _, err := worktreeGit(wt.Path, "push", "-u", "origin", branch); err != nil {
		return prInfo{}, branch, err
	}
	if _, err := ghOutput(wt.Path, "pr", "create", "--fill", "--base", wt.Base, "--head", branch); err != nil {
		return prInfo{}, branch, err
	}
	info, err := lookupPR(wt.Path, branch)
	return info, branch, err
}

func mergePR(wt *paneWorktree) error {
	if ghBinary() == "" {
		return errGhMissing
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
	stopPRTimer(wt)
	if _, err := worktreeGit(wt.Root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = worktreeGit(wt.Root, "worktree", "prune")
	_, _ = worktreeGit(wt.Root, "branch", "-D", wt.Branch)
	_, _ = worktreeGit(wt.Root, "push", "origin", "--delete", wt.Branch)
	return nil
}

// removeWorktreeKeeping removes the checkout but leaves the branch when it
// holds commits that were never merged. The remote branch is never deleted.
func removeWorktreeKeeping(wt *paneWorktree, keepBranch bool) error {
	stopPRTimer(wt)
	if _, err := worktreeGit(wt.Root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = worktreeGit(wt.Root, "worktree", "prune")
	if !keepBranch {
		_, _ = worktreeGit(wt.Root, "branch", "-D", wt.Branch)
	}
	return nil
}

// worktreeSafeToDelete reports a checkout with nothing to lose: no
// uncommitted changes and no commits ahead of the base branch.
func worktreeSafeToDelete(wt *paneWorktree) bool {
	if wt == nil || wt.busy != "" {
		return false
	}
	clean, err := worktreeIsClean(wt)
	if err != nil || !clean {
		return false
	}
	ahead, err := commitsAhead(wt)
	return err == nil && ahead == 0
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
		info, branch, err := createPR(&snapshot)
		w.onMain(func() {
			wt.busy = ""
			if err != nil {
				w.worktreeFailed(err)
			} else {
				applyPRStatus(wt, info)
				wt.Branch = branch
				w.flash(locales.WorktreePROpened())
				w.schedulePRRefresh(p)
			}
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

// mergePullRequest merges the PR on GitHub and fast-forwards the local base
// branch. The pane and its worktree stay open: the header button becomes
// Archive, so clearing them away is the user's next step rather than a
// side effect of the merge.
func (w *workspace) mergePullRequest(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.PR == 0 || wt.State == worktreeMerged || w.paneByID(p.ID) != p {
		return
	}
	if reason := w.mergeBlocker(wt); reason != "" {
		w.flash(locales.WorktreeMergeBlocked(reason))
		return
	}
	stopPRTimer(wt)
	wt.busy = worktreeBusyMerge
	w.invalidate()
	snapshot := *wt
	go func() {
		if err := mergePR(&snapshot); err != nil {
			w.onMain(func() {
				wt.busy = ""
				w.worktreeFailed(err)
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
			}
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

// mergeBlocker says why GitHub would not merge the pull request now, in the
// user's language, or "" when it is ready.
func (w *workspace) mergeBlocker(wt *paneWorktree) string {
	switch wt.State {
	case "merged":
		return locales.WorktreeBlockedMerged()
	case "closed":
		return locales.WorktreeBlockedClosed()
	}
	switch strings.ToUpper(wt.mergeState) {
	case "BEHIND":
		return locales.WorktreeBlockedBehind()
	case "DIRTY":
		return locales.WorktreeBlockedConflicts()
	}
	switch strings.ToUpper(wt.mergeable) {
	case "CONFLICTING":
		return locales.WorktreeBlockedConflicts()
	case "", "UNKNOWN":
		return locales.WorktreeBlockedChecking()
	}
	switch strings.ToUpper(wt.review) {
	case "CHANGES_REQUESTED":
		return locales.WorktreeBlockedChanges()
	case "REVIEW_REQUIRED":
		return locales.WorktreeBlockedReview()
	}
	switch wt.checks {
	case "":
		return locales.WorktreeBlockedChecking()
	case "failing":
		return locales.WorktreeBlockedChecks()
	case "pending":
		return locales.WorktreeBlockedChecksPending()
	}
	if strings.ToUpper(wt.mergeState) == "BLOCKED" {
		return locales.WorktreeBlockedGeneric()
	}
	return ""
}

// requestMerge refreshes the pull request, then asks before merging. The
// button is muted while GitHub would refuse it, and a press re-reads the
// status, so a check that finished since the last look still lets it through.
func (w *workspace) requestMerge(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.PR == 0 || wt.State != "open" {
		return
	}
	wt.busy = worktreeBusyCheck
	w.invalidate()
	path, branch := wt.Path, wt.Branch
	go func() {
		info, err := lookupPR(path, branch)
		w.onMain(func() {
			wt.busy = ""
			if err != nil {
				w.worktreeFailed(err)
				w.invalidate()
				return
			}
			applyPRStatus(wt, info)
			if reason := w.mergeBlocker(wt); reason != "" {
				w.flash(locales.WorktreeMergeBlocked(reason))
				w.schedulePRRefresh(p)
				w.invalidate()
				w.scheduleSave()
				return
			}
			w.mergeTarget = p
			w.mergeMsg = locales.WorktreeMergeConfirm(strconv.Itoa(wt.PR), wt.Base)
			w.mergeOpen = true
			w.invalidate()
		})
	}()
}

// archiveMergedWorktree hangs up the pane and any split that shares its
// checkout, then removes the checkout. The branch is already merged, so
// nothing is lost. The header offers this once the PR is merged instead of
// running it on its own.
func (w *workspace) archiveMergedWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.State != worktreeMerged {
		return
	}
	stopPRTimer(wt)
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
// state that changed outside gexi.
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
			info, err := lookupPR(prPath, prBranch)
			if err != nil {
				return
			}
			w.onMain(func() {
				if wt.PR == 0 {
					return
				}
				applyPRStatus(wt, info)
				w.invalidate()
				w.scheduleSave()
				w.schedulePRRefresh(p)
			})
		}()
	}
}

// schedulePRRefresh keeps a pull request that is not ready yet up to date:
// checks finish and reviews land without the user reselecting the session.
// It stops once the pull request is ready, and after twenty minutes of polls.
func (w *workspace) schedulePRRefresh(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.State != "open" || wt.prPolls >= 40 {
		stopPRTimer(wt)
		return
	}
	wt.prPolls++
	// The first two looks catch checks that register just after the pull
	// request opens; after that only one that is not ready keeps polling.
	delay := 30 * time.Second
	switch wt.prPolls {
	case 1:
		delay = 5 * time.Second
	case 2:
		delay = 20 * time.Second
	default:
		if w.mergeBlocker(wt) == "" {
			stopPRTimer(wt)
			return
		}
	}
	if wt.prTimer != nil {
		wt.prTimer.Stop()
	}
	wt.prTimer = time.AfterFunc(delay, func() {
		w.onMain(func() {
			if p.Worktree != wt || w.paneByID(p.ID) != p {
				return
			}
			wt.checked = time.Time{}
			w.refreshWorktree(p)
		})
	})
}

func stopPRTimer(wt *paneWorktree) {
	if wt != nil && wt.prTimer != nil {
		wt.prTimer.Stop()
		wt.prTimer = nil
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
	if w.restartPaneInDir(p, wt.Path, wt, locales.WorktreeCreated()) {
		return
	}
	go func() { _ = removeWorktree(wt) }()
}

// restartPaneInDir swaps the pane's terminal for a fresh shell in dir,
// keeping its slot, id, and focus. It reports whether the pane restarted.
func (w *workspace) restartPaneInDir(p *pane, dir string, wt *paneWorktree, message string) bool {
	s := w.sessionOf(p)
	if s == nil || s.Root == nil || w.paneByID(p.ID) != p {
		return false
	}
	if p.agent != nil {
		w.flash(locales.WorktreeAgentRunning())
		return false
	}
	np, err := newPane(w, p.ID, nil, dir, "")
	if err != nil {
		w.flash(locales.WorktreeError(err.Error()))
		return false
	}
	np.Worktree = wt
	s.Root = s.Root.replacing(p.ID, leaf(np))
	p.close()
	w.FocusedPane = np.ID
	w.focusReq = np.ID
	if message != "" {
		w.flash(message)
	}
	w.invalidate()
	w.scheduleSave()
	return true
}

// promptLeaveWorktree asks before deleting a checkout that holds uncommitted
// changes or commits that were never merged. A checkout with nothing to lose
// goes straight back to the project.
func (w *workspace) promptLeaveWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil || p.worktreeBusy != "" || wt.busy != "" {
		return
	}
	if p.agent != nil {
		w.flash(locales.WorktreeAgentRunning())
		return
	}
	p.worktreeBusy = worktreeBusyLeave
	w.invalidate()
	go func() {
		clean, _ := worktreeIsClean(wt)
		ahead, _ := commitsAhead(wt)
		w.onMain(func() {
			p.worktreeBusy = ""
			if w.paneByID(p.ID) != p {
				w.invalidate()
				return
			}
			if clean && ahead == 0 {
				w.leaveWorktree(p)
				return
			}
			msg := ""
			if ahead > 0 {
				msg = locales.WorktreeLeaveUnmerged(strconv.Itoa(ahead), wt.Branch)
			}
			if !clean {
				if msg != "" {
					msg += "\n\n"
				}
				msg += locales.WorktreeLeaveDirty()
			}
			w.leaveTarget = p
			w.leaveMsg = msg
			w.leaveOpen = true
			w.invalidate()
		})
	}()
}

// leaveWorktree deletes the checkout and restarts the pane in the project
// folder. A branch with commits that were never merged is kept.
func (w *workspace) leaveWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil {
		return
	}
	s := w.sessionOf(p)
	pr := w.projectOf(s)
	if pr == nil {
		return
	}
	stopPRTimer(wt)
	dir := pr.Dir
	p.worktreeBusy = worktreeBusyLeave
	w.invalidate()
	go func() {
		ahead, _ := commitsAhead(wt)
		w.onMain(func() {
			if w.paneByID(p.ID) != p {
				return
			}
			p.worktreeBusy = ""
			if !w.restartPaneInDir(p, dir, nil, locales.WorktreeLeft()) {
				w.invalidate()
				return
			}
			// Splits opened inside the checkout would be left with a
			// deleted folder; close them with it.
			if s != nil && s.Root != nil {
				for _, other := range s.Root.panes() {
					if other.ID == p.ID || !inWorktree(other, wt) {
						continue
					}
					if w.paneByID(other.ID) != nil {
						w.closePane(other)
					}
				}
			}
			go func() {
				if err := removeWorktreeKeeping(wt, ahead > 0); err != nil {
					w.onMain(func() { w.flash(locales.WorktreeError(err.Error())) })
				}
			}()
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

// worktreeLeaveButton returns the pane to the project folder.
func (w *workspace) worktreeLeaveButton(c *ui.Context, tk tokens, p *pane) {
	if p.Worktree == nil {
		return
	}
	action := func() { w.promptLeaveWorktree(p) }
	hint := locales.WorktreeLeaveHint()
	el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(6).
		Center().Cursor(ui.CursorPointer).Tooltip(hint)
	if el.Hovered() {
		el = el.Background(tk.Control)
	}
	el.OnClick(action).Children(func() {
		if p.worktreeBusy == worktreeBusyLeave {
			worktreeSpinner(c, tk, locales.WorktreeLeaving())
			return
		}
		ui.Text(c, "↩").FontSize(12).
			TextColor(tk.MutedForeground).OnClick(action)
	})
}

// cleanupClosedWorktree removes a clean checkout with nothing ahead when its
// last pane closes. Anything with changes or unmerged commits stays, so
// closing a pane never loses work.
func (w *workspace) cleanupClosedWorktree(wt *paneWorktree) {
	if wt == nil {
		return
	}
	stopPRTimer(wt)
	go func() {
		if worktreeSafeToDelete(wt) {
			if err := removeWorktreeKeeping(wt, false); err != nil {
				w.onMain(func() { w.flash(locales.WorktreeError(err.Error())) })
			}
			return
		}
		w.onMain(func() { w.flash(locales.WorktreeKeptClose(wt.Path)) })
	}()
}

// cleanupOrphanWorktrees removes checkouts under the app's worktree folder
// that no pane uses and that have nothing to lose. The manual run reports
// even when it found nothing.
func (w *workspace) cleanupOrphanWorktrees(manual bool) {
	referenced := map[string]bool{}
	for _, p := range w.allPanes() {
		if p.Worktree != nil {
			referenced[filepath.Clean(p.Worktree.Path)] = true
		}
	}
	// A split pane inside a checkout has no worktree of its own; its folder
	// still marks the checkout as used.
	used := func(path string) bool {
		for _, p := range w.allPanes() {
			for _, dir := range []string{p.dir(), p.initialDir} {
				if dir == path || strings.HasPrefix(dir, path+string(os.PathSeparator)) {
					return true
				}
			}
		}
		return false
	}
	root := worktreesRoot()
	repos, err := os.ReadDir(root)
	if err != nil {
		if manual {
			w.flash(locales.WorktreeCleanupNothing())
		}
		return
	}
	var candidates []string
	for _, repo := range repos {
		if !repo.IsDir() {
			continue
		}
		names, err := os.ReadDir(filepath.Join(root, repo.Name()))
		if err != nil {
			continue
		}
		for _, name := range names {
			if !name.IsDir() {
				continue
			}
			path := filepath.Join(root, repo.Name(), name.Name())
			if referenced[filepath.Clean(path)] || used(path) {
				continue
			}
			candidates = append(candidates, path)
		}
	}
	if len(candidates) == 0 {
		if manual {
			w.flash(locales.WorktreeCleanupNothing())
		}
		return
	}
	go func() {
		removed := 0
		for _, path := range candidates {
			if orphanWorktreeSafe(path) && removeOrphanWorktree(path) == nil {
				removed++
			}
		}
		w.onMain(func() {
			switch {
			case removed > 0:
				w.flash(locales.WorktreeCleanupDone(strconv.Itoa(removed)))
			case manual:
				w.flash(locales.WorktreeCleanupNothing())
			}
		})
	}()
}

// worktreeRepoRoot is the main checkout of the repository a worktree belongs
// to, found through the shared git directory.
func worktreeRepoRoot(path string) (string, bool) {
	out, err := worktreeGit(path, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", false
	}
	common := strings.TrimSpace(out)
	if common == "" {
		return "", false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	root := filepath.Dir(filepath.Clean(common))
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", false
	}
	return root, true
}

// orphanWorktreeSafe reports a checkout with no session that has nothing to
// lose: clean, and no commits ahead of its base branch.
func orphanWorktreeSafe(path string) bool {
	out, err := worktreeGit(path, "status", "--porcelain")
	if err != nil || strings.TrimSpace(out) != "" {
		return false
	}
	root, ok := worktreeRepoRoot(path)
	if !ok {
		return false
	}
	base := detectBaseBranch(root)
	if base == "" {
		return false
	}
	ahead, err := commitsAhead(&paneWorktree{Path: path, Base: base})
	return err == nil && ahead == 0
}

func removeOrphanWorktree(path string) error {
	root, ok := worktreeRepoRoot(path)
	if !ok {
		return errors.New("not a worktree")
	}
	branch := currentBranch(path)
	if _, err := worktreeGit(root, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, _ = worktreeGit(root, "worktree", "prune")
	if branch != "" {
		_, _ = worktreeGit(root, "branch", "-D", branch)
	}
	_ = os.Remove(filepath.Dir(path))
	return nil
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
		"gexi workspace: this session runs in the git worktree %s on branch %q. "+
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
	w.worktreeLeaveButton(c, tk, p)
}

// worktreeButton is the PR control in the pane header, left of the pane
// actions. The first press opens a pull request. Once one exists, PR opens
// the page and a separate Merge button appears; Merge asks first and is muted
// while GitHub would not merge it. After the merge it becomes Archive, which
// clears the pane and its checkout away.
func (w *workspace) worktreeButton(c *ui.Context, tk tokens, p *pane) {
	wt := p.Worktree
	if wt.State == worktreeMerged {
		action := func() { w.archiveMergedWorktree(p) }
		worktreeChip(c, tk).Tooltip(locales.WorktreeArchiveHint()).OnClick(action).Children(func() {
			ui.Text(c, locales.WorktreeArchive()).FontSize(11).FontWeight(600).
				TextColor(tk.Foreground).OnClick(action)
		})
		return
	}
	if busy := worktreeBusyLabel(wt); busy != "" {
		worktreeChip(c, tk).OnClick(func() {}).Children(func() {
			worktreeSpinner(c, tk, busy)
		})
		return
	}
	if wt.PR == 0 {
		action := func() { w.createPullRequest(p) }
		worktreeChip(c, tk).Tooltip(wt.Branch + " → " + wt.Base).OnClick(action).Children(func() {
			ui.Text(c, locales.WorktreePR()).FontSize(11).FontWeight(600).
				TextColor(tk.Foreground).OnClick(action)
		})
		return
	}
	w.pullRequestChip(c, tk, p)
	if wt.State == "open" {
		w.mergeChip(c, tk, p)
	}
}

func worktreeChip(c *ui.Context, tk tokens) ui.Element {
	el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(6).
		Center().Cursor(ui.CursorPointer)
	if el.Hovered() {
		el = el.Background(tk.Control)
	}
	return el
}

func worktreeBusyLabel(wt *paneWorktree) string {
	switch wt.busy {
	case worktreeBusyPR:
		return locales.WorktreePROpening()
	case worktreeBusyCheck:
		return locales.WorktreeCheckingPR()
	case worktreeBusyMerge:
		return locales.WorktreeMerging()
	}
	return ""
}

// pullRequestChip shows the number and opens the page, as Conductor's #8376 ↗.
func (w *workspace) pullRequestChip(c *ui.Context, tk tokens, p *pane) {
	wt := p.Worktree
	action := func() {
		w.refreshWorktree(p)
		if wt.URL != "" {
			openPath(wt.URL)
		}
	}
	el := worktreeChip(c, tk).Tooltip(prStatusHint(wt)).OnClick(action)
	el.Children(func() {
		ui.Text(c, "#"+strconv.Itoa(wt.PR)).FontSize(11).FontWeight(600).
			TextColor(tk.Foreground).OnClick(action)
		ui.Text(c, "↗").FontSize(10).TextColor(tk.MutedForeground).OnClick(action)
	})
}

// mergeStatus is the short label Conductor puts beside the pull request:
// ready, or the first thing standing in the way.
func (w *workspace) mergeStatus(wt *paneWorktree) (label string, ready, danger bool) {
	switch wt.State {
	case "merged":
		return locales.WorktreeMerged(), false, false
	case "closed":
		return locales.WorktreeStatusClosed(), false, false
	}
	switch {
	case strings.EqualFold(wt.mergeState, "BEHIND"):
		return locales.WorktreeStatusBehind(), false, false
	case strings.EqualFold(wt.mergeable, "CONFLICTING") || strings.EqualFold(wt.mergeState, "DIRTY"):
		return locales.WorktreeStatusConflicts(), false, true
	case strings.EqualFold(wt.review, "CHANGES_REQUESTED"):
		return locales.WorktreeStatusChanges(), false, true
	case strings.EqualFold(wt.review, "REVIEW_REQUIRED"):
		return locales.WorktreeStatusReview(), false, false
	case wt.checks == "failing":
		return locales.WorktreeStatusChecks(), false, true
	case wt.checks == "pending":
		return locales.WorktreeStatusChecksPending(), false, false
	case wt.checks == "":
		return locales.WorktreeStatusChecking(), false, false
	}
	switch strings.ToUpper(wt.mergeable) {
	case "", "UNKNOWN":
		return locales.WorktreeStatusChecking(), false, false
	}
	if strings.EqualFold(wt.mergeState, "BLOCKED") {
		return locales.WorktreeStatusBlocked(), false, false
	}
	return locales.WorktreeStatusReady(), true, false
}

// mergeChip is Conductor's filled Merge button: it asks first, and stays
// disabled while GitHub would refuse the merge. A press refreshes the status
// first, so a check that finished since the last look still lets it through.
func (w *workspace) mergeChip(c *ui.Context, tk tokens, p *pane) {
	wt := p.Worktree
	label, ready, danger := w.mergeStatus(wt)
	// The button's colour is the status: green can merge, amber is still
	// checking, red is blocked. No separate dot or label is needed.
	bg := tk.Waiting
	if ready {
		bg = tk.Working
	} else if danger {
		bg = tk.Danger
	}
	fg := tk.Background
	blocker := w.mergeBlocker(wt)
	hint := label
	if blocker != "" {
		hint = locales.WorktreeMergeBlocked(blocker)
	}
	action := func() { w.requestMerge(p) }
	el := ui.Row(c).Height(22).Padding(0, 9, 0, 9).Radius(6).
		Center().Background(bg).Cursor(ui.CursorPointer).Tooltip(hint)
	el.OnClick(action).Children(func() {
		ui.Text(c, locales.WorktreeMerge()).FontSize(11).FontWeight(600).
			TextColor(fg).OnClick(action)
	})
}

func prStatusHint(wt *paneWorktree) string {
	parts := []string{}
	if wt.checks != "" {
		parts = append(parts, "checks: "+wt.checks)
	}
	if wt.review != "" {
		parts = append(parts, "review: "+strings.ToLower(wt.review))
	}
	if wt.mergeable != "" {
		parts = append(parts, "mergeable: "+strings.ToLower(wt.mergeable))
	}
	if wt.mergeState != "" {
		parts = append(parts, "state: "+strings.ToLower(wt.mergeState))
	}
	hint := strings.Join(parts, " · ")
	if hint == "" {
		hint = wt.Branch + " → " + wt.Base
	}
	if wt.URL != "" {
		hint = wt.URL + "\n" + hint
	}
	return hint
}

// worktreeSpinner is a header button at work: the theme's spinner where the
// label was, so the wait reads as work rather than a truncated word. label
// names the operation for assistive technology.
func worktreeSpinner(c *ui.Context, tk tokens, label string) {
	ui.Spinner(c).Size(12, 12).TextColor(tk.MutedForeground).Label(label)
}
