package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSSHConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSSHHosts(t *testing.T) {
	writeSSHConfig(t, `
# a comment
Host devbox
    HostName 10.0.0.4
    User peng
    Port 2222

Host *.internal
    User ignored

Host prod
    HostName prod.example.com

Host devbox
    HostName second.example.com
`)
	hosts := loadSSHHosts()
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts: %+v", len(hosts), hosts)
	}
	if hosts[0].Alias != "devbox" || hosts[0].HostName != "10.0.0.4" ||
		hosts[0].User != "peng" || hosts[0].Port != "2222" {
		t.Errorf("devbox: %+v", hosts[0])
	}
	if got := hosts[0].target(); got != "peng@10.0.0.4:2222" {
		t.Errorf("target: %q", got)
	}
	if hosts[1].Alias != "prod" || hosts[1].HostName != "prod.example.com" {
		t.Errorf("prod: %+v", hosts[1])
	}
}

func TestLoadSSHHostsInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "conf.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Include conf.d/*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "conf.d", "work"), []byte("Host work\n  HostName work.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hosts := loadSSHHosts()
	if len(hosts) != 1 || hosts[0].Alias != "work" || hosts[0].HostName != "work.example.com" {
		t.Fatalf("got %+v", hosts)
	}
}

func TestSSHCommand(t *testing.T) {
	h := sshHost{Alias: "devbox"}
	argv := h.command(true)
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"ssh -t",
		"ControlMaster=auto",
		"ControlPath=",
		"ControlPersist=10m",
		"ServerAliveInterval=15",
		"devbox",
		"tmux new -A -s gexi-",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is missing from %q", want, joined)
		}
	}
	if len(h.command(false)) != len(argv)-1 {
		t.Errorf("without tmux the trailing command should be dropped")
	}
}

func TestRemoteProjectDefaults(t *testing.T) {
	w := &workspace{settings: appSettings{RemoteTmux: true}}
	pr := &project{ID: "r", Dir: t.TempDir(), Label: "devbox", RemoteHost: "devbox"}
	w.Projects = []*project{pr}

	// A new session, and a split, on a remote project open another ssh pane
	// instead of a local shell.
	if cmd := w.projectCommand(pr, nil); cmd == nil || cmd[0] != "ssh" {
		t.Errorf("remote default command: %v", cmd)
	}
	if cmd := w.projectCommand(&project{Dir: pr.Dir}, nil); cmd != nil {
		t.Errorf("a local project should keep the login shell, got %v", cmd)
	}
	// The remote project holds the home directory, so it must not swallow a
	// local project opened in the same folder.
	if got := w.projectFor(pr.Dir); got == pr {
		t.Error("projectFor returned the remote project")
	}
}
