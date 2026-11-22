package main

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A session has three places to be. Active sessions run in the sidebar under
// their project. Idle sessions wait in a fold at the bottom of the sidebar:
// stopped, and started again only from their menu, so a stray click cannot
// resume work. Archived sessions are kept in Settings › Archive, out of the
// everyday list until they are restored or deleted. Both idle and archived
// sessions keep their panes' commands and last screens, so opening one
// resumes the same work.

// putSessionAway records the session's panes, stops them, and files the
// record in the idle fold or the archive.
func (w *workspace) putSessionAway(s *session, archive bool) bool {
	if s == nil || s.Root == nil || s.remote != nil || w.sessionByID(s.ID) != s {
		return false
	}
	pr := w.projectOf(s)
	if pr == nil {
		return false
	}
	rec := storedSessionFor(s, pr)
	for _, p := range s.Root.panes() {
		w.saveSnapshot(p)
	}
	// Stop the panes before the record appears, so no row can be clicked
	// while the session it names is still in the layout.
	w.stopSession(s)
	if archive {
		w.archivedSessions = append([]storedSession{rec}, w.archivedSessions...)
	} else {
		w.idleSessions = append([]storedSession{rec}, w.idleSessions...)
	}
	if archive {
		w.flash(locales.SessionArchivedFlash())
	} else {
		w.flash(locales.SessionIdleFlash())
	}
	w.scheduleSave()
	w.invalidate()
	w.invalidateSettings()
	return true
}

// storedSessionFor freezes everything opening the session again needs.
func storedSessionFor(s *session, pr *project) storedSession {
	return storedSession{
		Session: savedSession{
			ID:         s.ID,
			Label:      s.Label,
			Root:       s.Root.saved(),
			ZoomedPane: s.zoomedPane,
		},
		ProjectDir:   pr.Dir,
		ProjectLabel: pr.name(),
		Title:        sessionLabel(s),
	}
}

// restoreIdleSession opens a session from the sidebar's fold and takes it
// out of the fold.
func (w *workspace) restoreIdleSession(id string) {
	rec, ok := findStoredSession(w.idleSessions, id)
	if !ok {
		return
	}
	if w.openStoredSession(rec) == nil {
		return
	}
	w.idleSessions = removeStoredSession(w.idleSessions, id)
	w.scheduleSave()
	w.invalidate()
}

// restoreArchivedSession opens a session from Settings and brings the window
// forward, as a worktree's Restore does.
func (w *workspace) restoreArchivedSession(id string) {
	rec, ok := findStoredSession(w.archivedSessions, id)
	if !ok {
		return
	}
	if w.openStoredSession(rec) == nil {
		return
	}
	w.archivedSessions = removeStoredSession(w.archivedSessions, id)
	w.scheduleSave()
	w.dismissSettings()
	w.invalidateSettings()
	if w.win != nil {
		w.win.Show()
		w.win.Focus()
	}
}

// archiveIdleSession moves a waiting session from the sidebar's fold into
// Settings › Archive. Nothing is running, so only the list changes.
func (w *workspace) archiveIdleSession(id string) {
	rec, ok := findStoredSession(w.idleSessions, id)
	if !ok {
		return
	}
	w.idleSessions = removeStoredSession(w.idleSessions, id)
	w.archivedSessions = append([]storedSession{rec}, w.archivedSessions...)
	w.flash(locales.SessionArchivedFlash())
	w.scheduleSave()
	w.invalidate()
	w.invalidateSettings()
}

// openStoredSession starts an idle or archived session's panes again, in the
// project it came from, and selects it. It returns nil when the saved tree
// cannot be opened.
func (w *workspace) openStoredSession(rec storedSession) *session {
	if s := w.sessionByID(rec.Session.ID); s != nil {
		w.selectSession(s.ID)
		return s
	}
	if rec.Session.Root.Type == "" {
		return nil
	}
	dir := rec.ProjectDir
	if dir == "" {
		dir = savedSessionDir(rec.Session.Root)
	}
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		dir, _ = os.UserHomeDir()
	}
	pr := w.projectFor(filepath.Clean(dir))
	if pr.Label == "" && rec.ProjectLabel != "" && rec.ProjectLabel != filepath.Base(pr.Dir) {
		pr.Label = rec.ProjectLabel
	}
	rec.Session.Root.dropMissingWorktrees(pr.Dir)
	root, err := rec.Session.Root.restore(w)
	if err != nil {
		w.flash(locales.SessionOpenFailed(err.Error()))
		return nil
	}
	s := &session{ID: rec.Session.ID, Label: rec.Session.Label, Root: root}
	if root.paneByID(rec.Session.ZoomedPane) != nil {
		s.zoomedPane = rec.Session.ZoomedPane
	}
	pr.Sessions = append(pr.Sessions, s)
	delete(w.collapsed, pr.ID)
	w.selectSession(s.ID)
	w.scheduleSave()
	return s
}

