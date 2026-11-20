package main

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/justdoswift/gexi/locales"
)

// The files drawer is the right-hand column Rex shows: a list of the working
// tree's changes or of the project's files, and, over it, one file's diff or
// text. It slides in over the canvas like a drawer, so opening it never
// reshuffles the panes.
//
// It reads local projects only.

const (
	drawerTabChanges = iota
	drawerTabFiles

	drawerListWidth float32 = 320
	drawerFileWidth float32 = 620
)

// The two scopes of the changes tab: the working tree's uncommitted work, and
// everything the branch would bring to its base.
const (
	drawerScopeWorktree = iota
	drawerScopeBranch
)

// drawerEntry is one row of the files tree. path is relative to the project,
// slash-separated as the tree walks it.
type drawerEntry struct {
	name string
	path string
	dir  bool
}

// drawerFile is the file shown over the list: a diff for a change, or a
// read-only editor for a file from the tree.
type drawerFile struct {
	Path  string
	Diff  *editor.DiffState
	View  *editor.State
	Note  string
	Ready bool

	// What the list said about it, so the file's own header carries the same
	// mark and counts as its row did.
	Status  byte
	Added   int
	Deleted int
}

// drawerState is everything the drawer keeps between frames, and what the
// next launch restores.
type drawerState struct {
	open    bool
	closing bool
	shown   bool
	tab     int

	listWidth float32
	fileWidth float32

	// The list's project, and what it read there.
	dir     string
	changes []changedFile
	err     string

	// The changes tab's scope, and the branch side's own read: the files the
	// branch would bring to base, the merge base their old side comes from,
	// and the base branch's name for the row's hint.
	scope       int
	branchFiles []changedFile
	branchErr   string
	branchBusy  bool
	branchGen   int
	branchName  string
	branchBase  string
	branchDir   string

	// The files tree: the folders opened, and the listings read so far.
	expanded map[string]bool
	dirs     map[string][]drawerEntry

	file    *drawerFile
	viewGen int
}

func (d *drawerState) width() float32 {
	if d.file != nil {
		if d.fileWidth <= 0 {
			return drawerFileWidth
		}
		return d.fileWidth
	}
	if d.listWidth <= 0 {
		return drawerListWidth
	}
	return d.listWidth
}

// syncDrawerDir follows the selection: when it moves to another project, the
// listings and the open file are read again rather than shown over the new
// project's name.
func (w *workspace) syncDrawerDir() string {
	dir := w.drawerDir()
	d := &w.drawer
	if dir != d.dir {
		d.dir = dir
		d.changes = nil
		d.err = ""
		d.clearBranch()
		d.expanded = map[string]bool{}
		d.dirs = map[string][]drawerEntry{}
		d.file = nil
		d.viewGen++
	}
	return dir
}

// clearBranch drops the branch side's read for a checkout the drawer no
// longer shows.
func (d *drawerState) clearBranch() {
	d.branchFiles = nil
	d.branchErr = ""
	d.branchBusy = false
	d.branchGen++
	d.branchName = ""
	d.branchBase = ""
	d.branchDir = ""
}

// drawerProject is the folder the drawer reads: the selected session's local
// project.
func (w *workspace) drawerProject() *project {
	s := w.selected()
	if s == nil {
		return nil
	}
	return w.projectOf(s)
}

// drawerDir is what to read, and "" when there is nothing to read here. A
// focused pane that was moved into a worktree reads that checkout.
func (w *workspace) drawerDir() string {
	pr := w.drawerProject()
	if pr == nil {
		return ""
	}
	if s := w.selected(); s != nil && s.Root != nil {
		for _, p := range s.Root.panes() {
			if p.ID == w.FocusedPane && p.Worktree != nil {
				return p.Worktree.Path
			}
		}
	}
	return pr.Dir
}

// branchPillInfo is what the branch pill has to show. It holds the guards the
// pill draws by, so the header can measure the pill before deciding what fits.
type branchPillInfo struct {
	dir     string
	branch  string
	added   int
	deleted int
	changed bool
}

func (w *workspace) branchPillInfo(p *pane) (branchPillInfo, bool) {
	// A pane on another Mac brings its branch with it; the changes are its
	// host's to count.
	if p.remote != nil {
		b := p.remote.branch
		if b == nil || b.Name == "" {
			return branchPillInfo{}, false
		}
		return branchPillInfo{
			dir:     p.dir(),
			branch:  b.Name,
			added:   b.Added,
			deleted: b.Deleted,
			changed: b.Changed,
		}, true
	}
	pr := w.projectOf(w.sessionOf(p))
	if pr == nil {
		return branchPillInfo{}, false
	}
	dir := pr.Dir
	if p.Worktree != nil {
		dir = p.Worktree.Path
	}
	s, ok := w.gitInfo[dir]
	if !ok {
		w.refreshGitDir(dir)
		return branchPillInfo{}, false
	}
	if !s.OK || s.Branch == "" {
		return branchPillInfo{}, false
	}
	return branchPillInfo{
		dir:     dir,
		branch:  s.Branch,
		added:   s.Added,
		deleted: s.Deleted,
		changed: s.Changed,
	}, true
}

