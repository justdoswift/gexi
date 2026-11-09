package main

import (
	"context"
	"time"

	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// Permissions: what macOS keeps for the user to give an app, checked and
// asked for from Settings. The app is the one that runs the shells and the
// agents inside them, so a permission given here is inherited by every
// terminal and every agent in a pane.

// permRow is one permission, its last known state and why it failed.
type permRow struct {
	perm   permissionflow.Permission
	status permissionflow.Status
	err    error
	busy   bool
}

// permissionsAsk is the list the page shows, in the order it reads in:
// what seeing the screen needs, what reading protected folders needs, and
// what driving other apps' windows needs.
func permissionsAsk() []permissionflow.Permission {
	return []permissionflow.Permission{
		permissionflow.ScreenRecording,
		permissionflow.FullDiskAccess,
		permissionflow.Accessibility,
	}
}

// permissionsPage is Settings › Permissions.
func (w *workspace) permissionsPage(c *ui.Context, tk tokens) {
	w.ensurePermissionRows()

	ui.Row(c).Padding(2, 2, 4, 2).Gap(12).Children(func() {
		ui.Text(c, locales.PermissionsIntro()).FontSize(11).
			TextColor(tk.MutedForeground).Grow(1)
		outlineButton(c, tk, locales.PermissionsRecheck(), func() {
			w.refreshPermissions(true)
		})
	})

	settingsCard(c, tk, func() {
		for i, row := range w.permRows {
			if i > 0 {
				cardDivider(c, tk)
			}
			w.permissionRowView(c, tk, row)
		}
	})

	ui.Text(c, locales.PermissionsRestartHint()).FontSize(11).
		TextColor(tk.MutedForeground).Padding(2, 2, 0, 2)
}

func (w *workspace) permissionRowView(c *ui.Context, tk tokens, row *permRow) {
	status, color := permissionStatusText(tk, row)
	ui.Row(c).Padding(10, 16, 10, 16).Gap(12).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, permissionTitle(row.perm)).FontSize(13).TextColor(tk.Foreground)
			ui.Text(c, status).FontSize(11).TextColor(color)
			ui.Text(c, permissionDetail(row.perm)).FontSize(11).
				TextColor(tk.MutedForeground)
		})
		if row.busy {
			ui.Text(c, locales.PermissionsWorking()).FontSize(11).
				TextColor(tk.MutedForeground)
			return
		}
		if row.status == permissionflow.Granted {
			return
		}
		if permissionHasPrompt(row.perm) {
			p := row.perm
			outlineButton(c, tk, locales.PermissionsRequest(), func() {
				w.askPermission(p)
			})
		}
		p := row.perm
		outlineButton(c, tk, locales.PermissionsGuide(), func() {
			w.guidePermission(p)
		})
	})
}

// permissionHasPrompt reports whether macOS shows a prompt for p. Full Disk
// Access has none: it only has a list in System Settings.
func permissionHasPrompt(p permissionflow.Permission) bool {
	return p != permissionflow.FullDiskAccess
}

func permissionTitle(p permissionflow.Permission) string {
	switch p {
	case permissionflow.ScreenRecording:
		return locales.PermissionsScreenRecording()
	case permissionflow.FullDiskAccess:
		return locales.PermissionsFullDiskAccess()
	case permissionflow.Accessibility:
		return locales.PermissionsAccessibility()
	}
	return p.Title()
}

func permissionDetail(p permissionflow.Permission) string {
	switch p {
	case permissionflow.ScreenRecording:
		return locales.PermissionsScreenRecordingDetail()
	case permissionflow.FullDiskAccess:
		return locales.PermissionsFullDiskAccessDetail()
	case permissionflow.Accessibility:
		return locales.PermissionsAccessibilityDetail()
	}
	return ""
}

func permissionStatusText(tk tokens, row *permRow) (string, ui.Color) {
	if row.err != nil {
		return locales.PermissionsFailed(row.err.Error()), tk.Danger
	}
	switch row.status {
	case permissionflow.Granted:
		return locales.PermissionsGranted(), tk.Working
	case permissionflow.Denied:
		return locales.PermissionsDenied(), tk.Danger
	case permissionflow.NotDetermined:
		return locales.PermissionsNotAsked(), tk.Waiting
	default:
		return locales.PermissionsUnknown(), tk.MutedForeground
	}
}

// ensurePermissionRows starts with every permission unknown and asks the
// system once, the first time the page is drawn.
func (w *workspace) ensurePermissionRows() {
	if w.permRows == nil {
		for _, p := range permissionsAsk() {
			w.permRows = append(w.permRows, &permRow{perm: p})
		}
	}
	if !w.permChecked {
		w.permChecked = true
		w.refreshPermissions(false)
	}
}

// refreshPermissions checks every row again, off the main thread. Checking
// Full Disk Access reads a protected file, which is why it does not run on
// the thread that draws.
func (w *workspace) refreshPermissions(manual bool) {
	if w.permWorking {
		return
	}
	w.permWorking = true
	list := append([]*permRow(nil), w.permRows...)
	go func() {
		type result struct {
			status permissionflow.Status
			err    error
		}
		out := make([]result, len(list))
		for i, row := range list {
			out[i].status, out[i].err = permissionflow.Check(row.perm)
		}
		w.onMain(func() {
			for i, row := range list {
				row.status, row.err = out[i].status, out[i].err
			}
			w.permWorking = false
			if manual {
				w.permRows = list
			}
			w.invalidateSettings()
		})
	}()
}

// askPermission shows the system's prompt. It runs off the main thread,
// which Request would block while the user answers.
func (w *workspace) askPermission(p permissionflow.Permission) {
	w.setPermissionBusy(p, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		st, err := permissionflow.Request(ctx, p)
		w.finishPermission(p, st, err)
	}()
}

// guidePermission opens System Settings on the permission's page and shows
// the app to drag into its list, then takes the status it comes back with.
func (w *workspace) guidePermission(p permissionflow.Permission) {
	w.setPermissionBusy(p, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		st, err := permissionflow.Guide(ctx, p, permissionflow.GuideOptions{})
		w.finishPermission(p, st, err)
	}()
}

func (w *workspace) setPermissionBusy(p permissionflow.Permission, busy bool) {
	w.onMain(func() {
		for _, row := range w.permRows {
			if row.perm == p {
				row.busy = busy
			}
		}
		w.invalidateSettings()
	})
}

func (w *workspace) finishPermission(p permissionflow.Permission, st permissionflow.Status, err error) {
	w.onMain(func() {
		for _, row := range w.permRows {
			if row.perm != p {
				continue
			}
			row.busy = false
			// A guide that ended without an answer leaves the status it
			// had; a check that failed says why.
			if err == nil {
				row.status, row.err = st, nil
			} else if st == permissionflow.Unknown {
				row.err = err
			}
			break
		}
		w.invalidateSettings()
	})
}
