package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
)

// pane is one terminal: mygo's Ghostty-backed terminal plus the bits the
// control API reports. JustCli's forkpty + libghostty-spm pair collapses
// into this one object.
type pane struct {
	ID         string
	term       *terminal.Terminal
	initialDir string
	command    []string
	exited     bool
	ws         *workspace
}

func newPane(ws *workspace, command []string, dir string) (*pane, error) {
	p := &pane{ID: newID(), initialDir: dir, command: command, ws: ws}
	if len(p.command) == 0 {
		p.command = []string{loginShell(), "-l"}
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
		p.initialDir = dir
	}

	term, err := terminal.New(terminal.Options{
		Command:    p.command,
		Dir:        dir,
		Env:        []string{"JUST_PANE=" + p.ID, "JUST_SESSION=" + ws.SessionID, "JUST_SOCKET=" + socketPath(), "JUSTCLI_HOME=" + supportDir()},
		Scrollback: 10 << 20,
		OnTitle: func(string) {
			if ws.win != nil {
				ws.win.Invalidate()
			}
		},
		OnExit: func(int) {
			p.exited = true
			if ws.win != nil {
				ws.win.Invalidate()
			}
		},
	})
	if err != nil {
		return nil, err
	}
	p.term = term
	return p, nil
}

func (p *pane) title() string {
	if t := strings.TrimSpace(p.term.Title()); t != "" {
		return t
	}
	if len(p.command) > 0 {
		return filepath.Base(p.command[0])
	}
	return "Terminal"
}

// dir prefers the directory the shell reported (OSC 7), as JustCli does.
func (p *pane) dir() string {
	if d := p.term.Dir(); d != "" {
		return d
	}
	if p.initialDir != "" {
		return p.initialDir
	}
	home, _ := os.UserHomeDir()
	return home
}

func (p *pane) send(text string) { p.term.Send([]byte(text)) }

func (p *pane) text() string { return p.term.Text() }

func (p *pane) close() { _ = p.term.Close() }