// branchPillWidth measures the pill before it is drawn, so the header fits it
// beside the title instead of deciding by a fixed pane width.
func branchPillWidth(c *ui.Context, info branchPillInfo) float32 {
	textW := func(text string, size float32) float32 {
		tw, _ := c.MeasureText(0, ui.Span{Text: text, Size: size})
		return tw
	}
	// The pill's padding a side, the branch mark, and the gap after each.
	width := float32(16 + 12 + 5 + textW(shortBranch(info.branch), 11))
	if info.added > 0 {
		width += 5 + textW("+"+strconv.Itoa(info.added), 10)
	}
	if info.deleted > 0 {
		width += 5 + textW("−"+strconv.Itoa(info.deleted), 10)
	}
	return width
}

// branchPill is the pane header's Git badge: the branch the project is on,
// what the working tree changes, and, on a click, their diff. It follows the
// title as Rex's does, and is the drawer's everyday way in. A hint replaces
// the pill's own tooltip when it already stands for the checkout as well.
func (w *workspace) branchPill(c *ui.Context, tk tokens, p *pane, hint string) {
	info, ok := w.branchPillInfo(p)
	if !ok {
		return
	}
	d := &w.drawer
	showing := d.open && d.tab == drawerTabChanges && d.dir == info.dir
	// A change Git counts no lines for has no +N or −N to show, so the
	// branch name itself carries the colour.
	unmeasured := info.changed && info.added+info.deleted == 0
	color := tk.MutedForeground
	active := showing
	el := ui.Row(c).Height(chipHeight).Radius(chipRadius).Padding(0, chipPadX).Gap(chipGap).
		AlignItems(ui.Center).Shrink(1).Cursor(ui.CursorPointer)
	if el.Hovered() {
		active = true
	}
	if active {
		color = tk.Foreground
	}
	el = badge(c, el, tk, active)
	// A remote pane's branch is the host's: its changes open on the host,
	// not here, so the pill only names it.
	pick := func() { w.toggleChangesDrawer(info.dir) }
	if p.remote != nil {
		pick = func() {}
		el = el.Cursor(ui.CursorDefault)
		if hint == "" {
			hint = info.branch
		}
	}
	if hint == "" {
		hint = locales.DrawerOpenChanges()
	}
	el.Tooltip(hint).OnClick(pick).Children(func() {
		if src := iconSource("branch", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(12).Height(12).IgnoreInput(true)
		}
		textColor := color
		if unmeasured {
			textColor = tk.Waiting
		}
		ui.Text(c, shortBranch(info.branch)).FontSize(chipFont).SingleLine().
			Shrink(1).TextColor(textColor).OnClick(pick)
		if info.added > 0 {
			ui.Text(c, "+"+strconv.Itoa(info.added)).FontSize(chipFontNum).Shrink(0).
				TextColor(tk.Working).OnClick(pick)
		}
		if info.deleted > 0 {
			ui.Text(c, "−"+strconv.Itoa(info.deleted)).FontSize(chipFontNum).Shrink(0).
				TextColor(tk.Danger).OnClick(pick)
		}
	})
}

// shortBranch keeps a long branch name from crowding the header.
func shortBranch(name string) string {
	r := []rune(name)
	if len(r) > 28 {
		return string(r[:27]) + "…"
	}
	return name
}

// toggleChangesDrawer opens the drawer on the changes, or puts them away when
// they are already what it shows; a file open over the list comes back to it.
func (w *workspace) toggleChangesDrawer(dir string) {
	d := &w.drawer
	if d.open && d.tab == drawerTabChanges && d.dir == dir {
		if d.file != nil {
			w.closeDrawerFile()
			return
		}
		w.closeDrawer()
		return
	}
	if !d.open {
		w.openDrawer(drawerTabChanges)
		return
	}
	w.setDrawerTab(drawerTabChanges)
}

func (w *workspace) toggleDrawer() {
	if w.drawer.open {
		w.closeDrawer()
		return
	}
	w.openDrawer(w.drawer.tab)
}

func (w *workspace) openDrawer(tab int) {
	// The reply reader holds the same right-hand column.
	if w.reply.open || w.reply.closing {
		w.closeReply()
	}
	d := &w.drawer
	d.open = true
	d.closing = false
	d.shown = false
	d.tab = tab
	w.resetDrawer()
	w.syncStageMirrors()
	w.invalidate()
	w.scheduleSave()
	if tab == drawerTabChanges {
		w.refreshGit()
		if w.drawer.scope == drawerScopeBranch {
			w.refreshBranch()
		}
	}
}

func (w *workspace) closeDrawer() {
	d := &w.drawer
	if !d.open {
		return
	}
	d.open = false
	d.closing = true
	w.syncStageMirrors()
	w.invalidate()
	w.scheduleSave()
	time.AfterFunc(sidebarMotion, func() {
		w.onMain(func() {
			if w.drawer.open {
				return
			}
			w.drawer.closing = false
			w.drawer.shown = false
			w.syncStageMirrors()
			w.invalidate()
		})
	})
}

