package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A sidebar row's context menu renames or closes it. A session's name typed
// by hand is kept on the session ahead of the branch, agent, or terminal
// title it would otherwise show, so an agent renaming its own branch cannot
// overwrite the name.

func (w *workspace) sessionRowMenu(m *ui.Menu, s *session) {
	// A session on another Mac is named by its host, which has no rename
	// command for this window to call.
	if s.remote == nil {
		if m.Item(locales.Rename()).Chosen() {
			w.openSessionRename(s)
		}
		m.Separator()
	}
	if m.Item(locales.CloseSession()).Danger().Chosen() {
		w.closeSession(s)
	}
}

func (w *workspace) projectRowMenu(m *ui.Menu, pr *project) {
	// A paired Mac is forgotten rather than named here: its name belongs to
	// the host.
	if pr.remote != nil {
		if m.Item(locales.SettingsRemoteRemove()).Danger().Chosen() {
			w.closeProject(pr)
		}
		return
	}
	if m.Item(locales.Rename()).Chosen() {
		w.openProjectRename(pr)
	}
	m.Separator()
	if m.Item(locales.CloseProject()).Danger().Chosen() {
		w.closeProject(pr)
	}
}

func (w *workspace) openSessionRename(s *session) {
	if s == nil || s.remote != nil {
		return
	}
	w.sidebarRenameSession = s
	w.sidebarRenameProject = nil
	w.sidebarRenameText = sessionLabel(s)
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = true
	w.sidebarRenameOpen = true
	w.invalidate()
}

func (w *workspace) openProjectRename(pr *project) {
	if pr == nil || pr.remote != nil {
		return
	}
	w.sidebarRenameSession = nil
	w.sidebarRenameProject = pr
	w.sidebarRenameText = pr.name()
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = true
	w.sidebarRenameOpen = true
	w.invalidate()
}

func (w *workspace) cancelSidebarRename() {
	w.sidebarRenameOpen = false
	w.sidebarRenameSession = nil
	w.sidebarRenameProject = nil
	w.sidebarRenameText = ""
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = false
	w.invalidate()
}

func (w *workspace) submitSidebarRename() {
	name := strings.TrimSpace(w.sidebarRenameText)
	if name == "" {
		w.sidebarRenameErr = locales.RenameEmpty()
		w.invalidate()
		return
	}
	switch {
	case w.sidebarRenameSession != nil:
		if s := w.sidebarRenameSession; w.sessionByID(s.ID) == s {
			s.Label = name
		}
	case w.sidebarRenameProject != nil:
		if pr := w.sidebarRenameProject; w.projectByID(pr.ID) == pr {
			pr.Label = name
		}
	}
	w.sidebarRenameOpen = false
	w.sidebarRenameSession = nil
	w.sidebarRenameProject = nil
	w.sidebarRenameText = ""
	w.sidebarRenameErr = ""
	w.sidebarRenameSelect = false
	w.invalidate()
	w.scheduleSave()
}

func (w *workspace) sidebarRenameDialog(c *ui.Context, tk tokens) {
	title := locales.RenameSessionTitle()
	placeholder := locales.RenameSessionPlaceholder()
	hint := locales.RenameSessionHint()
	if w.sidebarRenameProject != nil {
		title = locales.RenameProjectTitle()
		placeholder = locales.RenameProjectPlaceholder()
		hint = ""
	}
	ui.Column(c).Width(380).Padding(20).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(tk.Foreground)
		if hint != "" {
			ui.Text(c, hint).FontSize(11).TextColor(tk.MutedForeground)
		}
		input := ui.TextInput(c, &w.sidebarRenameText).Placeholder(placeholder).
			Label(placeholder).Height(28).FontSize(12).AutoFocus()
		if w.sidebarRenameSelect {
			input.SetTextSelection(0, utf8.RuneCountInString(w.sidebarRenameText))
			w.sidebarRenameSelect = false
		}
		input.OnSubmit(func() { w.submitSidebarRename() })
		if w.sidebarRenameErr != "" {
			ui.Text(c, w.sidebarRenameErr).FontSize(11).TextColor(tk.Waiting)
		}
		ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			ui.Button(c, locales.Cancel()).OnClick(func() { w.cancelSidebarRename() })
			ui.Button(c, locales.RenameConfirm()).OnClick(func() { w.submitSidebarRename() })
		})
	})
}
