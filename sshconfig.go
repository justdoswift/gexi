package main

import (
	"os"
	"path/filepath"
	"strings"
)

// sshHost is one concrete Host entry in ~/.ssh/config, reduced to what the
// host menu shows and what a pane runs. Everything else in the file (keys,
// proxy, forwarding) stays ssh's business: the pane calls `ssh <alias>`, so
// the client applies the same config a terminal would.
type sshHost struct {
	Alias     string
	HostName  string
	User      string
	Port      string
	ProxyJump string
}

// target is the alias spelled out, shown beside it in the menu.
func (h sshHost) target() string {
	target := h.HostName
	if target == "" {
		target = h.Alias
	}
	if h.User != "" {
		target = h.User + "@" + target
	}
	if h.Port != "" {
		target += ":" + h.Port
	}
	return target
}

// command is what a pane runs to open the host. ControlMaster puts every pane
// to the same host on one TCP connection and leaves it open for the next.
// tmux keeps the remote shell alive when the link drops, and falls back to a
// login shell on hosts without it.
func (h sshHost) command(tmux bool) []string {
	args := []string{
		"ssh", "-t",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + sshControlPath(),
		"-o", "ControlPersist=10m",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		h.Alias,
	}
	if tmux {
		// One tmux session per pane: two panes to the same host must not
		// mirror each other. The name rides in the saved command, so a restore
		// reattaches to the same session.
		args = append(args, "tmux new -A -s gexi-"+newID()[:8]+" || exec ${SHELL:-sh} -l")
	}
	return args
}

func sshControlPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "control-%C")
}

func sshConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

// loadSSHHosts lists the concrete Host aliases in ~/.ssh/config, in file
// order. Wildcard and negated patterns are skipped: they set options for
// other hosts, they are not hosts to open. Include is followed, as ssh does.
func loadSSHHosts() []sshHost {
	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")

	var hosts []sshHost
	index := map[string]int{}
	visited := map[string]bool{}

	var read func(path string, depth int)
	read = func(path string, depth int) {
		if depth > 8 {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil || visited[abs] {
			return
		}
		visited[abs] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var current []int
		for _, line := range strings.Split(string(data), "\n") {
			key, value := sshDirective(line)
			switch key {
			case "host":
				current = current[:0]
				for _, pattern := range strings.Fields(value) {
					if !concreteHost(pattern) {
						continue
					}
					i, ok := index[pattern]
					if !ok {
						i = len(hosts)
						index[pattern] = i
						hosts = append(hosts, sshHost{Alias: pattern})
					}
					current = append(current, i)
				}
			case "hostname":
				for _, i := range current {
					setOnce(&hosts[i].HostName, value)
				}
			case "user":
				for _, i := range current {
					setOnce(&hosts[i].User, value)
				}
			case "port":
				for _, i := range current {
					setOnce(&hosts[i].Port, value)
				}
			case "proxyjump":
				for _, i := range current {
					setOnce(&hosts[i].ProxyJump, value)
				}
			case "include":
				for _, include := range strings.Fields(value) {
					for _, match := range sshInclude(include, sshDir) {
						read(match, depth+1)
					}
				}
			}
		}
	}
	read(sshConfigPath(), 0)
	return hosts
}

// sshDirective splits "Key value" and "Key=value". An empty key marks a blank
// line or a comment.
func sshDirective(line string) (key, value string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), ""
	}
	key = strings.ToLower(strings.TrimSpace(line[:i]))
	value = strings.TrimSpace(line[i:])
	value = strings.TrimPrefix(value, "=")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	return key, value
}

// concreteHost reports whether the pattern names one host. Wildcards and `!`
// negations are rules about other names, not a destination.
func concreteHost(pattern string) bool {
	return pattern != "" && !strings.ContainsAny(pattern, "*?!")
}

// setOnce keeps ssh's rule that the first value found for a keyword wins.
func setOnce(dst *string, value string) {
	if *dst == "" && value != "" {
		*dst = value
	}
}

// sshInclude expands an Include argument: ~ is the home directory and a
// relative path is relative to ~/.ssh, as in ssh's own config parsing.
func sshInclude(pattern, sshDir string) []string {
	switch {
	case strings.HasPrefix(pattern, "~/"):
		home, _ := os.UserHomeDir()
		pattern = filepath.Join(home, pattern[2:])
	case !filepath.IsAbs(pattern):
		pattern = filepath.Join(sshDir, pattern)
	}
	matches, _ := filepath.Glob(pattern)
	return matches
}
