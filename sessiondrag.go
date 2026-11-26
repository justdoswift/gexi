package main

import (
	"github.com/egoist/mygo/ui"
)

// Sessions move with the pointer: a session row drags its session, and any
// other session row takes it, before or after itself as the pointer sits. The
// same gesture orders a project's work and moves a session to another
// project, and a project's header takes one first in that project.

// sessionMoved is the value a session row drags: the project it lives in and
// the session itself.
type sessionMoved struct {
	Project *project
	Session *session
}

// sessionDrop is where a dragged session goes: before the row it was let go
// on, after it, or nowhere.
type sessionDrop int

const (
	sessionDropNone sessionDrop = iota
	sessionDropBefore
	sessionDropAfter
)

// sessionDropAt reads a drop at y in a row of height h: the half above the
// middle puts the session before the row, the half below after it.
func sessionDropAt(y, h float32) sessionDrop {
	if h <= 0 {
		return sessionDropNone
	}
	if y < h/2 {
		return sessionDropBefore
	}
	return sessionDropAfter
}

// sessionDropAtRow reads where a dragged session would go over row as the
// pointer sits on it, or nowhere when the row holds that session already.
func sessionDropAtRow(row ui.Element, s *session) sessionDrop {
	v, ok := ui.DragOver[sessionMoved](row)
	if !ok || v.Session == s {
		return sessionDropNone
	}
	_, y, ok := row.PointerPosition()
	if !ok {
		return sessionDropNone
	}
	return sessionDropAt(y, row.Bounds().H)
}

// sessionDropLine draws where a dragged session would sit: over the row for a
// drop before it, under it for a drop after it. It takes no input, so the row
// under it stays the drop target.
func sessionDropLine(c *ui.Context, place sessionDrop, h float32) {
	if place == sessionDropNone {
		return
	}
	top := float32(-4)
	if place == sessionDropAfter {
		top = h + 2
	}
	ui.Box(c).Absolute().Left(4).Right(4).Top(top).Height(2).
		Background(dropAccent).IgnoreInput(true)
	ui.Box(c).Absolute().Left(0).Top(top - 3).Width(8).Height(8).Radius(4).
		Background(dropAccent).IgnoreInput(true)
}

// moveSession puts a dragged session into to's rows, before or after the
// session it was let go on; a session with no row to go on — a folded
// project's header — goes first. The session leaves the project it was in, so
// the same move orders a project's work and takes a session to another
// project. Its panes keep running where they were: only the row it lives
// under moves.
func (w *workspace) moveSession(from *project, s *session, to *project, at *session, place sessionDrop) {
	if from == nil || to == nil || s == nil || at == s {
		return
	}
	// A host's sessions are its own: they neither move nor take another's.
	if from.remote != nil || to.remote != nil || place == sessionDropNone {
		return
	}
	si := -1
	for i, have := range from.Sessions {
		if have == s {
			si = i
			break
		}
	}
	if si < 0 {
		return
	}
	// The rows the session goes among, as they are without it. One with no
	// row to go on, or on a row that is gone, goes last.
	rest := make([]*session, 0, len(to.Sessions))
	for _, have := range to.Sessions {
		if have != s {
			rest = append(rest, have)
		}
	}
	ti := len(rest)
	for i, have := range rest {
		if have == at {
			ti = i
			if place == sessionDropAfter {
				ti++
			}
			break
		}
	}
	// Let go where it sits already: no row moves, and nothing is saved.
	if from == to && ti == si {
		return
	}
	if from != to {
		out := make([]*session, 0, len(from.Sessions)-1)
		for _, have := range from.Sessions {
			if have != s {
				out = append(out, have)
			}
		}
		from.Sessions = out
	}
	order := make([]*session, 0, len(rest)+1)
	order = append(order, rest[:ti]...)
	order = append(order, s)
	order = append(order, rest[ti:]...)
	to.Sessions = order
	w.invalidate()
	w.scheduleSave()
}