// resetDrawer drops what was read from the previous project.
func (w *workspace) resetDrawer() {
	d := &w.drawer
	d.dir = ""
	d.changes = nil
	d.err = ""
	d.clearBranch()
	d.expanded = map[string]bool{}
	d.dirs = map[string][]drawerEntry{}
	d.file = nil
}

func (w *workspace) setDrawerTab(tab int) {
	d := &w.drawer
	if d.tab == tab {
		return
	}
	d.tab = tab
	d.file = nil
	if tab == drawerTabFiles {
		// Coming back to the tree reads its folders again, so files made
		// while the changes tab was up show without a restart.
		d.dirs = map[string][]drawerEntry{}
	}
	w.invalidate()
	w.scheduleSave()
	if tab == drawerTabChanges {
		w.refreshGit()
		if d.scope == drawerScopeBranch {
			w.refreshBranch()
		}
	}
}

// refreshGit reads the selected project's status off the UI thread and applies
// it there: the pane headers' branch pill always, and the drawer's list while
// it shows the changes. One read feeds both, and a read already in flight is
// left to land; the next tick gets the newer one.
func (w *workspace) refreshGit() {
	w.refreshGitDir(w.syncDrawerDir())
}

// refreshGitDir is refreshGit for one checkout, so a pane's branch pill can
// read its own worktree rather than the project's main checkout.
func (w *workspace) refreshGitDir(dir string) {
	if dir == "" || w.gitBusy[dir] {
		return
	}
	w.gitBusy[dir] = true
	w.gitGen[dir]++
	gen := w.gitGen[dir]
	go func() {
		files, summary, err := gitSnapshot(dir)
		w.onMain(func() {
			if w.gitGen[dir] != gen {
				return
			}
			delete(w.gitBusy, dir)
			changed := w.gitInfo[dir] != summary
			w.gitInfo[dir] = summary
			d := &w.drawer
			showing := d.open && d.tab == drawerTabChanges && d.dir == dir && d.file == nil
			if showing {
				if err != nil {
					d.changes, d.err = nil, drawerError(err)
				} else {
					d.changes, d.err = files, ""
				}
			}
			if changed || showing {
				w.invalidate()
			}
			if changed && showing && d.scope == drawerScopeBranch {
				w.refreshBranch()
			}
			if showing && d.scope == drawerScopeBranch && d.branchDir != dir {
				w.refreshBranch()
			}
		})
	}()
}

// refreshBranch reads what the branch would bring to its base, off the UI
// thread. The base is the pane's worktree base for that checkout, else the
// repository's own default.
func (w *workspace) refreshBranch() {
	d := &w.drawer
	dir := w.syncDrawerDir()
	if dir == "" {
		return
	}
	if d.branchBusy && d.branchDir == dir {
		return
	}
	base := w.drawerBase(dir)
	d.branchName = base
	d.branchDir = dir
	d.branchBusy = true
	d.branchGen++
	gen := d.branchGen
	go func() {
		files, mb, err := gitBranchChanges(dir, base)
		w.onMain(func() {
			if d.branchGen != gen {
				return
			}
			d.branchBusy = false
			if err != nil {
				d.branchFiles, d.branchBase, d.branchErr = nil, "", ""
				if err != errNoBase {
					d.branchErr = drawerError(err)
				}
				w.invalidate()
				return
			}
			d.branchFiles, d.branchErr, d.branchBase = files, "", mb
			w.invalidate()
		})
	}()
}

// drawerBase is the branch the drawer measures against: the focused pane's
// worktree base when that is the checkout, else the repository's default.
func (w *workspace) drawerBase(dir string) string {
	if s := w.selected(); s != nil && s.Root != nil {
		for _, p := range s.Root.panes() {
			if p.ID == w.FocusedPane && p.Worktree != nil && sameDir(p.Worktree.Path, dir) {
				if p.Worktree.Base != "" {
					return p.Worktree.Base
				}
				break
			}
		}
	}
	return detectBaseBranch(dir)
}

// drawerError is what to say when the read failed.
func drawerError(err error) string {
	if err == errNotRepo {
		return locales.DrawerNotRepo()
	}
	return err.Error()
}

// startGitMonitor keeps the branch pill, and the drawer's list while it is
// up, following the working tree, so a build or an agent's edits land without
// a click.
func (w *workspace) startGitMonitor() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if w.win == nil {
				continue
			}
			w.onMain(func() {
				w.refreshGit()
			})
		}
	}()
}

