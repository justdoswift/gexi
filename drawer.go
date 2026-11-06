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
// It reads a local project only. A remote session's folder lives on the other
// Mac, where Git would have to run too.

const (
	drawerTabChanges = iota
	drawerTabFiles

	drawerListWidth float32 = 320
	drawerFileWidth float32 = 620
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
		d.expanded = map[string]bool{}
		d.dirs = map[string][]drawerEntry{}
		d.file = nil
		d.viewGen++
	}
	return dir
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
	if pr == nil || pr.RemoteHost != "" {
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
	pr := w.projectOf(w.sessionOf(p))
	if pr == nil || pr.RemoteHost != "" {
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
func (w *workspace) branchPillWidth(c *ui.Context, p *pane) float32 {
	info, ok := w.branchPillInfo(p)
	if !ok {
		return 0
	}
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
// title as Rex's does, and is the drawer's everyday way in.
func (w *workspace) branchPill(c *ui.Context, tk tokens, p *pane) {
	info, ok := w.branchPillInfo(p)
	if !ok {
		return
	}
	d := &w.drawer
	showing := d.open && d.tab == drawerTabChanges && d.dir == info.dir
	// A change Git counts no lines for has no +N or −N to show, so the
	// branch name itself carries the colour.
	unmeasured := info.changed && info.added+info.deleted == 0
	bg := tk.Active
	color := tk.MutedForeground
	if showing {
		bg, color = tk.Control, tk.Foreground
	}
	el := ui.Row(c).Height(22).Radius(chromeRadius).Padding(0, 8).Gap(5).
		AlignItems(ui.Center).Shrink(1).Cursor(ui.CursorPointer)
	if el.Hovered() {
		bg, color = tk.Control, tk.Foreground
	}
	pick := func() { w.toggleChangesDrawer(info.dir) }
	el.Background(bg).Tooltip(locales.DrawerOpenChanges()).OnClick(pick).Children(func() {
		if src := iconSource("branch", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(12).Height(12).IgnoreInput(true)
		}
		textColor := color
		if unmeasured {
			textColor = tk.Waiting
		}
		ui.Text(c, shortBranch(info.branch)).FontSize(11).SingleLine().
			Shrink(1).TextColor(textColor).OnClick(pick)
		if info.added > 0 {
			ui.Text(c, "+"+strconv.Itoa(info.added)).FontSize(10).Shrink(0).
				TextColor(tk.Working).OnClick(pick)
		}
		if info.deleted > 0 {
			ui.Text(c, "−"+strconv.Itoa(info.deleted)).FontSize(10).Shrink(0).
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
	d := &w.drawer
	d.open = true
	d.closing = false
	d.shown = false
	d.tab = tab
	w.resetDrawer()
	w.invalidate()
	w.scheduleSave()
	if tab == drawerTabChanges {
		w.refreshGit()
	}
}

func (w *workspace) closeDrawer() {
	d := &w.drawer
	if !d.open {
		return
	}
	d.open = false
	d.closing = true
	w.invalidate()
	w.scheduleSave()
	time.AfterFunc(sidebarMotion, func() {
		w.onMain(func() {
			if w.drawer.open {
				return
			}
			w.drawer.closing = false
			w.drawer.shown = false
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
		})
	}()
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
	file := &drawerFile{Path: f.Path}
	w.drawer.file = file
	w.drawer.viewGen++
	gen := w.drawer.viewGen
	w.invalidate()
	go func() {
		diff := loadDiff(dir, f)
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
	el := ui.Column(c).Absolute().Top(0).Bottom(0).Right(0)
	width := el.Animate("drawerWidth", d.width(), sidebarMotion)
	el = el.Width(width).Background(tk.Background).
		BorderWidth(0, 0, 0, 1).BorderColor(tk.Border).Clip()
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
			w.drawerNote(c, tk, locales.DrawerNoProject())
		case w.drawerProject().RemoteHost != "":
			w.drawerNote(c, tk, locales.DrawerRemote())
		case d.tab == drawerTabChanges:
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

// drawerHeader holds the two tabs and the close button.
func (w *workspace) drawerHeader(c *ui.Context, tk tokens) {
	ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Padding(0, 8, 0, 6).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		selected := "changes"
		if w.drawer.tab == drawerTabFiles {
			selected = "files"
		}
		// The segmented track is shorter than the header band, so it reads
		// as a switch inside the bar rather than a second bar.
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			w.segmented(c, tk, []segment{
				{value: "changes", label: locales.DrawerChanges()},
				{value: "files", label: locales.DrawerFiles()},
			}, selected, func(value string) {
				if value == "files" {
					w.setDrawerTab(drawerTabFiles)
				} else {
					w.setDrawerTab(drawerTabChanges)
				}
			})
		})
		ui.Spacer(c)
		iconButton(c, tk, "x", func() { w.closeDrawer() }, locales.DrawerClose())
	})
}

// drawerChanges lists the working tree's changes. A click opens the diff
// over the list.
func (w *workspace) drawerChanges(c *ui.Context, tk tokens) {
	d := &w.drawer
	switch {
	case d.err != "":
		w.drawerNote(c, tk, d.err)
		return
	case len(d.changes) == 0 && w.gitBusy[d.dir]:
		w.drawerNote(c, tk, locales.DrawerLoading())
		return
	case len(d.changes) == 0:
		w.drawerNote(c, tk, locales.DrawerNoChanges())
		return
	}
	ui.Scroll(c).Fill().Padding(6, 6, 6, 6).Children(func() {
		for _, f := range d.changes {
			f := f
			row := ui.Row(c).FillWidth().Height(28).Padding(0, 8, 0, 8).Gap(8).
				Radius(chromeRadius).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row = row.Background(tk.Active)
			}
			pick := func() { w.openChange(f) }
			row.OnClick(pick).Children(func() {
				ui.Text(c, string(f.Status)).FontSize(10).FontWeight(600).
					Width(12).TextColor(changeColor(tk, f.Status)).OnClick(pick)
				ui.Text(c, f.name()).FontSize(12).SingleLine().
					TextColor(tk.Foreground).OnClick(pick)
				if dir := f.dir(); dir != "" {
					ui.Text(c, dir).FontSize(11).SingleLine().
						TextColor(tk.MutedForeground).OnClick(pick)
				}
				ui.Spacer(c).OnClick(pick)
			})
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

// drawerTree adds rel's entries under the rows already drawn for its parent.
func (w *workspace) drawerTree(c *ui.Context, tk tokens, dir, rel string, depth int) {
	for _, e := range w.drawerEntries(dir, rel) {
		e := e
		row := ui.Row(c).FillWidth().Height(28).PaddingStart(8 + float32(depth)*12).
			PaddingEnd(8).Gap(5).Radius(chromeRadius).Cursor(ui.CursorPointer)
		if row.Hovered() {
			row = row.Background(tk.Active)
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
			// A file's row leaves the arrow's and the folder icon's room
			// empty, so every name starts at one edge. A Box, not a Spacer:
			// a Spacer would grow and push the name to the middle.
			ui.Box(c).Width(32).OnClick(pick)
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
			el := ui.Row(c).Width(iconSize).Height(iconSize).Radius(iconSize / 2).
				Center().Cursor(ui.CursorPointer)
			if el.Hovered() {
				el = el.Background(tk.Control)
			}
			el.OnClick(back).Tooltip(locales.DrawerBack()).Children(func() {
				if src := iconSource("chevron-right", colorHex(tk.MutedForeground)); src != nil {
					ui.Image(c, src).Width(15).Height(15).Rotate(180).OnClick(back)
				}
			})
			dir := filepath.Dir(file.Path)
			if dir == "." {
				dir = ""
			}
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, filepath.Base(file.Path)).FontSize(12).FontWeight(500).
					SingleLine().TextColor(tk.Foreground)
				if dir != "" {
					ui.Text(c, dir).FontSize(11).SingleLine().
						TextColor(tk.MutedForeground)
				}
			})
			ui.Spacer(c)
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

// drawerNote is a centered line of explanation in place of a list.
func (w *workspace) drawerNote(c *ui.Context, tk tokens, text string) {
	ui.Column(c).Fill().Padding(24).Center().Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(tk.MutedForeground).
			TextAlign(ui.Center).SingleLine()
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
