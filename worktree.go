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
	// Carried is the project folder whose uncommitted changes moved here
	// when the pane's agent was stopped for the move. The first prompt
	// mentions it once, then it is cleared.
	Carried string `json:"carried,omitempty"`
	// Detected marks a checkout gexi found by its pane's folder rather than
	// made itself. The header offers its pull request, but nothing here
	// leaves, archives, or deletes a checkout gexi does not own.
	Detected bool `json:"detected,omitempty"`
	// MergedHead is the branch's commit when its pull request merged. A
	// commit made afterwards is new work, and the header goes back to PR
	// instead of offering to archive that work away.
	MergedHead string `json:"mergedHead,omitempty"`

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
	worktreeBusyCreate  = "create"
	worktreeBusyLeave   = "leave"
	worktreeBusyCheck   = "check"
	worktreeBusyPR      = "pr"
	worktreeBusyMerge   = "merge"
	worktreeBusyResolve = "resolve"
	worktreeMerged      = "merged"
)

// reopenCheckInterval is how often a merged checkout is looked at for work
// that landed after the merge; the header returns to PR as soon as there is
// any.
const reopenCheckInterval = 5 * time.Second

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

// worktreePaneName reports the name of a checkout gexi made itself, as the
// pane header shows it. gexi puts its worktrees at
// <support>/worktrees/<repo>/<name>, so only a folder exactly that deep
// counts: a checkout found elsewhere, or a folder inside one, keeps its path.
func worktreePaneName(dir string) (string, bool) {
	root := filepath.Clean(worktreesRoot())
	rel, err := filepath.Rel(root, filepath.Clean(dir))
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
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

// errWorktreeGone says the checkout a pane's worktree names was deleted.
// Git would only answer with "cannot change to ..." there, so the PR and
// merge cores stop on this instead.
var errWorktreeGone = errors.New("the worktree folder is gone")

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
	if errors.Is(err, errWorktreeGone) {
		w.flash(locales.WorktreeGoneFailed())
		return
	}
	w.flash(locales.WorktreeError(err.Error()))
}

// worktreeCheckoutExists reports whether the folder a pane's worktree lives
// in is still on disk. A checkout deleted under the running app leaves the
// pane pointing at nothing, and every git call there fails with a raw
// "cannot change to ..." exit status.
func worktreeCheckoutExists(wt *paneWorktree) bool {
	if wt == nil || wt.Path == "" {
		return false
	}
	info, err := os.Stat(wt.Path)
	return err == nil && info.IsDir()
}

// worktreeReady reports whether a pane's worktree can still run git. A
// binding whose checkout was deleted while gexi ran is cleared here, so the
// header stops offering actions that cannot work and the pane can start a
// new worktree.
func (w *workspace) worktreeReady(p *pane) bool {
	if p == nil || p.Worktree == nil {
		return false
	}
	if worktreeCheckoutExists(p.Worktree) {
		return true
	}
	w.dropGoneWorktree(p)
	return false
}

// dropGoneWorktree forgets a worktree whose checkout was deleted under the
// running app, and points the user at Settings › Worktrees.
func (w *workspace) dropGoneWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil {
		return
	}
	stopPRTimer(wt)
	p.Worktree = nil
	p.worktreeBusy = ""
	w.worktreesScanned = false
	w.scheduleSave()
	w.invalidate()
	w.flashAction(locales.WorktreeGoneNotice(abbreviate(wt.Path)), locales.SettingsWorktrees(), func() {
		w.settingsTab = settingsWorktrees
		w.openSettings()
	})
}