// openChange shows a change's diff over the list.
func (w *workspace) openChange(f changedFile) {
	dir := w.drawerDir()
	if dir == "" {
		return
	}
	file := &drawerFile{Path: f.Path, Status: f.Status, Added: f.Added, Deleted: f.Deleted}
	w.drawer.file = file
	w.drawer.viewGen++
	gen := w.drawer.viewGen
	rev := ""
	if w.drawer.scope == drawerScopeBranch {
		rev = w.drawer.branchBase
	}
	w.invalidate()
	go func() {
		diff := loadDiff(dir, f)
		if rev != "" {
			diff = loadDiffAt(dir, f, rev)
		}
		w.onMain(func() {
			if w.drawer.viewGen != gen || w.drawer.file != file {
				return
			}
			switch {
			case diff.TooLarge:
				file.Note = locales.DrawerTooLarge()
			case diff.Binary:
				file.Note = locales.DrawerBinary()
			default:
				oldText := editor.NewBuffer(diff.Old)
				newText := editor.NewBuffer(diff.New)
				if lang := editor.LanguageFor(f.Path); lang != nil {
					oldText.SetLanguage(lang)
					newText.SetLanguage(lang)
				}
				file.Diff = &editor.DiffState{
					Old:    oldText,
					New:    newText,
					Layout: editor.DiffUnified,
				}
			}
			file.Ready = true
			w.invalidate()
		})
	}()
}

// openFile shows a file from the tree, read-only, over the list.
func (w *workspace) openFile(path string) {
	dir := w.drawerDir()
	if dir == "" {
		return
	}
	file := &drawerFile{Path: path}
	w.drawer.file = file
	w.drawer.viewGen++
	gen := w.drawer.viewGen
	w.invalidate()
	go func() {
		text, binary, large, err := loadText(dir, path)
		w.onMain(func() {
			if w.drawer.viewGen != gen || w.drawer.file != file {
				return
			}
			switch {
			case err != nil:
				file.Note = err.Error()
			case large:
				file.Note = locales.DrawerTooLarge()
			case binary:
				file.Note = locales.DrawerBinary()
			default:
				buf := editor.NewBuffer(text)
				if lang := editor.LanguageFor(path); lang != nil {
					buf.SetLanguage(lang)
				}
				file.View = &editor.State{Buffer: buf, ReadOnly: true}
			}
			file.Ready = true
			w.invalidate()
		})
	}()
}

// closeDrawerFile puts the list back.
func (w *workspace) closeDrawerFile() {
	w.drawer.file = nil
	w.drawer.viewGen++
	w.invalidate()
	if w.drawer.tab == drawerTabChanges {
		w.refreshGit()
		if w.drawer.scope == drawerScopeBranch {
			w.refreshBranch()
		}
	}
}

// drawer is the column itself. It is built only while it opens, shows, or
// slides out, so a closed drawer costs nothing.
func (w *workspace) drawerView(c *ui.Context, tk tokens) {
	d := &w.drawer
	if !d.open && !d.closing {
		return
	}
	w.syncDrawerDir()
	c.Key("drawer")
	el := ui.Column(c).Absolute().Top(0).Bottom(0).Right(0)
	width := el.Animate("drawerWidth", d.width(), sidebarMotion)
	// A drawer is its own surface: the theme's background and a hairline, not
	// glass. Glass over a terminal in light mode reads as a grey sheet, which
	// is not what the theme's white is meant to say.
	el = el.Width(width)
	el = sheet(el.Background(tk.Background).BorderWidth(0, 0, 0, 1).BorderColor(tk.Border), tk)
	el = el.Clip()
	// A drawer is an overlay, so Escape closes it wherever the keyboard is.
	if el.OverlayShortcut(0, ui.KeyEscape) {
		w.closeDrawer()
	}
	pose := ui.Pose{}
	if d.closing {
		pose = ui.Pose{X: 32, Fade: 1}
	} else if !d.shown {
		// The first frame starts it off the edge and transparent; the next
		// frame, a millisecond later, lets it slide and fade in.
		d.shown = true
		pose = ui.Pose{X: 32, Fade: 1}
		c.After(time.Millisecond)
	}
	el = el.AnimatePose(pose, ui.Animation{Duration: sidebarMotion, Ease: ui.EaseOut})
	el.Children(func() {
		w.drawerResizer(c, tk)
		w.drawerHeader(c, tk)
		if d.file != nil {
			w.drawerFileView(c, tk, d.file)
			return
		}
		switch {
		case w.drawerProject() == nil:
			w.drawerNote(c, tk, locales.DrawerNoProject(), "")
		case d.tab == drawerTabChanges:
			w.drawerScopeRow(c, tk)
			w.drawerChanges(c, tk)
		default:
			w.drawerFiles(c, tk)
		}
	})
}

// drawerResizer is the drawer's left edge, which the pointer drags to widen
// or narrow it. The width it lands on is the one the next launch restores.
func (w *workspace) drawerResizer(c *ui.Context, tk tokens) {
	edge := ui.Box(c).Absolute().Left(-3).Top(0).Width(6).FillHeight().
		Cursor(ui.CursorResizeEW)
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		d := &w.drawer
		next := d.width() - dx
		if next < 240 {
			next = 240
		}
		if next > 900 {
			next = 900
		}
		if d.file != nil {
			d.fileWidth = next
		} else {
			d.listWidth = next
		}
		w.invalidate()
		w.scheduleSave()
	}
}

