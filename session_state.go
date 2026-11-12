package main

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A session has three places to be. Active sessions run in the sidebar under
// their project. Idle sessions wait in a fold at the bottom of the sidebar:
// stopped, but one click away. Archived sessions are kept in Settings ›
// Archive, out of the everyday list until they are restored or deleted. Both
// idle and archived sessions keep their panes' commands and last screens, so
// opening one resumes the same work.

// askCloseSession is what the sidebar's close button asks: archive the
// session in Settings, set it idle at the bottom of the sidebar, or cancel.
// Closing a session used to throw it away; now it always has somewhere to go.
func (w *workspace) askCloseSession(s *session) {
	if s == nil {
		return
	}
	// A session on another Mac is its host's to close; this window has no
	// saved tree to keep.
	if s.remote != nil {
		w.closeSession(s)
		return
	}
	w.sessionCloseTarget = s
	w.sessionCloseOpen = true
	w.invalidate()
}

// sessionCloseDialog asks where the closing session goes.
func (w *workspace) sessionCloseDialog(c *ui.Context, tk tokens) {
	s := w.sessionCloseTarget
	if s == nil {
		return
	}
	dialogAlert(c, tk, &w.sessionCloseOpen, 440,
		locales.SessionCloseTitle(), locales.SessionCloseBody(),
		dialogAction{label: locales.Cancel(), kind: dialogQuiet, run: func() {
			w.sessionCloseOpen = false
		}},
		dialogAction{label: locales.SessionArchive(), kind: dialogQuiet, run: func() {
			w.sessionCloseOpen = false
			if w.sessionByID(s.ID) == s {
				w.putSessionAway(s, true)
			}
		}},
		dialogAction{label: locales.SessionIdle(), kind: dialogPrimary, auto: true, run: func() {
			w.sessionCloseOpen = false
			if w.sessionByID(s.ID) == s {
				w.putSessionAway(s, false)
			}
		}},
	)
	if !w.sessionCloseOpen {
		w.sessionCloseTarget = nil
	}
}

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
	w.archivedDeleteID = id
	w.archivedDeleteOpen = true
	w.invalidateSettings()
}

// archivedSessionDeleteDialog is the settings window's alert for that delete.
func (w *workspace) archivedSessionDeleteDialog(c *ui.Context, tk tokens) {
	rec, ok := findStoredSession(w.archivedSessions, w.archivedDeleteID)
	if !ok {
		w.archivedDeleteOpen = false
		w.archivedDeleteID = ""
		return
	}
	dialogAlert(c, tk, &w.archivedDeleteOpen, 430,
		locales.ArchiveSessionDeleteTitle(),
		locales.ArchiveSessionDeleteBody(storedSessionTitle(rec)),
		dialogAction{label: locales.Cancel(), kind: dialogQuiet, auto: true, run: func() {
			w.archivedDeleteOpen = false
		}},
		dialogAction{label: locales.ArchiveDelete(), kind: dialogDanger, run: func() {
			w.archivedDeleteOpen = false
			w.deleteArchivedSession(rec.Session.ID)
		}},
	)
	if !w.archivedDeleteOpen {
		w.archivedDeleteID = ""
	}
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
	row := ui.Row(c).Height(groupRowHeight).Padding(0, 8, 0, 8).Gap(5).
		Radius(chromeRadius).MarginX(2)
	color := tk.MutedForeground
	if row.Hovered() {
		color = tk.Foreground
		row = row.Background(tk.Active)
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
		ui.Text(c, locales.SessionIdle()).FontSize(11.5).FontWeight(600).SingleLine().
			TextColor(color).OnClick(toggle)
		ui.Spacer(c).OnClick(toggle)
		ui.Text(c, strconv.Itoa(len(w.idleSessions))).FontSize(10.5).
			TextColor(tk.MutedForeground).OnClick(toggle)
	})
	if !open {
		return
	}
	for _, rec := range w.idleSessions {
		rec := rec
		w.idleSessionRow(c, tk, rec)
	}
}

// idleSessionRow is one waiting session: its name, the project it belongs
// to, and a click that starts it again.
func (w *workspace) idleSessionRow(c *ui.Context, tk tokens, rec storedSession) {
	row := ui.Row(c).Height(rowHeightPx).PaddingStart(8 + 16).PaddingEnd(8).Gap(8).
		Radius(chromeRadius).MarginX(2)
	color := tk.MutedForeground
	if row.Hovered() {
		color = tk.Foreground
		row = row.Background(tk.Foreground.Alpha(0.045))
	}
	pick := func() { w.restoreIdleSession(rec.Session.ID) }
	row.OnClick(pick).Tooltip(storedSessionDetail(rec)).ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.SessionRestore()).Chosen() {
			pick()
		}
		m.Separator()
		if m.Item(locales.SessionArchive()).Chosen() {
			w.archiveIdleSession(rec.Session.ID)
		}
	}).Children(func() {
		if src := iconSource("terminal", colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(14).Height(14).OnClick(pick)
		}
		ui.Text(c, storedSessionTitle(rec)).FontSize(13).FontWeight(500).SingleLine().
			TextColor(color).OnClick(pick)
		ui.Spacer(c).OnClick(pick)
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