// confirmDeleteArchivedSession asks before an archived session is forgotten
// for good.
func (w *workspace) confirmDeleteArchivedSession(id string) {
	if _, ok := findStoredSession(w.archivedSessions, id); !ok {
		return
	}
	rec, ok := findStoredSession(w.archivedSessions, id)
	if !ok {
		return
	}
	w.archivedDeleteID = id
	w.askConfirm(nativeAsk{
		kind:        mygo.MessageWarning,
		title:       locales.ArchiveSessionDeleteTitle(),
		message:     locales.ArchiveSessionDeleteBody(storedSessionTitle(rec)),
		buttons:     []string{locales.Cancel(), locales.ArchiveDelete()},
		def:         0,
		cancel:      0,
		destructive: 1,
	}, func(button int) {
		if button == 1 {
			w.deleteArchivedSession(id)
		}
		w.archivedDeleteID = ""
	})
	w.invalidateSettings()
}

// deleteArchivedSession forgets the saved session and the screens kept for
// its panes.
func (w *workspace) deleteArchivedSession(id string) {
	rec, ok := findStoredSession(w.archivedSessions, id)
	if !ok {
		return
	}
	w.archivedSessions = removeStoredSession(w.archivedSessions, id)
	for _, paneID := range savedPaneIDs(rec.Session.Root) {
		_ = os.Remove(filepath.Join(snapshotsDir(), paneID+".txt"))
	}
	w.archivedDeleteID = ""
	w.flash(locales.ArchiveSessionDeleted(storedSessionTitle(rec)))
	w.scheduleSave()
	w.invalidate()
	w.invalidateSettings()
}

// idleSessionSection is the fold at the bottom of the sidebar. It is there
// only while something is idle, closed by default, and opens to the rows
// that bring their sessions back.
func (w *workspace) idleSessionSection(c *ui.Context, tk tokens) {
	if len(w.idleSessions) == 0 {
		return
	}
	open := w.idleOpen
	row := ui.Row(c).Height(sidebarRowHeight).
		Padding(0, sidebarRowEnd, 0, sidebarRowInset).Gap(sidebarRowGap).
		Radius(sidebarRowRadius()).MarginX(2).MarginY(rowMarginY).
		Border(1, ui.Transparent)
	color := tk.MutedForeground
	if row.Hovered() {
		color = tk.Foreground
		// The fold's header is a row of the sidebar like any other, so the
		// pointer gives it the same material the session rows wear. A flat
		// fill here was the one opaque block left in a list made of glass.
		if liquid(c) {
			row = rowSkin(row, c, tk)
		} else {
			row = row.Background(tk.Active)
		}
	}
	toggle := func() {
		w.idleOpen = !w.idleOpen
		w.invalidate()
	}
	row.OnClick(toggle).Children(func() {
		chevron := "chevron-right"
		if open {
			chevron = "chevron-down"
		}
		if src := iconSource(chevron, colorHex(color)); src != nil {
			ui.Image(c, src).Width(12).Height(12).OnClick(toggle)
		}
		if src := iconSource("folder", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(13).Height(13).IgnoreInput(true).OnClick(toggle)
		}
		ui.Text(c, locales.SessionIdle()).FontSize(sidebarRowFont).FontWeight(600).SingleLine().
			TextColor(color).OnClick(toggle)
		ui.Spacer(c).OnClick(toggle)
		ui.Text(c, strconv.Itoa(len(w.idleSessions))).FontSize(10.5).
			TextColor(tk.MutedForeground).OnClick(toggle)
	})
	// The rows open to their full height and close again, so the list
	// makes room for them smoothly. The header owns the height's animation:
	// it is the part drawn in both states.
	// A row is its height, the gap under it, and the room its margins take:
	// the same margins the session rows keep, so the fold's rows line up with
	// the list above them.
	rowStride := rowHeightPx + 2*rowMarginY + 2
	rowsH := float32(len(w.idleSessions))*rowStride - 2
	target := float32(0)
	if open {
		target = rowsH
	}
	rowsH = row.Animate("idleFoldHeight", target, sidebarMotion)
	if rowsH < 1 {
		return
	}
	fold := ui.Column(c).Height(rowsH).Clip().Gap(2)
	fold.Children(func() {
		for _, rec := range w.idleSessions {
			rec := rec
			w.idleSessionRow(c, tk, rec)
		}
	})
	// While the rows grow in below the list's bottom edge, the list
	// follows them, so opening the fold shows what it opened instead of
	// growing past the window.
	if open && rowsH < target-0.5 {
		fold.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollEnd})
	}
}