// drawerHeader holds the drawer's two sections and the close button. The
// sections are labels standing on the header's own edge, with an ink rule
// under the one in front: the scope row below them is the quiet control, so
// the header reads as a bar rather than as a second toolbar.
func (w *workspace) drawerHeader(c *ui.Context, tk tokens) {
	d := &w.drawer
	ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Padding(0, 6, 0, 6).
		Gap(2).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		count := ""
		if n := len(d.listedChanges()); n > 0 {
			count = strconv.Itoa(n)
		}
		w.drawerTab(c, tk, locales.DrawerChanges(), count, d.tab == drawerTabChanges, func() {
			w.setDrawerTab(drawerTabChanges)
		})
		w.drawerTab(c, tk, locales.DrawerFiles(), "", d.tab == drawerTabFiles, func() {
			w.setDrawerTab(drawerTabFiles)
		})
		ui.Spacer(c)
		iconButtonInFrame(c, tk, "x", func() { w.closeDrawer() }, locales.DrawerClose())
	})
}

// drawerTab is one section's name on the header, with the count of what it
// holds beside it and an ink rule under the section in front.
func (w *workspace) drawerTab(c *ui.Context, tk tokens, label, count string, active bool, pick func()) {
	color := tk.MutedForeground
	if active {
		color = tk.Foreground
	}
	el := ui.Row(c).Height(headerHeight).Padding(0, 7, 0, 7).Gap(5).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if !active && el.Hovered() {
		color = tk.Foreground
	}
	el.OnClick(pick).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(color).OnClick(pick)
		if count != "" {
			ui.Text(c, count).FontSize(10.5).TextColor(tk.MutedForeground).OnClick(pick)
		}
		if active {
			ui.Box(c).Absolute().Left(7).Right(7).Bottom(0).Height(2).
				Radius(1).Background(tk.Foreground).IgnoreInput(true)
		}
	})
}

// listedChanges is the change list the drawer's section count reports: the
// working tree's, or the branch's, whichever the scope row puts up.
func (d *drawerState) listedChanges() []changedFile {
	if d.scope == drawerScopeBranch {
		return d.branchFiles
	}
	return d.changes
}

// drawerScopeRow switches the changes list between the working tree and the
// branch, and names the base the branch is measured against. It is a pair of
// quiet pills: the header's tabs are the drawer's sections, this only filters
// the one they show.
func (w *workspace) drawerScopeRow(c *ui.Context, tk tokens) {
	d := &w.drawer
	ui.Row(c).Height(32).Shrink(0).FillWidth().Padding(5, 8, 5, 5).
		Gap(2).AlignItems(ui.Center).Children(func() {
		for _, opt := range []struct {
			scope int
			label string
		}{
			{drawerScopeWorktree, locales.DrawerUncommitted()},
			{drawerScopeBranch, locales.DrawerBranch()},
		} {
			opt := opt
			selected := d.scope == opt.scope
			color := tk.MutedForeground
			el := ui.Row(c).Height(22).Padding(0, 8, 0, 8).Radius(6).
				Cursor(ui.CursorPointer)
			fill := ui.Transparent
			if selected {
				color = tk.Foreground
				// A step up from the drawer's paper: a grey on the
				// light sheet, a tenth of the ink on the dark one, as
				// the sidebar's chosen row is drawn.
				fill = tk.Active
				if tk.Dark {
					fill = tk.Foreground.Alpha(0.10)
				}
			} else if el.Hovered() {
				fill = hoverInk(c, tk)
			}
			el = el.Background(el.AnimateColor("scopeFill", fill,
				ui.Animation{Duration: hoverMotion, Ease: ui.EaseOut}))
			pick := func() { w.setDrawerScope(opt.scope) }
			el.OnClick(pick).Children(func() {
				ui.Text(c, opt.label).FontSize(11).FontWeight(500).
					TextColor(color).OnClick(pick)
			})
		}
		ui.Spacer(c)
		if d.scope == drawerScopeBranch && d.branchName != "" {
			ui.Text(c, locales.DrawerAgainst(d.branchName)).FontSize(11).
				SingleLine().TextColor(tk.MutedForeground)
		}
	})
}

// setDrawerScope puts the other scope up and reads the branch side when it
// has not been read for this checkout yet.
func (w *workspace) setDrawerScope(scope int) {
	d := &w.drawer
	if d.scope == scope {
		return
	}
	d.scope = scope
	d.file = nil
	d.viewGen++
	w.invalidate()
	if scope == drawerScopeBranch {
		w.refreshBranch()
	}
}