// ghBinaryOnce is gh's path. An app opened from Finder inherits launchd's
// short PATH, which misses a gh installed beside the user's other tools,
// though the panes' login shell finds it; the usual install folders are asked
// as well.
var ghBinaryOnce = sync.OnceValue(func() string {
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

// ghBinaryOverride lets a test point gh at a script, or at nothing. It is
// nil in the app.
var ghBinaryOverride func() (string, bool)

func ghBinary() string {
	if ghBinaryOverride != nil {
		if path, ok := ghBinaryOverride(); ok {
			return path
		}
	}
	return ghBinaryOnce()
}

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

// createPR pushes the worktree branch and opens its pull request. When one
// is already open, it returns that instead. message commits the pending
// changes under it; an empty one there is an error.
func createPR(wt *paneWorktree, message string) (prInfo, string, error) {
	if wt == nil {
		return prInfo{}, "", errWorktreeGone
	}
	branch := currentBranch(wt.Path)
	if branch == "" {
		branch = wt.Branch
	}
	if !worktreeCheckoutExists(wt) {
		return prInfo{}, branch, errWorktreeGone
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
		if strings.TrimSpace(message) == "" {
			return prInfo{}, branch, errors.New("a commit message is needed")
		}
		if _, err := worktreeGit(wt.Path, "add", "-A"); err != nil {
			return prInfo{}, branch, err
		}
		if err := commitStaged(wt, message); err != nil {
			return prInfo{}, branch, err
		}
	}
	ahead, err := commitsAhead(wt)
	if err != nil {
		return prInfo{}, branch, err
	}
	if ahead == 0 {
		return prInfo{}, branch, errors.New("there are no commits to open a pull request with")
	}
	if hasOriginRemote(wt.Path) {
		// A merged pull request still answers for the branch: only one that
		// is open is reused, or the next press would return the old page
		// instead of opening the next pull request.
		if info, err := lookupPR(wt.Path, branch); err == nil && info.Number != 0 && info.State == "open" {
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

func commitStaged(wt *paneWorktree, msg string) error {
	if _, err := worktreeGit(wt.Path, "commit", "-m", msg); err != nil {
		if clean, _ := worktreeIsClean(wt); clean {
			return nil
		}
		return err
	}
	return nil
}

// commitPrompt asks for one line, the shape the agent and the manual prompt
// both use.
func commitPrompt(diff string) string {
	return "Write one git commit message for the staged diff below. " +
		"Use the imperative mood, keep it under 72 characters, write it in English, " +
		"and output only the message, with no quotes or markdown.\n\n" + diff
}

func cleanCommitMessage(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(strings.Trim(s, "`\"'"))
	lower := strings.ToLower(s)
	for _, prefix := range []string{"commit message:", "message:", "commit:"} {
		if strings.HasPrefix(lower, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			lower = strings.ToLower(s)
		}
	}
	if r := []rune(s); len(r) > 100 {
		s = strings.TrimSpace(string(r[:100]))
	}
	return s
}

// agentHeadless is the command that runs kind's agent once, reading the
// prompt from stdin and printing only its answer. The agents without a print
// mode gexi knows keep the manual dialog.
func agentHeadless(kind agentKind) ([]string, bool) {
	switch kind {
	case agentClaude:
		// JSON keeps the answer separate from anything Claude Code prints
		// around it, so a warning cannot become the commit message.
		return []string{"claude", "-p", "--output-format", "json"}, true
	case agentCodex:
		return []string{"codex", "exec", "-"}, true
	case agentPerch:
		// Perch takes the prompt as an argument, not on stdin, so runAgent
		// appends it. A thread of gexi's own keeps these one-shot questions
		// out of the conversation the user is having in the pane.
		return []string{"perch", "run", "--json", "--thread", "gexi-worktree"}, true
	case agentPi:
		// Print mode puts the piped prompt first and writes only the final
		// answer to stdout. --no-session keeps the one-shot out of the
		// conversation --continue would bring back.
		return []string{"pi", "-p", "--no-session"}, true
	}
	return nil, false
}

// agentCommitMessage asks the same agent the pane already runs for one line,
// through its own binary rather than a service of gexi's: the user's login
// and model write the message, and gexi stores no provider key.
func agentCommitMessage(kind agentKind, dir, prompt string) (string, error) {
	command, ok := agentHeadless(kind)
	if !ok {
		return "", fmt.Errorf("%s cannot write one automatically", kind.name())
	}
	answer, err := runAgent(kind, command, dir, prompt, 4*time.Minute)
	if err != nil {
		return "", err
	}
	msg := cleanCommitMessage(answer)
	if kind == agentClaude {
		var result struct {
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if err := json.Unmarshal([]byte(answer), &result); err == nil {
			if result.IsError {
				return "", fmt.Errorf("%s: %s", kind.name(), cleanCommitMessage(result.Result))
			}
			msg = cleanCommitMessage(result.Result)
		}
	}
	if kind == agentPerch {
		var result struct {
			OK            bool   `json:"ok"`
			AssistantText string `json:"assistantText"`
			Error         string `json:"error"`
		}
		if err := json.Unmarshal([]byte(answer), &result); err == nil {
			if !result.OK {
				return "", fmt.Errorf("%s: %s", kind.name(), cleanCommitMessage(result.Error))
			}
			msg = cleanCommitMessage(result.AssistantText)
		}
	}
	if msg == "" {
		return "", fmt.Errorf("%s returned nothing", kind.name())
	}
	return msg, nil
}

// runAgent runs one agent command with prompt on stdin, in dir, and returns
// its stdout. A headless agent has no one to answer a permission prompt, so
// the caller passes the flags its task needs.
func runAgent(kind agentKind, command []string, dir, prompt string, timeout time.Duration) (string, error) {
	path := agentBinary(command[0])
	if path == "" {
		return "", fmt.Errorf("%s is not on the PATH", kind.name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := command[1:]
	stdin := prompt
	if kind == agentPerch {
		// Perch reads the task from its arguments; `perch run -` does not
		// read stdin. The prompt goes last so the flags stay where they are.
		args = append(append([]string(nil), args...), prompt)
		stdin = ""
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = withPath(interactivePath(), cleanEnvironment(os.Environ()))
	cmd.Stdin = strings.NewReader(stdin)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timed out", kind.name())
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		if line, _, _ := strings.Cut(message, "\n"); line != "" {
			message = line
		}
		return "", fmt.Errorf("%s: %s", kind.name(), message)
	}
	return out.String(), nil
}

// agentResolveCommand is what runs kind's agent with permission to edit the
// working tree, for a task no permission prompt can interrupt.
func agentResolveCommand(kind agentKind) ([]string, bool) {
	switch kind {
	case agentClaude:
		return []string{
			"claude", "-p", "--output-format", "json",
			"--permission-mode", "acceptEdits",
			"--allowedTools", "Read,Edit,Write,Grep,Glob",
		}, true
	case agentCodex:
		return []string{"codex", "exec", "-s", "workspace-write", "-"}, true
	case agentPerch:
		// take_the_wheel is the mode that edits without stopping to ask, which
		// is what a conflict resolution needs: nothing is there to answer a
		// prompt. The prompt itself goes on the command line, as runAgent does.
		return []string{"perch", "run", "--json", "--thread", "gexi-worktree", "--permission", "take_the_wheel"}, true
	}
	return nil, false
}

// agentResolveConflicts asks the pane's agent to settle the files git left in
// conflict. gexi does the git work around it; the agent only edits the files,
// so nothing it does can commit, push, or touch another branch.
func agentResolveConflicts(kind agentKind, dir string, files []string) error {
	command, ok := agentResolveCommand(kind)
	if !ok {
		return fmt.Errorf("%s cannot resolve conflicts automatically", kind.name())
	}
	prompt := "This git working tree has unresolved merge conflicts in these files:\n\n" +
		strings.Join(files, "\n") +
		"\n\nEdit only these files. Remove every conflict marker (<<<<<<<, =======, >>>>>>>) " +
		"and combine both sides so the result is coherent. Do not run git commands, do not commit, " +
		"and do not change unrelated files. When done, reply with one short line saying what you combined."
	_, err := runAgent(kind, command, dir, prompt, 5*time.Minute)
	return err
}

// agentBinary finds an agent the way the panes do: the login shell's PATH
// first, then launchd's, since an app opened from Finder cannot see a tool
// installed under the user's home.
func agentBinary(name string) string {
	for _, dir := range filepath.SplitList(mergePaths(interactivePath(), os.Getenv("PATH"))) {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path
		}
	}
	return ""
}

func mergePR(wt *paneWorktree) error {
	if ghBinary() == "" {
		return errGhMissing
	}
	if !worktreeCheckoutExists(wt) {
		return errWorktreeGone
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

// mergeConflicting reports whether GitHub refuses the merge because the
// branch and its base changed the same lines.
func mergeConflicting(mergeable, state string) bool {
	return strings.EqualFold(mergeable, "CONFLICTING") || strings.EqualFold(state, "DIRTY")
}

func mergeConflicts(wt *paneWorktree) bool {
	return wt != nil && mergeConflicting(wt.mergeable, wt.mergeState)
}

// canResolveConflicts reports a pane whose agent can settle the conflict.
func canResolveConflicts(p *pane) bool {
	if p == nil || p.agent == nil {
		return false
	}
	_, ok := agentResolveCommand(p.agent.Kind)
	return ok
}

// resolveConflictsAndMerge settles the conflict in the checkout with the
// pane's own agent, pushes the result, and merges once GitHub sees it.
func (w *workspace) resolveConflictsAndMerge(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.PR == 0 || wt.State != "open" ||
		!canResolveConflicts(p) || w.paneByID(p.ID) != p {
		return
	}
	stopPRTimer(wt)
	wt.busy = worktreeBusyResolve
	w.invalidate()
	snapshot := *wt
	kind := p.agent.Kind
	go func() {
		if err := resolveConflicts(&snapshot, kind); err != nil {
			w.onMain(func() {
				wt.busy = ""
				w.flash(locales.WorktreeResolveFailed(err.Error()))
				w.invalidate()
				w.scheduleSave()
			})
			return
		}
		info, err := waitMergeable(snapshot.Path, snapshot.Branch)
		w.onMain(func() {
			wt.busy = ""
			if err != nil {
				w.worktreeFailed(err)
				w.invalidate()
				w.scheduleSave()
				return
			}
			applyPRStatus(wt, info)
			if mergeConflicts(wt) {
				// GitHub had yet to recompute when the wait ran out; the
				// conflict is resolved, so another press can go through.
				w.flash(locales.WorktreeResolvePending())
				w.schedulePRRefresh(p)
			} else if reason := w.mergeBlocker(wt); reason != "" {
				w.flash(locales.WorktreeMergeBlocked(reason))
				w.schedulePRRefresh(p)
			} else {
				w.mergePullRequest(p)
			}
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

// resolveConflicts brings the base into the branch, lets the agent settle what
// git could not, commits the merge, and pushes it. A failed resolution aborts
// the merge, so the checkout goes back to how it was.
func resolveConflicts(wt *paneWorktree, kind agentKind) error {
	clean, err := worktreeIsClean(wt)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("commit the changes first")
	}
	if _, err := worktreeGit(wt.Path, "fetch", "--quiet", "origin", wt.Base); err != nil {
		return err
	}
	base := "origin/" + wt.Base
	var files []string
	if _, mergeErr := worktreeGit(wt.Path, "merge", "--no-edit", base); mergeErr == nil {
		return pushBranch(wt)
	} else if files, err = conflictedFiles(wt.Path); err != nil {
		return err
	} else if len(files) == 0 {
		return mergeErr
	} else if err = agentResolveConflicts(kind, wt.Path, files); err != nil {
		abortMerge(wt)
		return err
	}
	if err = stageResolution(wt.Path, kind, files); err != nil {
		abortMerge(wt)
		return err
	}
	if _, err := worktreeGit(wt.Path, "commit", "-m", "Merge "+base+" into "+wt.Branch); err != nil {
		abortMerge(wt)
		return err
	}
	return pushBranch(wt)
}

// stageResolution checks the agent's edits and stages them. The index keeps
// showing a file as conflicted until what replaced the markers is staged, so
// the markers are what tells whether the agent finished; git's unmerged list
// only clears after the add.
func stageResolution(dir string, kind agentKind, files []string) error {
	if markers := conflictMarkers(dir, files); len(markers) > 0 {
		return fmt.Errorf("%s left conflict markers in %s", kind.name(), strings.Join(markers, ", "))
	}
	if _, err := worktreeGit(dir, "add", "-A"); err != nil {
		return err
	}
	left, err := conflictedFiles(dir)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("%s left %d file(s) in conflict", kind.name(), len(left))
	}
	return nil
}

func pushBranch(wt *paneWorktree) error {
	_, err := worktreeGit(wt.Path, "push", "origin", wt.Branch)
	return err
}

func abortMerge(wt *paneWorktree) {
	_, _ = worktreeGit(wt.Path, "merge", "--abort")
}

// conflictedFiles lists what git still cannot merge, by path from the
// checkout's root.
func conflictedFiles(dir string) ([]string, error) {
	out, err := worktreeGit(dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	raw := strings.TrimRight(out, "\x00")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\x00"), nil
}

// conflictMarkers names the conflicted files that still hold git's markers,
// in case the agent adds them back without finishing the job.
func conflictMarkers(dir string, files []string) []string {
	var found []string
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "<<<<<<< ") ||
				strings.HasPrefix(line, ">>>>>>> ") || line == "=======" {
				found = append(found, name)
				break
			}
		}
	}
	return found
}

// waitMergeable gives GitHub the moment it takes to recompute a branch after
// a push, so the merge that follows sees the resolved tree.
func waitMergeable(path, branch string) (prInfo, error) {
	deadline := time.Now().Add(45 * time.Second)
	var last prInfo
	var lastErr error
	for {
		info, err := lookupPR(path, branch)
		if err == nil {
			last = info
			if !mergeConflicting(info.Mergeable, info.MergeState) {
				return info, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if last.Number == 0 && lastErr != nil {
				return prInfo{}, lastErr
			}
			return last, nil
		}
		time.Sleep(2 * time.Second)
	}
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

// worktreeHasNewWork reports a merged checkout holding work the merge did
// not include: changes still in the tree, or a commit made after the merge.
// The header then offers the next pull request instead of Archive, which
// would throw that work away.
func worktreeHasNewWork(wt *paneWorktree) (bool, error) {
	clean, err := worktreeIsClean(wt)
	if err != nil {
		return false, err
	}
	if !clean {
		return true, nil
	}
	if wt.MergedHead == "" {
		// A merge recorded before gexi knew the commit: fall back to the
		// branch being ahead of its base.
		ahead, err := commitsAhead(wt)
		return ahead > 0, err
	}
	out, err := worktreeGit(wt.Path, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != wt.MergedHead, nil
}

// reopenMergedWorktree puts a merged checkout back in the PR state once it
// holds work the merge did not include, so the next pull request starts from
// here rather than from a new worktree.
func (w *workspace) reopenMergedWorktree(p *pane, wt *paneWorktree) {
	if w.paneByID(p.ID) != p || p.Worktree != wt || wt.State != worktreeMerged {
		return
	}
	if !w.worktreeReady(p) {
		return
	}
	stopPRTimer(wt)
	wt.PR, wt.URL, wt.State = 0, "", ""
	wt.mergeable, wt.review, wt.checks, wt.mergeState = "", "", "", ""
	wt.MergedHead = ""
	wt.checked = time.Time{}
	w.invalidate()
	w.scheduleSave()
}

// createPullRequest starts the PR in the background; the header button shows
// the operation while it runs.
func (w *workspace) createPullRequest(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.State == worktreeMerged || wt.PR != 0 {
		return
	}
	if !w.worktreeReady(p) {
		return
	}
	wt.busy = worktreeBusyPR
	w.invalidate()
	snapshot := *wt
	kind := agentKind("")
	if p.agent != nil {
		kind = p.agent.Kind
	}
	go func() {
		msg, hint := "", ""
		clean, err := worktreeIsClean(&snapshot)
		if err != nil {
			w.onMain(func() { w.finishPullRequest(wt, p, prInfo{}, "", err) })
			return
		}
		if !clean {
			if kind == "" {
				hint = locales.CommitMessageNoAgent()
			} else {
				// Stage first, so the diff includes untracked files.
				if _, err := worktreeGit(snapshot.Path, "add", "-A"); err != nil {
					w.onMain(func() { w.finishPullRequest(wt, p, prInfo{}, "", err) })
					return
				}
				diff, _ := worktreeGit(snapshot.Path, "diff", "--cached", "--no-color", "--unified=1")
				if len(diff) > 6000 {
					diff = diff[:6000] + "\n… (truncated)"
				}
				msg, err = agentCommitMessage(kind, snapshot.Path, commitPrompt(diff))
				if err != nil {
					hint = locales.CommitMessageAgentFailed(kind.name(), err.Error())
					msg = ""
				}
			}
			if msg == "" {
				w.onMain(func() {
					wt.busy = ""
					w.commitTarget = p
					w.commitText = "Update from Gexi"
					w.commitHint = hint
					w.commitErr = ""
					w.commitOpen = true
					w.invalidate()
				})
				return
			}
		}
		info, branch, err := createPR(&snapshot, msg)
		w.onMain(func() { w.finishPullRequest(wt, p, info, branch, err) })
	}()
}

func (w *workspace) createPullRequestWithMessage(p *pane, message string) {
	wt := p.Worktree
	if wt == nil || wt.busy != "" || wt.PR != 0 || wt.State == worktreeMerged || w.paneByID(p.ID) != p {
		return
	}
	if !w.worktreeReady(p) {
		return
	}
	wt.busy = worktreeBusyPR
	w.invalidate()
	snapshot := *wt
	go func() {
		info, branch, err := createPR(&snapshot, message)
		w.onMain(func() { w.finishPullRequest(wt, p, info, branch, err) })
	}()
}

func (w *workspace) finishPullRequest(wt *paneWorktree, p *pane, info prInfo, branch string, err error) {
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
}

func (w *workspace) submitCommitMessage() {
	p := w.commitTarget
	message := strings.TrimSpace(w.commitText)
	if p == nil || w.paneByID(p.ID) != p {
		w.cancelCommitMessage()
		return
	}
	if !w.worktreeReady(p) {
		w.cancelCommitMessage()
		return
	}
	if message == "" {
		w.commitErr = locales.CommitMessageEmpty()
		w.invalidate()
		return
	}
	w.commitOpen = false
	w.commitTarget = nil
	w.commitText = ""
	w.commitHint = ""
	w.commitErr = ""
	w.invalidate()
	w.createPullRequestWithMessage(p, message)
}

func (w *workspace) cancelCommitMessage() {
	w.commitOpen = false
	w.commitTarget = nil
	w.commitText = ""
	w.commitHint = ""
	w.commitErr = ""
	w.invalidate()
}

func (w *workspace) commitDialog(c *ui.Context, tk tokens) {
	ui.Column(c).Width(440).Padding(20).Gap(8).Children(func() {
		ui.Text(c, locales.CommitMessageTitle()).FontSize(14).FontWeight(600).TextColor(tk.Foreground)
		if w.commitHint != "" {
			ui.Text(c, w.commitHint).FontSize(11).TextColor(tk.MutedForeground)
		}
		ui.TextInput(c, &w.commitText).Placeholder(locales.CommitMessagePlaceholder()).
			Height(28).FontSize(12)
		if w.commitErr != "" {
			ui.Text(c, w.commitErr).FontSize(11).TextColor(tk.Waiting)
		}
		ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			ui.Button(c, locales.Cancel()).OnClick(func() { w.cancelCommitMessage() })
			ui.Button(c, locales.CommitMessageConfirm()).OnClick(func() { w.submitCommitMessage() })
		})
	})
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
	if !w.worktreeReady(p) {
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
		headOut, headErr := worktreeGit(snapshot.Path, "rev-parse", "HEAD")
		head := strings.TrimSpace(headOut)
		w.onMain(func() {
			wt.busy = ""
			wt.State = worktreeMerged
			if headErr == nil && head != "" && wt.MergedHead == "" {
				wt.MergedHead = head
			}
			if syncErr != nil {
				w.flash(locales.WorktreeMergedLocalFailed(wt.Base, syncErr.Error()))
			} else {
				w.flash(locales.WorktreeMergedInto(wt.Base))
			}
			w.invalidate()
			w.scheduleSave()
			// A merged worktree has nothing left to keep, so the pane goes
			// back to the project folder instead of leaving the user to
			// archive it by hand.
			w.archiveMergedAndReturn(p)
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
	if strings.EqualFold(wt.mergeState, "BEHIND") {
		return locales.WorktreeBlockedBehind()
	}
	if mergeConflicts(wt) {
		return locales.WorktreeBlockedConflicts()
	}
	switch strings.ToUpper(wt.mergeable) {
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

// requestMerge refreshes the pull request, then merges it. The button is muted
// while GitHub would refuse it, and a press re-reads the status, so a check
// that finished since the last look still lets it through; a blocker that
// appeared since then is said in a flash instead of merging.
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
				// A conflict the agent can settle is worth a try before the
				// press is refused: it edits the files, gexi does the git.
				if mergeConflicts(wt) && canResolveConflicts(p) {
					w.resolveConflictsAndMerge(p)
					return
				}
				w.flash(locales.WorktreeMergeBlocked(reason))
				w.schedulePRRefresh(p)
				w.invalidate()
				w.scheduleSave()
				return
			}
			w.mergePullRequest(p)
		})
	}()
}

// archiveMergedWorktree hangs up the pane and any split that shares its
// checkout, then clears the checkout away. The branch is already merged, so
// nothing is lost; the branch is kept so Settings › Archived can restore the
// worktree until the user deletes it for good. The header offers this once
// the PR is merged instead of running it on its own.
func (w *workspace) archiveMergedWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil || wt.Detected || wt.busy != "" || wt.State != worktreeMerged {
		return
	}
	if !w.worktreeReady(p) {
		return
	}
	stopPRTimer(wt)
	s := w.sessionOf(p)
	projectDir := ""
	if pr := w.projectOf(s); pr != nil {
		projectDir = pr.Dir
	}
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
	w.archiveWorktree(wt, projectDir)
	w.flash(locales.WorktreeArchived(wt.Branch))
}

// archiveMergedAndReturn archives a worktree whose pull request just merged
// and puts the project back in front of the user: a pane in the project
// folder, on the branch the merge landed on. A project checkout with changes
// is left on whatever branch it is on, since that checkout is the user's own.
func (w *workspace) archiveMergedAndReturn(p *pane) {
	wt := p.Worktree
	if wt == nil {
		return
	}
	pr := w.projectOf(w.sessionOf(p))
	if pr == nil {
		return
	}
	root, base, dir := wt.Root, wt.Base, pr.Dir
	w.archiveMergedWorktree(p)
	// The merged pane is gone; a project with nothing left to show gets a
	// fresh shell in its folder, which is where the work continues.
	if !projectHasPane(pr) {
		_, _ = w.newSession(pr, nil, dir)
	}
	go func() {
		// Only a clean checkout follows the merge: the user's own changes
		// outrank a branch switch.
		if clean, err := checkoutIsClean(root); err != nil || !clean {
			return
		}
		if currentBranch(root) == base {
			return
		}
		if _, err := worktreeGit(root, "checkout", base); err != nil {
			return
		}
		w.onMain(func() {
			w.invalidate()
			w.scheduleSave()
		})
	}()
}

func projectHasPane(pr *project) bool {
	for _, s := range pr.Sessions {
		if s.Root != nil && len(s.Root.panes()) > 0 {
			return true
		}
	}
	return false
}

func checkoutIsClean(dir string) (bool, error) {
	out, err := worktreeGit(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

func inWorktree(p *pane, wt *paneWorktree) bool {
	for _, dir := range []string{p.dir(), p.initialDir} {
		if dirWithin(dir, wt.Path) {
			return true
		}
	}
	return false
}

// dirWithin reports whether dir is path itself or a folder inside it.
func dirWithin(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	dir, path = filepath.Clean(dir), filepath.Clean(path)
	return dir == path || strings.HasPrefix(dir, path+string(os.PathSeparator))
}

// startWorktreeMonitor follows each pane's folder. A shell moved into a
// linked git worktree gexi did not make still gets the header's pull request
// control instead of another W: the checkout is adopted once, marked as
// found, and dropped again when the pane leaves it. Git runs off the UI
// thread, and only when a pane's folder changed.
func (w *workspace) startWorktreeMonitor() {
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			type probe struct {
				p   *pane
				dir string
			}
			type reopen struct {
				p  *pane
				wt *paneWorktree
			}
			var probes []probe
			var refreshes []*pane
			var reopens []reopen
			now := time.Now()
			w.onMain(func() {
				if w.quitting {
					return
				}
				visible := map[string]bool{}
				for _, p := range w.visiblePanes() {
					visible[p.ID] = true
				}
				refreshed := map[string]bool{}
				for _, p := range w.allPanes() {
					dir := p.dir()
					if dir == "" {
						continue
					}
					if dir != p.lastWorktreeDir {
						p.lastWorktreeDir = dir
						// A checkout gexi made itself keeps its own chip
						// and its own lifecycle.
						if p.Worktree == nil || p.Worktree.Detected {
							probes = append(probes, probe{p, dir})
						}
						continue
					}
					// Only the panes on screen need the poll: selecting a
					// session refreshes the rest.
					if wt := p.Worktree; wt != nil && wt.Detected && visible[p.ID] &&
						!refreshed[wt.Path] && now.Sub(p.lastWorktreeRefresh) > 30*time.Second {
						p.lastWorktreeRefresh = now
						refreshed[wt.Path] = true
						refreshes = append(refreshes, p)
					}
					// A merged checkout goes back to PR once work lands
					// after the merge; checking is cheap but not free, so
					// it waits a few seconds between looks.
					if wt := p.Worktree; wt != nil && wt.State == worktreeMerged && !wt.Detected &&
						now.Sub(wt.checked) > reopenCheckInterval {
						wt.checked = now
						reopens = append(reopens, reopen{p, wt})
					}
				}
			})
			for _, pr := range probes {
				wt, ok := detectWorktreeAt(pr.dir)
				w.onMain(func() {
					w.applyDetectedWorktree(pr.p, pr.dir, wt, ok)
				})
			}
			for _, p := range refreshes {
				w.onMain(func() { w.refreshWorktree(p) })
			}
			for _, re := range reopens {
				if newWork, err := worktreeHasNewWork(re.wt); err == nil && newWork {
					w.onMain(func() { w.reopenMergedWorktree(re.p, re.wt) })
				}
			}
		}
	}()
}

// applyDetectedWorktree attaches or drops the checkout a pane's shell moved
// into. The probe ran off the UI thread, so the folder is checked again here:
// a result that arrived after the pane moved on is dropped.
func (w *workspace) applyDetectedWorktree(p *pane, dir string, wt *paneWorktree, ok bool) {
	if w.quitting || w.paneByID(p.ID) != p || p.dir() != dir {
		return
	}
	if p.Worktree != nil && !p.Worktree.Detected {
		return
	}
	if !ok {
		if p.Worktree != nil && p.Worktree.Detected && !dirWithin(dir, p.Worktree.Path) {
			stopPRTimer(p.Worktree)
			p.Worktree = nil
			p.lastWorktreeRefresh = time.Time{}
			w.invalidate()
			w.scheduleSave()
		}
		return
	}
	if cur := p.Worktree; cur != nil {
		if cur.Path == wt.Path {
			if cur.Branch != wt.Branch {
				cur.Branch = wt.Branch
				w.invalidate()
				w.scheduleSave()
			}
			return
		}
		stopPRTimer(cur)
	}
	p.Worktree = wt
	p.lastWorktreeRefresh = time.Now()
	w.invalidate()
	w.scheduleSave()
	w.refreshWorktree(p)
}

// refreshWorktree picks up a branch the agent renamed and, now and then, a PR
// state that changed outside gexi.
func (w *workspace) refreshWorktree(p *pane) {
	wt := p.Worktree
	if wt == nil || !w.worktreeReady(p) {
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
			if p.Worktree != wt || branch == "" || branch == "HEAD" || branch == wt.Branch {
				return
			}
			wt.Branch = branch
			wt.Naming = false
			w.invalidate()
			w.scheduleSave()
		})
	}()
	// A detected checkout's pull request may appear or change outside gexi,
	// from the agent or gh itself, so it is looked up even before one exists.
	interval := 30 * time.Second
	if wt.Detected && wt.PR == 0 {
		interval = time.Minute
	}
	if wt.State != worktreeMerged && (wt.PR != 0 || wt.Detected) && time.Since(wt.checked) > interval {
		wt.checked = time.Now()
		prPath, prBranch := wt.Path, wt.Branch
		go func() {
			info, err := lookupPR(prPath, prBranch)
			if err != nil {
				return
			}
			head := ""
			if info.State == worktreeMerged {
				if out, err := worktreeGit(prPath, "rev-parse", "HEAD"); err == nil {
					head = strings.TrimSpace(out)
				}
			}
			w.onMain(func() {
				if p.Worktree != wt {
					return
				}
				applyPRStatus(wt, info)
				if head != "" && wt.MergedHead == "" {
					wt.MergedHead = head
				}
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
// the pane in it. An agent running in the pane is stopped first: the changes
// it made move with the pane, and a fresh conversation of the same agent
// opens in the worktree. Git and the stop run off the UI thread.
func (w *workspace) createPaneWorktree(p *pane) {
	if p.Worktree != nil || p.worktreeBusy != "" {
		return
	}
	s := w.sessionOf(p)
	pr := w.projectOf(s)
	if pr == nil {
		return
	}
	p.worktreeBusy = worktreeBusyCreate
	w.invalidate()
	dir := pr.Dir
	go func() {
		// The checkout is made while the agent still runs: creating a
		// worktree never reads the project's working tree. Only a project
		// that cannot have one leaves the agent untouched.
		wt, err := createWorktree(dir)
		if err != nil || wt == nil {
			w.onMain(func() {
				if w.paneByID(p.ID) != p {
					return
				}
				p.worktreeBusy = ""
				if err != nil {
					w.flash(locales.WorktreeError(err.Error()))
				} else {
					w.flash(locales.WorktreeNotRepo())
				}
				w.invalidate()
			})
			return
		}
		// The pane may have closed while git worked. Moving its project's
		// changes now would surprise the user, and the new checkout has
		// nothing to keep.
		open := false
		group := 0
		initialDir := ""
		w.onMain(func() {
			if w.paneByID(p.ID) != p {
				return
			}
			open = true
			group = p.foregroundGroup()
			initialDir = p.initialDir
		})
		if !open {
			go func() { _ = removeWorktree(wt) }()
			return
		}
		// What opens in the worktree is decided from the agent the pane is
		// running now, not from the monitor's last look, which may be
		// seconds old.
		var det *agentDetection
		if group > 0 {
			det = detectAgent(group, initialDir)
		}
		launch := []string(nil)
		if det != nil {
			launch = det.Kind.launch()
		}
		if det != nil && !stopAgentGroup(group) {
			go func() { _ = removeWorktree(wt) }()
			w.onMain(func() {
				if w.paneByID(p.ID) != p {
					return
				}
				p.worktreeBusy = ""
				w.flash(locales.WorktreeAgentStuck())
				w.invalidate()
			})
			return
		}
		carried, err := moveChangesToWorktree(dir, wt.Path)
		if err != nil {
			// The changes are back in the project folder, or in a stash
			// when even that failed; the fresh checkout has nothing to
			// keep.
			go func() { _ = removeWorktree(wt) }()
			wt = nil
		}
		w.onMain(func() {
			if w.paneByID(p.ID) != p {
				// The pane closed while git worked. A checkout that now
				// holds the project's changes is kept, so the work is not
				// lost with it; an empty one goes.
				if wt != nil && !carried {
					go func() { _ = removeWorktree(wt) }()
				}
				return
			}
			p.worktreeBusy = ""
			// The agent's process is gone now. Forgetting it here lets the
			// pane restart either way, without waiting for the monitor.
			if p.agent != nil {
				p.applyAgent(nil)
			}
			switch {
			case err != nil:
				w.flash(locales.WorktreeError(err.Error()))
				w.recoverPaneAfterWorktreeMove(p, dir, det != nil)
			case wt == nil:
				w.flash(locales.WorktreeNotRepo())
				w.recoverPaneAfterWorktreeMove(p, dir, det != nil)
			default:
				if carried && det != nil && det.Kind == agentClaude {
					wt.Carried = dir
				}
				// Perch has no hook to answer, so the note it would have got
				// from one is put in the checkout before Perch starts there.
				if det != nil && det.Kind == agentPerch {
					if carried {
						wt.Carried = dir
					}
					seedPerchWorktreeRule(wt)
				}
				w.restartPaneInWorktree(p, wt, launch, carried)
				return
			}
			w.invalidate()
		})
	}()
}

// worktreeMoveMu keeps two panes from pushing and popping each other's
// stash while the working tree's changes move into a checkout.
var worktreeMoveMu sync.Mutex

// moveChangesToWorktree moves the project folder's uncommitted changes into
// a fresh worktree. Stashes live in the repository rather than in a
// checkout, so the push made here pops there. It reports whether there was
// anything to move; on failure the changes go back to the project folder,
// or stay in a stash when even that fails.
func moveChangesToWorktree(projectDir, worktreeDir string) (bool, error) {
	worktreeMoveMu.Lock()
	defer worktreeMoveMu.Unlock()
	files, err := gitChanges(projectDir)
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, nil
	}
	if _, err := worktreeGit(projectDir, "stash", "push", "-u", "-m", "gexi worktree"); err != nil {
		return false, err
	}
	if _, err := worktreeGit(worktreeDir, "stash", "pop", "--index"); err != nil {
		// A failed pop keeps the stash entry. Clear the half-applied
		// checkout and put the changes back where they came from.
		_, _ = worktreeGit(worktreeDir, "reset", "--hard", "--quiet")
		_, _ = worktreeGit(worktreeDir, "clean", "-fd", "--quiet")
		if _, back := worktreeGit(projectDir, "stash", "pop", "--index"); back != nil {
			return false, fmt.Errorf("%w (the changes are in git stash)", err)
		}
		return false, err
	}
	return true, nil
}

// recoverPaneAfterWorktreeMove gives back a pane whose own program the stop
// ended when the worktree could not take it, so the move does not leave a
// dead terminal behind. It resumes the conversation the pane had: the fresh
// start is for the worktree, which the failed move never reached. stopped
// says an agent was signalled, so a shell the agent merely ran inside is
// left alone.
func (w *workspace) recoverPaneAfterWorktreeMove(p *pane, dir string, stopped bool) {
	if !stopped {
		return
	}
	if !p.exited && !paneRunsAgent(p.command) {
		return
	}
	w.restartPaneInDir(p, dir, nil, p.lastAgentRestore, "")
}

// restartPaneInWorktree swaps the pane's terminal for command (a shell when
// nil) in the worktree, keeping its slot, id, and focus. keep says the
// checkout holds changes that a failed restart must not throw away.
func (w *workspace) restartPaneInWorktree(p *pane, wt *paneWorktree, command []string, keep bool) {
	if w.restartPaneInDir(p, wt.Path, wt, command, locales.WorktreeCreated()) {
		return
	}
	if keep {
		w.flash(locales.WorktreeKeptClose(wt.Path))
		return
	}
	go func() { _ = removeWorktree(wt) }()
}

// restartPaneInDir swaps the pane's terminal for command in dir, keeping its
// slot, id, and focus. It reports whether the pane restarted.
func (w *workspace) restartPaneInDir(p *pane, dir string, wt *paneWorktree, command []string, message string) bool {
	s := w.sessionOf(p)
	if s == nil || s.Root == nil || w.paneByID(p.ID) != p {
		return false
	}
	if p.agent != nil {
		w.flash(locales.WorktreeAgentRunning())
		return false
	}
	np, err := newPane(w, p.ID, command, dir, "")
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
	if wt == nil || wt.Detected || p.worktreeBusy != "" || wt.busy != "" {
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
	if wt == nil || wt.Detected {
		return
	}
	if !w.worktreeReady(p) {
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
			if !w.restartPaneInDir(p, dir, nil, nil, locales.WorktreeLeft()) {
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

// worktreeActionChip draws the pane header's worktree control as one chip
// split in two: the label runs action on the left, a chevron on the right
// opens the worktree menu, where leaving the checkout sits. Leaving is
// rare, so it shares the chip instead of holding a button of its own. bg
// is the chip's fill, ui.Transparent for the quiet chip that tints as the
// pointer crosses it, and fg colours its label and chevron.
func (w *workspace) worktreeActionChip(c *ui.Context, tk tokens, p *pane, bg, fg ui.Color,
	hint string, action func(), label func()) {
	quiet := bg == ui.Transparent
	group := ui.Row(c).Height(22).Radius(headerRadius).Center().
		Border(1, ui.Transparent)
	switch {
	case !quiet:
		group = group.Background(bg)
	case group.Hovered():
		group = capsule(c, group, tk, tk.Control.Alpha(0.3), tk.Control)
	}
	group.Children(func() {
		// A checkout gexi only found has no Leave menu: the chip is one
		// press, so it takes the whole pill.
		detected := p.Worktree != nil && p.Worktree.Detected
		main := ui.Row(c).Height(22).Padding(0, 6, 0, 8).
			Center().Cursor(ui.CursorPointer).Tooltip(hint).OnClick(action)
		if detected {
			main = main.Padding(0, 8, 0, 8).Radius(headerRadius)
		} else {
			main = main.Radius(headerRadius)
		}
		main.Children(label)
		if detected {
			return
		}
		// The rule separates the two presses: each half does its own
		// thing, and the chip still reads as one control.
		rule := tk.Control
		if !quiet {
			rule = fg.Alpha(0.28)
		}
		ui.Box(c).Width(1).Height(12).Background(rule)
		chevron := fg
		if quiet {
			// The chevron is a secondary press: it stays quieter than
			// the label it follows.
			chevron = tk.MutedForeground
		}
		w.worktreeMenuSegment(c, tk, p, chevron)
	})
}

// worktreeMenuSegment is the chevron half of worktreeActionChip.
func (w *workspace) worktreeMenuSegment(c *ui.Context, tk tokens, p *pane, fg ui.Color) {
	seg := ui.Row(c).Width(18).Height(22).Radius(headerRadius).
		Center().Cursor(ui.CursorPointer).Tooltip(locales.WorktreeLeaveHint())
	if p.worktreeBusy == worktreeBusyLeave {
		seg.Children(func() { worktreeSpinner(c, tk, locales.WorktreeLeaving()) })
		return
	}
	wt := p.Worktree
	seg.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(locales.WorktreeLeave()).Disabled(wt == nil || wt.busy != "").Chosen() {
			w.promptLeaveWorktree(p)
		}
	}).Children(func() {
		if src := iconSource("chevron-down", colorHex(fg)); src != nil {
			// The glyph sits inside the button: the press has to reach
			// the segment so its menu opens.
			ui.Image(c, src).Width(10).Height(10).IgnoreInput(true)
		}
	})
}

// keepClosedWorktree leaves a pane's checkout on disk when its panel closes.
// Closing is not deleting: Settings › Worktrees lists the checkout, where it
// can be opened again, and Clean up is the one action that removes unused
// ones. Nothing is silently thrown away by a mistyped ⌘W.
func (w *workspace) keepClosedWorktree(wt *paneWorktree) {
	// A checkout gexi only found belongs to the user, not to this pane.
	if wt == nil || wt.Detected {
		return
	}
	stopPRTimer(wt)
	w.flash(locales.WorktreeKeptClose(wt.Path))
}

// cleanupOrphanWorktrees removes checkouts under the app's worktree folder
// that no pane uses and that have nothing to lose. It runs only from the
// Worktrees page, since closing a panel keeps its checkout.
func (w *workspace) cleanupOrphanWorktrees() {
	referenced := map[string]bool{}
	for _, p := range w.livePanes() {
		if p.Worktree != nil {
			referenced[filepath.Clean(p.Worktree.Path)] = true
		}
	}
	// A split pane inside a checkout has no worktree of its own; its folder
	// still marks the checkout as used.
	used := func(path string) bool {
		for _, p := range w.livePanes() {
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
		w.flash(locales.WorktreeCleanupNothing())
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
		w.flash(locales.WorktreeCleanupNothing())
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
			w.worktreesScanned = false
			w.invalidateSettings()
			switch {
			case removed > 0:
				w.flash(locales.WorktreeCleanupDone(strconv.Itoa(removed)))
			default:
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

// detectWorktreeAt reports the linked git worktree a folder belongs to. The
// repository's main checkout is not one: W still makes a worktree from it.
// A pane that was opened or moved into a worktree this way is shown its pull
// request instead of another W.
func detectWorktreeAt(dir string) (*paneWorktree, bool) {
	top, ok := gitTopLevel(dir)
	if !ok {
		return nil, false
	}
	root, ok := worktreeRepoRoot(top)
	if !ok || sameDir(root, top) {
		return nil, false
	}
	branch := currentBranch(top)
	if branch == "" {
		return nil, false
	}
	base := detectBaseBranch(root)
	if base == "" {
		return nil, false
	}
	return &paneWorktree{
		Root:     root,
		Path:     top,
		Branch:   branch,
		Base:     base,
		Detected: true,
	}, true
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
// prompt, so this reaches the model with the first request. Perch has no hook
// to answer, so it reads the note from a rule file instead; see
// writePerchWorktreeRule.
func (w *workspace) worktreeNamingContext(p *pane) string {
	wt := p.Worktree
	if wt == nil {
		return ""
	}
	note := worktreeNamingNote(wt)
	if note == "" {
		return ""
	}
	// Both flags are one-shot: the note is delivered once, whichever channel
	// the agent has.
	wt.Carried = ""
	wt.Naming = false
	w.scheduleSave()
	return note
}

// worktreeNamingNote is the text both channels carry: the hook's stdout for
// Claude Code, and the rule file for Perch. Keeping one string means the two
// agents are told the same thing.
func worktreeNamingNote(wt *paneWorktree) string {
	if wt == nil {
		return ""
	}
	var notes []string
	if wt.Carried != "" {
		notes = append(notes, fmt.Sprintf(
			"gexi workspace: the uncommitted changes that were in %s moved into this worktree "+
				"before this session started, so the project folder is clean and the work is here.",
			wt.Carried))
	}
	if wt.Naming {
		notes = append(notes, fmt.Sprintf(
			"gexi workspace: this session runs in the git worktree %s on branch %q. "+
				"The branch name is a placeholder. Once the request is clear, rename it once "+
				"with `git branch -m <new-name>` to a short kebab-case name for this task "+
				"(for example `fix-login-redirect`), then continue; leave the worktree path unchanged.",
			wt.Path, wt.Branch))
	}
	return strings.Join(notes, "\n\n")
}

// seedPerchWorktreeRule writes the naming note where Perch will read it. It
// runs when a pane restarts in a fresh checkout, before Perch has started, so
// the note is already in place for the first turn.
func seedPerchWorktreeRule(wt *paneWorktree) {
	if wt == nil {
		return
	}
	_ = writePerchWorktreeRule(wt, worktreeNamingNote(wt))
}

// perchRuleName is the rule file gexi writes into a worktree for Perch to
// read. It is named for gexi so a later run can replace exactly its own file.
const perchRuleName = "gexi-worktree.md"

// writePerchWorktreeRule hands Perch the note Claude Code gets through a
// hook. Perch loads .perch/rules/*.md as project instructions before the
// first turn, so a rule file is the one channel that reaches the model
// without a hook. The file is written into the checkout's .git/info/exclude
// so it never shows up as a change of its own.
func writePerchWorktreeRule(wt *paneWorktree, note string) error {
	if wt == nil || wt.Path == "" {
		return nil
	}
	dir := filepath.Join(wt.Path, ".perch", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, perchRuleName)
	if strings.TrimSpace(note) == "" {
		_ = os.Remove(path)
		return nil
	}
	body := "# gexi worktree\n\n" + strings.TrimSpace(note) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	excludePerchRule(wt.Path)
	return nil
}

// excludePerchRule keeps gexi's own rule file out of the checkout's changes,
// so the note never becomes part of a commit or a diff the user reviews.
func excludePerchRule(worktreePath string) {
	root, ok := worktreeRepoRoot(worktreePath)
	if !ok {
		return
	}
	// A worktree's own exclude file lives under the common git dir.
	out, err := worktreeGit(worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	common := strings.TrimSpace(out)
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	path := filepath.Join(common, "info", "exclude")
	entry := ".perch/rules/" + perchRuleName
	current, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(current), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	body := string(current)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	_ = os.WriteFile(path, []byte(body+entry+"\n"), 0o644)
}

// paneWorktreeButton is the W control in the pane header. W makes a worktree
// for the pane; once it has one, the same slot shows PR / Merge instead.
func (w *workspace) paneWorktreeButton(c *ui.Context, tk tokens, p *pane) {
	if p.Worktree == nil {
		label := "W"
		action := func() { w.createPaneWorktree(p) }
		hint := locales.WorktreeCreateHint()
		busy := ""
		if p.worktreeBusy != "" {
			busy = locales.WorktreeCreating()
			action = func() {}
		}
		el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(headerRadius).
			Border(1, ui.Transparent).
			Center().Cursor(ui.CursorPointer).Tooltip(hint)
		if el.Hovered() {
			el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
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

// worktreeButton is the PR control in the pane header, left of the pane
// actions. The first press opens a pull request. Once one exists, PR opens
// the page and a separate Merge button appears; Merge is muted while GitHub
// would not merge it, and otherwise merges on the press. After the merge it
// becomes Archive, which clears the pane and its checkout away. Each form
// carries the worktree menu on its right, where leaving the checkout sits.
func (w *workspace) worktreeButton(c *ui.Context, tk tokens, p *pane) {
	wt := p.Worktree
	// The checkout can be deleted while gexi runs. There is no git work to
	// offer then, only a way to clear the binding and find the worktree page.
	if !worktreeCheckoutExists(wt) {
		action := func() { w.dropGoneWorktree(p) }
		el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(headerRadius).
			Border(1, ui.Transparent).
			Center().Cursor(ui.CursorPointer).Tooltip(locales.WorktreeGoneHint())
		if el.Hovered() {
			el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
		}
		el.OnClick(action).Children(func() {
			ui.Text(c, locales.WorktreeGone()).FontSize(11).FontWeight(600).
				TextColor(tk.MutedForeground).OnClick(action)
		})
		return
	}
	if wt.State == worktreeMerged {
		if wt.Detected {
			// gexi does not own this checkout, so there is nothing to
			// archive; the merged pull request itself stays reachable.
			w.pullRequestChip(c, tk, p, true)
			return
		}
		action := func() { w.archiveMergedWorktree(p) }
		w.worktreeActionChip(c, tk, p, ui.Transparent, tk.Foreground,
			locales.WorktreeArchiveHint(), action, func() {
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
		w.worktreeActionChip(c, tk, p, ui.Transparent, tk.Foreground,
			wt.Branch+" → "+wt.Base, action, func() {
				ui.Text(c, locales.WorktreePR()).FontSize(11).FontWeight(600).
					TextColor(tk.Foreground).OnClick(action)
			})
		return
	}
	// The menu sits on the chip farthest right: Merge while it shows, the
	// pull request itself otherwise.
	w.pullRequestChip(c, tk, p, wt.State != "open")
	if wt.State == "open" {
		w.mergeChip(c, tk, p)
	}
}

func worktreeChip(c *ui.Context, tk tokens) ui.Element {
	el := ui.Row(c).Height(22).Padding(0, 7, 0, 7).Radius(headerRadius).
		Border(1, ui.Transparent).
		Center().Cursor(ui.CursorPointer)
	if el.Hovered() {
		el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
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
	case worktreeBusyResolve:
		return locales.WorktreeResolving()
	}
	return ""
}

// pullRequestChip shows the number and opens the page, as Conductor's #8376 ↗.
// onItsOwn says no Merge button follows it, so the chip carries the
// worktree menu itself.
func (w *workspace) pullRequestChip(c *ui.Context, tk tokens, p *pane, onItsOwn bool) {
	wt := p.Worktree
	action := func() {
		w.refreshWorktree(p)
		if wt.URL != "" {
			openPath(wt.URL)
		}
	}
	content := func() {
		ui.Text(c, "#"+strconv.Itoa(wt.PR)).FontSize(11).FontWeight(600).
			TextColor(tk.Foreground).OnClick(action)
		ui.Text(c, "↗").FontSize(10).TextColor(tk.MutedForeground).OnClick(action)
	}
	if onItsOwn {
		w.worktreeActionChip(c, tk, p, ui.Transparent, tk.Foreground,
			prStatusHint(wt), action, content)
		return
	}
	worktreeChip(c, tk).Tooltip(prStatusHint(wt)).OnClick(action).Children(content)
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
	case mergeConflicts(wt):
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

// mergeChip is Conductor's filled Merge button: it merges on the press, and
// stays disabled while GitHub would refuse the merge. A press refreshes the
// status first, so a check that finished since the last look still lets it
// through, and one that started failing is said in a flash instead.
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
		if mergeConflicts(wt) && canResolveConflicts(p) {
			hint = locales.WorktreeResolveConflicts(p.agent.Kind.name())
		}
	}
	action := func() { w.requestMerge(p) }
	w.worktreeActionChip(c, tk, p, bg, fg, hint, action, func() {
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