// idleSessionRow is one waiting session: its name, the project it belongs
// to, and the menu that starts it again. A click alone does nothing, so a
// stray click on the fold cannot start work by accident; Restore in the
// menu is the only way back.
func (w *workspace) idleSessionRow(c *ui.Context, tk tokens, rec storedSession) {
	// The hairline is reserved here too, so a row of the fold is exactly as
	// wide as a session row above it.
	row := ui.Row(c).Height(sidebarRowHeight).
		PaddingStart(sidebarRowInset + sidebarRowStep).PaddingEnd(sidebarRowEnd).Gap(sidebarRowGap).
		Radius(sidebarRowRadius()).MarginX(2).MarginY(rowMarginY).
		Border(1, ui.Transparent)
	color := tk.MutedForeground
	if row.Hovered() {
		color = tk.Foreground
		if liquid(c) {
			row = rowSkin(row, c, tk)
		} else {
			row = row.Background(tk.Foreground.Alpha(0.045))
		}
	}
	pick := func() { w.restoreIdleSession(rec.Session.ID) }
	row.Tooltip(storedSessionDetail(rec)).ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.SessionRestore()).Chosen() {
			pick()
		}
		m.Separator()
		if m.Item(locales.SessionArchive()).Chosen() {
			w.archiveIdleSession(rec.Session.ID)
		}
	}).Children(func() {
		if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(sidebarRowMark).Height(sidebarRowMark)
		}
		ui.Text(c, storedSessionTitle(rec)).FontSize(sidebarRowFont).FontWeight(500).SingleLine().
			TextColor(color)
		ui.Spacer(c)
	})
}

// archiveSessionsPage is Settings › Archive: the sessions put away from the
// sidebar, with the two things that can happen to them.
func (w *workspace) archiveSessionsPage(c *ui.Context, tk tokens) {
	sectionLabel(c, tk, locales.SettingsArchiveSessions())
	if len(w.archivedSessions) == 0 {
		settingsCard(c, tk, func() {
			ui.Row(c).Padding(10, 16, 10, 16).Children(func() {
				ui.Text(c, locales.SettingsArchiveEmpty()).FontSize(12).
					TextColor(tk.MutedForeground)
			})
		})
		return
	}
	settingsCard(c, tk, func() {
		for i, rec := range w.archivedSessions {
			if i > 0 {
				cardDivider(c, tk)
			}
			rec := rec
			ui.Row(c).Padding(10, 16, 10, 16).Gap(16).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, storedSessionTitle(rec)).FontSize(13).
						TextColor(tk.Foreground)
					ui.Text(c, storedSessionDetail(rec)).FontSize(11).
						TextColor(tk.MutedForeground)
				})
				ui.Spacer(c)
				outlineButton(c, tk, locales.SessionRestore(), func() {
					w.restoreArchivedSession(rec.Session.ID)
				})
				outlineButton(c, tk, locales.ArchiveDelete(), func() {
					w.confirmDeleteArchivedSession(rec.Session.ID)
				})
			})
		}
	})
}

// storedSessionTitle is the name the row showed when the session was idle
// or archived, kept even after its project is gone.
func storedSessionTitle(rec storedSession) string {
	if rec.Title != "" {
		return rec.Title
	}
	if rec.Session.Label != "" {
		return rec.Session.Label
	}
	if dir := savedSessionDir(rec.Session.Root); dir != "" {
		return filepath.Base(dir)
	}
	return locales.Terminal()
}

func storedSessionProject(rec storedSession) string {
	if rec.ProjectLabel != "" {
		return rec.ProjectLabel
	}
	if rec.ProjectDir != "" {
		return filepath.Base(filepath.Clean(rec.ProjectDir))
	}
	return ""
}

func storedSessionDetail(rec storedSession) string {
	count := len(savedPaneIDs(rec.Session.Root))
	detail := locales.SessionPaneCountOne()
	if count != 1 {
		detail = locales.SessionPaneCount(strconv.Itoa(count))
	}
	if project := storedSessionProject(rec); project != "" {
		return project + " · " + detail
	}
	return detail
}

// savedSessionDir is where a saved session starts: its first pane's checkout,
// or the folder the pane was working in.
func savedSessionDir(n savedNode) string {
	if n.Pane != nil {
		if n.Pane.Worktree != nil && n.Pane.Worktree.Root != "" {
			return n.Pane.Worktree.Root
		}
		return n.Pane.WorkingDirectory
	}
	if n.First != nil {
		if dir := savedSessionDir(*n.First); dir != "" {
			return dir
		}
	}
	if n.Second != nil {
		return savedSessionDir(*n.Second)
	}
	return ""
}

// savedPaneIDs names every pane in a saved layout, so the snapshots written
// for them can be dropped when the session is deleted.
func savedPaneIDs(n savedNode) []string {
	if n.Pane != nil {
		return []string{n.Pane.ID}
	}
	var ids []string
	if n.First != nil {
		ids = append(ids, savedPaneIDs(*n.First)...)
	}
	if n.Second != nil {
		ids = append(ids, savedPaneIDs(*n.Second)...)
	}
	return ids
}

func findStoredSession(list []storedSession, id string) (storedSession, bool) {
	for _, rec := range list {
		if rec.Session.ID == id {
			return rec, true
		}
	}
	return storedSession{}, false
}

func removeStoredSession(list []storedSession, id string) []storedSession {
	for i, rec := range list {
		if rec.Session.ID == id {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}