// drawerChanges lists the working tree's changes, or the branch's against its
// base, gathered by folder with the line counts Git measured, and closes with
// a foot of totals. A click opens the diff over the list.
func (w *workspace) drawerChanges(c *ui.Context, tk tokens) {
	d := &w.drawer
	files, err, busy := d.changes, d.err, w.gitBusy[d.dir]
	if d.scope == drawerScopeBranch {
		files, err, busy = d.branchFiles, d.branchErr, d.branchBusy
		// A folder Git does not track says that, rather than inventing a
		// missing base for it.
		if err == "" && d.err != "" {
			err = d.err
		}
	}
	switch {
	case err != "":
		w.drawerNote(c, tk, err, "")
		return
	case len(files) == 0 && busy:
		w.drawerNote(c, tk, locales.DrawerLoading(), "")
		return
	case len(files) == 0:
		if d.scope == drawerScopeBranch {
			if d.branchName == "" {
				w.drawerNote(c, tk, locales.DrawerNoBase(), "")
			} else {
				w.drawerNote(c, tk, locales.DrawerNoBranchChanges(d.branchName), "")
			}
			return
		}
		w.drawerNote(c, tk, locales.DrawerNoChanges(), locales.DrawerCleanHint())
		return
	}
	root := filepath.Base(d.dir)
	ui.Column(c).Fill().Children(func() {
		ui.Scroll(c).Fill().Padding(4, 6, 10, 6).Children(func() {
			for _, g := range groupChanges(files) {
				label := g.dir
				if label == "" {
					// The checkout's own files read under its name, so the
					// folder they sit in is named once, as the others are.
					label = root
				}
				w.drawerGroupLabel(c, tk, label)
				for _, f := range g.files {
					w.drawerChangeRow(c, tk, f)
				}
			}
		})
		w.drawerSummary(c, tk, files)
	})
}

// changeGroup is one folder's changes, as the list shows them.
type changeGroup struct {
	dir   string
	files []changedFile
}

// groupChanges gathers the changes by folder, the checkout's own files first
// and the folders after them, so a path reads once instead of on every row.
// The order within a folder is the one Git gave, which is by name.
func groupChanges(files []changedFile) []changeGroup {
	byDir := map[string][]changedFile{}
	var dirs []string
	for _, f := range files {
		dir := f.dir()
		if _, ok := byDir[dir]; !ok {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], f)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if (dirs[i] == "") != (dirs[j] == "") {
			return dirs[i] == ""
		}
		return strings.ToLower(dirs[i]) < strings.ToLower(dirs[j])
	})
	groups := make([]changeGroup, 0, len(dirs))
	for _, dir := range dirs {
		groups = append(groups, changeGroup{dir: dir, files: byDir[dir]})
	}
	return groups
}

// drawerGroupLabel names a folder above its rows: a quiet line, quieter than
// the files it holds. Its mark fills the same 16pt slot a change's mark does,
// and its name starts where a file's name does, so the column reads straight
// down.
func (w *workspace) drawerGroupLabel(c *ui.Context, tk tokens, label string) {
	ui.Row(c).Height(26).FillWidth().PaddingStart(8).PaddingEnd(8).Gap(8).
		AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(16).Height(16).Center().Children(func() {
			if src := iconSource("folder", colorHex(tk.MutedForeground)); src != nil {
				ui.Image(c, src).Width(13).Height(13).IgnoreInput(true).Opacity(0.75)
			}
		})
		ui.Text(c, label).FontSize(11).FontWeight(500).SingleLine().
			TextColor(tk.MutedForeground)
	})
}

// drawerChangeRow is one change: its mark, its name, and what it costs in
// lines, aligned at the right so the column can be read down.
func (w *workspace) drawerChangeRow(c *ui.Context, tk tokens, f changedFile) {
	pick := func() { w.openChange(f) }
	row := ui.Row(c).FillWidth().Height(30).Padding(0, 8, 0, 8).Gap(8).
		Radius(chromeRadius).Cursor(ui.CursorPointer)
	if row.Hovered() {
		if liquid(c) {
			row = rowSkin(row, c, tk)
		} else {
			row = row.Background(tk.Active)
		}
	}
	row.OnClick(pick).Children(func() {
		w.drawerStatusChip(c, tk, f.Status, pick)
		ui.Text(c, f.name()).FontSize(12).SingleLine().Shrink(1).
			TextColor(tk.Foreground).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
		drawerLineCounts(c, tk, f.Added, f.Deleted, pick)
	})
}

// drawerStatusChip is a change's letter on its own tint: the letters stack
// into a column the eye can run down, and the letter says what the colour
// only repeats. The tint is mixed into the drawer's own paper rather than
// laid over it, so the letter can be checked against what it really sits on
// and darkened until it reads.
func (w *workspace) drawerStatusChip(c *ui.Context, tk tokens, status byte, pick func()) {
	if status == 0 {
		return
	}
	color := changeColor(tk, status)
	share := float32(0.12)
	if tk.Dark {
		share = 0.18
	}
	tint := tk.Background.MixOklab(color, share)
	ink := legible(color, tint, tk.Foreground, 4.5)
	ui.Row(c).Width(16).Height(16).Radius(4).Center().Shrink(0).
		Background(tint).OnClick(pick).Children(func() {
		ui.Text(c, string(status)).FontSize(9.5).FontWeight(600).
			TextColor(ink).OnClick(pick)
	})
}

// drawerLineCounts is a change's added and deleted lines, in the colours the
// branch pill uses for the same numbers. Each side keeps a column of its own,
// so the numbers line up down the list even where one side is empty.
func drawerLineCounts(c *ui.Context, tk tokens, added, deleted int, pick func()) {
	if added == 0 && deleted == 0 {
		return
	}
	column := func(value int, sign string, color ui.Color, width float32) {
		if value == 0 {
			ui.Box(c).Width(width).OnClick(pick)
			return
		}
		ui.Text(c, sign+strconv.Itoa(value)).FontSize(10).Width(width).
			TextAlign(ui.End).TextColor(color).OnClick(pick)
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
		column(added, "+", tk.Working, 30)
		column(deleted, "−", tk.Danger, 24)
	})
}

// drawerSummary is the list's foot: how many files, and how the lines divide
// between what the change adds and what it takes away.
func (w *workspace) drawerSummary(c *ui.Context, tk tokens, files []changedFile) {
	added, deleted := 0, 0
	for _, f := range files {
		added += f.Added
		deleted += f.Deleted
	}
	ui.Row(c).Height(32).Shrink(0).FillWidth().Padding(0, 14, 0, 14).
		Gap(6).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).Children(func() {
		ui.Text(c, locales.DrawerFileCount(len(files))).FontSize(11).
			TextColor(tk.MutedForeground)
		ui.Spacer(c)
		if added+deleted == 0 {
			return
		}
		drawerChangeBar(c, tk, added, deleted)
		if added > 0 {
			ui.Text(c, "+"+strconv.Itoa(added)).FontSize(10).TextColor(tk.Working)
		}
		if deleted > 0 {
			ui.Text(c, "−"+strconv.Itoa(deleted)).FontSize(10).TextColor(tk.Danger)
		}
	})
}

// drawerChangeBar is the summary's little measure: one bar split between the
// added and the deleted lines, so the shape of the change reads before the
// numbers do.
func drawerChangeBar(c *ui.Context, tk tokens, added, deleted int) {
	const width float32 = 46
	addW := width * float32(added) / float32(added+deleted)
	if added > 0 && addW < 2 {
		addW = 2
	}
	if deleted > 0 && width-addW < 2 {
		addW = width - 2
	}
	ui.Row(c).Width(width).Height(4).Radius(2).Background(tk.Active).
		Clip().Shrink(0).Children(func() {
		if added > 0 {
			ui.Box(c).Width(addW).FillHeight().Background(tk.Working)
		}
		if deleted > 0 {
			ui.Box(c).Width(width - addW).FillHeight().Background(tk.Danger)
		}
	})
}

// drawerFiles walks the project's folders, opening one at a time.
func (w *workspace) drawerFiles(c *ui.Context, tk tokens) {
	dir := w.drawerDir()
	if dir == "" {
		return
	}
	ui.Scroll(c).Fill().Padding(6, 6, 6, 6).Children(func() {
		w.drawerTree(c, tk, dir, "", 0)
	})
}

// fileIconName is the mark a drawer row wears for a file, chosen by its
// extension. Every name here has a PNG in icons/; anything unknown is a plain
// document.
func fileIconName(name string) string {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case "readme", "license", "dockerfile", "makefile",
		".gitignore", ".gitattributes", ".npmrc", ".editorconfig":
		return "file-text"
	}
	switch filepath.Ext(base) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".rb", ".rs",
		".c", ".h", ".cc", ".cpp", ".hpp", ".java", ".kt", ".kts", ".swift", ".php",
		".cs", ".scala", ".m", ".mm", ".lua", ".dart", ".vue", ".svelte":
		return "file-code"
	case ".md", ".markdown", ".txt", ".rst", ".log":
		return "file-text"
	case ".json", ".jsonc", ".yaml", ".yml", ".toml", ".xml", ".plist", ".conf", ".ini":
		return "file-braces"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".bmp", ".tiff", ".heic":
		return "file-image"
	case ".lock", ".pem", ".key", ".crt", ".cer":
		return "file-lock"
	case ".sh", ".bash", ".zsh", ".fish":
		return "terminal"
	}
	return "file"
}

// drawerTree adds rel's entries under the rows already drawn for its parent.
func (w *workspace) drawerTree(c *ui.Context, tk tokens, dir, rel string, depth int) {
	for _, e := range w.drawerEntries(dir, rel) {
		e := e
		row := ui.Row(c).FillWidth().Height(30).PaddingStart(8 + float32(depth)*12).
			PaddingEnd(8).Gap(6).Radius(chromeRadius).Cursor(ui.CursorPointer)
		if row.Hovered() {
			if liquid(c) {
				row = rowSkin(row, c, tk)
			} else {
				row = row.Background(tk.Active)
			}
		}
		if e.dir {
			open := w.drawer.expanded[e.path]
			toggle := func() {
				w.drawer.expanded[e.path] = !open
				w.invalidate()
			}
			row.OnClick(toggle).Children(func() {
				name := "chevron-right"
				if open {
					name = "chevron-down"
				}
				if src := iconSource(name, colorHex(tk.MutedForeground)); src != nil {
					ui.Image(c, src).Width(13).Height(13).OnClick(toggle)
				}
				if src := iconSource("folder", colorHex(tk.MutedForeground)); src != nil {
					ui.Image(c, src).Width(14).Height(14).IgnoreInput(true)
				}
				ui.Text(c, e.name).FontSize(12).SingleLine().
					TextColor(tk.Foreground).OnClick(toggle)
				ui.Spacer(c).OnClick(toggle)
			})
			if open {
				// The rows open under the folder, in the same scroll view.
				w.drawerTree(c, tk, dir, e.path, depth+1)
			}
			continue
		}
		pick := func() { w.openFile(e.path) }
		row.OnClick(pick).Children(func() {
			// A file leaves the arrow's room empty and wears its own mark,
			// so every name starts at one edge. A Box, not a Spacer: a
			// Spacer would grow and push the name to the middle.
			ui.Box(c).Width(13).OnClick(pick)
			if src := iconSource(fileIconName(e.name), colorHex(tk.MutedForeground)); src != nil {
				ui.Image(c, src).Width(14).Height(14).IgnoreInput(true).OnClick(pick)
			}
			ui.Text(c, e.name).FontSize(12).SingleLine().
				TextColor(tk.Foreground).OnClick(pick)
			ui.Spacer(c).OnClick(pick)
		})
	}
}

// drawerEntries reads one folder once and keeps the listing; opening and
// closing a folder in a frame never touches the disk again.
func (w *workspace) drawerEntries(dir, rel string) []drawerEntry {
	if list, ok := w.drawer.dirs[rel]; ok {
		return list
	}
	entries, err := readDrawerDir(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		entries = nil
	}
	for i := range entries {
		entries[i].path = path.Join(rel, entries[i].name)
	}
	w.drawer.dirs[rel] = entries
	return entries
}

// readDrawerDir lists a folder the way the tree shows it: folders first, then
// files, each alphabetical, with .git left out so the repository's own noise
// stays out of the way.
func readDrawerDir(dir string) ([]drawerEntry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []drawerEntry
	for _, it := range items {
		if it.Name() == ".git" {
			continue
		}
		out = append(out, drawerEntry{
			name: it.Name(),
			path: it.Name(),
			dir:  it.IsDir(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].dir != out[j].dir {
			return out[i].dir
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out, nil
}

// drawerFileView is one file over the list: its diff, its text, or why it has
// neither.
func (w *workspace) drawerFileView(c *ui.Context, tk tokens, file *drawerFile) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Padding(0, 6, 0, 6).
			Gap(6).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
			back := func() { w.closeDrawerFile() }
			el := ui.Row(c).Width(iconSize).Height(iconSize).Radius(chromeRadius).
				Border(1, ui.Transparent).
				Center().Cursor(ui.CursorPointer)
			el = hoverWash(c, el, tk)
			el.OnClick(back).Tooltip(locales.DrawerBack()).Children(func() {
				if src := iconSource("chevron-right", colorHex(tk.MutedForeground)); src != nil {
					ui.Image(c, src).Width(15).Height(15).Rotate(180).OnClick(back)
				}
			})
			dir := filepath.Dir(file.Path)
			if dir == "." {
				dir = ""
			}
			w.drawerStatusChip(c, tk, file.Status, func() {})
			ui.Column(c).Gap(1).Shrink(1).Children(func() {
				ui.Text(c, filepath.Base(file.Path)).FontSize(12).FontWeight(500).
					SingleLine().TextColor(tk.Foreground)
				if dir != "" {
					ui.Text(c, dir).FontSize(11).SingleLine().
						TextColor(tk.MutedForeground)
				}
			})
			ui.Spacer(c)
			drawerLineCounts(c, tk, file.Added, file.Deleted, func() {})
		})
		body := ui.Column(c).Fill()
		switch {
		case !file.Ready:
			body.Padding(16).Children(func() {
				ui.Text(c, locales.DrawerLoading()).FontSize(12).
					TextColor(tk.MutedForeground)
			})
		case file.Note != "":
			body.Padding(16).Children(func() {
				ui.Text(c, file.Note).FontSize(12).TextColor(tk.MutedForeground)
			})
		case file.Diff != nil:
			body.Children(func() { editor.DiffView(c, file.Diff).Fill() })
		case file.View != nil:
			body.Children(func() { editor.View(c, file.View).Fill() })
		}
	})
}

// drawerNote is a centered pair of lines in place of a list: what is missing,
// and what that means, quieter than the list it stands in for.
func (w *workspace) drawerNote(c *ui.Context, tk tokens, title, hint string) {
	ui.Column(c).Fill().Padding(24).Center().Gap(5).Children(func() {
		ui.Text(c, title).FontSize(12.5).FontWeight(500).TextColor(tk.Foreground).
			TextAlign(ui.Center)
		if hint != "" {
			ui.Text(c, hint).FontSize(11).TextColor(tk.MutedForeground).
				TextAlign(ui.Center)
		}
	})
}

// changeColor tints a change's letter: amber for a modification, green for
// something new, red for something gone or in conflict.
func changeColor(tk tokens, status byte) ui.Color {
	switch status {
	case 'U', 'A':
		return tk.Working
	case 'D', '!':
		return tk.Danger
	case 'R', 'C':
		return tk.MutedForeground
	default:
		return tk.Waiting
	}
}
