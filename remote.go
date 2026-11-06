package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/justdoswift/gexi/locales"
)

// Remote access, the simple half: the Mac being reached keeps one reverse
// tunnel to a relay (an ordinary ssh account the owner already has), and the
// Mac reaching it gets a single code that carries everything a connection
// needs. No ssh config, no frp, no ports to remember.

const (
	remoteCodePrefix = "gexi1."
	authorizedBegin  = "# gexi-remote begin"
	authorizedEnd    = "# gexi-remote end"
	remoteKeyComment = "gexi-remote"
)

// remoteHost is a Mac imported from a connection code and kept in settings.
type remoteHost struct {
	Name  string `json:"name"`
	Relay string `json:"relay"` // user@host of the relay
	Port  int    `json:"port"`
	User  string `json:"user"` // the account on the far Mac
	Key   string `json:"key"`  // private key file
}

// remoteCode is what travels between the two Macs, base64url inside one line.
// It carries the private key, so the code is a credential: regenerate it by
// turning remote access off and on again.
type remoteCode struct {
	Version int    `json:"v"`
	Name    string `json:"name"`
	Relay   string `json:"relay"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Key     string `json:"key"`
}

func (rc remoteCode) encode() string {
	data, _ := json.Marshal(rc)
	return remoteCodePrefix + base64.RawURLEncoding.EncodeToString(data)
}

func parseRemoteCode(s string) (remoteCode, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, remoteCodePrefix) {
		return remoteCode{}, errors.New("That is not a gexi connection code")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, remoteCodePrefix))
	if err != nil {
		return remoteCode{}, errors.New("The code is damaged; copy it again")
	}
	var rc remoteCode
	if err := json.Unmarshal(data, &rc); err != nil || rc.Version != 1 {
		return remoteCode{}, errors.New("That code comes from another version")
	}
	if rc.Relay == "" || rc.Port == 0 || rc.User == "" || rc.Key == "" {
		return remoteCode{}, errors.New("The code is incomplete; copy it again")
	}
	return rc, nil
}

func remoteDir() string { return filepath.Join(supportDir(), "remote") }

// deviceName is the short host name shown on the other Mac, with a suffix
// when the name repeats (two "macbook-pro"s on one relay).
func deviceName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "mac"
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

// relayPort maps a device name to a stable port on the relay, so the same Mac
// keeps the same code until remote access is turned off.
func relayPort(name string) int {
	return 22000 + int(crc32.ChecksumIEEE([]byte(name))%2000)
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if name := os.Getenv("USER"); name != "" {
		return name
	}
	return "user"
}

// ensureRemoteKey creates the key this Mac accepts remote logins with, and
// returns the private and public key paths.
func ensureRemoteKey() (priv, pub string, err error) {
	dir := remoteDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	priv = filepath.Join(dir, "id_ed25519")
	pub = priv + ".pub"
	if _, statErr := os.Stat(priv); statErr == nil {
		if _, pubErr := os.Stat(pub); pubErr == nil {
			return priv, pub, nil
		}
	}
	_ = os.Remove(priv)
	_ = os.Remove(pub)
	cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", remoteKeyComment, "-f", priv)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("ssh-keygen: %v %s", err, strings.TrimSpace(string(out)))
	}
	return priv, pub, nil
}

// authorizedKeysPath is where this Mac accepts logins from.
func authorizedKeysPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "authorized_keys")
}

// addAuthorizedKey appends the key inside a marked block, so turning remote
// access off can take exactly it away again.
func addAuthorizedKey(keyLine string) error {
	path := authorizedKeysPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	current, _ := os.ReadFile(path)
	body := stripAuthorizedKey(string(current))
	if !strings.HasSuffix(body, "\n") && body != "" {
		body += "\n"
	}
	body += authorizedBegin + "\n" + strings.TrimSpace(keyLine) + "\n" + authorizedEnd + "\n"
	return os.WriteFile(path, []byte(body), 0o600)
}

// stripAuthorizedKey removes a block written by an earlier enable.
func stripAuthorizedKey(text string) string {
	lines := strings.Split(text, "\n")
	var kept []string
	inside := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == authorizedBegin:
			inside = true
		case trimmed == authorizedEnd:
			inside = false
		case inside:
			// dropped
		default:
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func removeAuthorizedKey() {
	path := authorizedKeysPath()
	current, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(stripAuthorizedKey(string(current))), 0o600)
}

// enableRemote prepares this Mac to be reached: a key, an ssh tunnel to the
// relay, and a code the other Mac can take. It returns the code.
func (w *workspace) enableRemote(relay string) (string, error) {
	relay = strings.TrimSpace(relay)
	if !strings.Contains(relay, "@") || strings.HasPrefix(relay, "@") {
		return "", errors.New("Write the relay as user@host")
	}
	if !relayReachable(relay) {
		return "", fmt.Errorf("Cannot ssh to %s. Check the address or run ssh-copy-id once", relay)
	}
	priv, pub, err := ensureRemoteKey()
	if err != nil {
		return "", err
	}
	pubLine, err := os.ReadFile(pub)
	if err != nil {
		return "", err
	}
	if err := addAuthorizedKey(string(pubLine)); err != nil {
		return "", err
	}
	name := strings.TrimSpace(w.relayName)
	if name == "" {
		name = deviceName()
	}
	port := relayPort(name)
	key, err := os.ReadFile(priv)
	if err != nil {
		return "", err
	}

	w.settings.RemoteListen = true
	w.settings.RemoteRelay = relay
	w.settings.RemoteName = name
	w.settings.RemotePort = port
	w.settings.save()

	w.startRemoteTunnel()
	return remoteCode{
		Version: 1,
		Name:    name,
		Relay:   relay,
		Port:    port,
		User:    currentUser(),
		Key:     string(key),
	}.encode(), nil
}

// disableRemote undoes enableRemote: the key stops being accepted and the
// tunnel is closed.
func (w *workspace) disableRemote() {
	w.stopRemoteTunnel()
	removeAuthorizedKey()
	w.settings.RemoteListen = false
	w.tunnelUp = false
	w.settings.save()
}

// remoteCodeValue rebuilds the code from settings, reading the key again so a
// lost clipboard can be filled without turning the tunnel off.
func (w *workspace) remoteCodeValue() (string, error) {
	if !w.settings.RemoteListen {
		return "", errors.New("Remote access is off")
	}
	key, err := os.ReadFile(filepath.Join(remoteDir(), "id_ed25519"))
	if err != nil {
		return "", err
	}
	return remoteCode{
		Version: 1,
		Name:    w.settings.RemoteName,
		Relay:   w.settings.RemoteRelay,
		Port:    w.settings.RemotePort,
		User:    currentUser(),
		Key:     string(key),
	}.encode(), nil
}

// relayReachable checks the relay accepts the user's existing ssh login,
// before a tunnel is promised.
func relayReachable(relay string) bool {
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new", relay, "true")
	return cmd.Run() == nil
}

func remoteTunnelArgs(relay string, port int) []string {
	return []string{
		"ssh", "-N", "-T",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-R", "127.0.0.1:" + strconv.Itoa(port) + ":localhost:22",
		relay,
	}
}

// startRemoteTunnel keeps a reverse tunnel open for as long as remote access
// is on, restarting it when the link drops.
func (w *workspace) startRemoteTunnel() {
	if !w.settings.RemoteListen || w.settings.RemoteRelay == "" {
		return
	}
	w.stopRemoteTunnel()
	stop := make(chan struct{})
	w.tunnelStop = stop
	relay, port := w.settings.RemoteRelay, w.settings.RemotePort

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			args := remoteTunnelArgs(relay, port)
			cmd := exec.Command(args[0], args[1:]...)
			if err := cmd.Start(); err != nil {
				w.setTunnelState(false, err.Error())
				time.Sleep(10 * time.Second)
				continue
			}
			w.tunnel = cmd
			// A tunnel that stays up a few seconds is a working one; report
			// it, and wait for it to end before trying again.
			time.Sleep(2 * time.Second)
			if cmd.Process != nil {
				w.setTunnelState(true, "")
			}
			_ = cmd.Wait()
			select {
			case <-stop:
				return
			default:
			}
			w.setTunnelState(false, "reconnecting…")
			time.Sleep(5 * time.Second)
		}
	}()
}

func (w *workspace) stopRemoteTunnel() {
	if w.tunnelStop != nil {
		close(w.tunnelStop)
		w.tunnelStop = nil
	}
	if w.tunnel != nil && w.tunnel.Process != nil {
		_ = w.tunnel.Process.Kill()
		w.tunnel = nil
	}
}

func (w *workspace) setTunnelState(up bool, note string) {
	apply := func() {
		w.tunnelUp = up
		w.tunnelNote = note
		w.invalidate()
	}
	if w.win != nil {
		w.onMain(apply)
		return
	}
	apply()
}

// importRemoteCode takes a code from the other Mac and opens it.
func (w *workspace) importRemoteCode(code string) error {
	rc, err := parseRemoteCode(code)
	if err != nil {
		return err
	}
	dir := remoteDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(dir, "imported-"+safeName(rc.Name))
	if err := os.WriteFile(keyPath, []byte(rc.Key), 0o600); err != nil {
		return err
	}
	host := remoteHost{Name: rc.Name, Relay: rc.Relay, Port: rc.Port, User: rc.User, Key: keyPath}
	replaced := false
	for i := range w.settings.ImportedHosts {
		if w.settings.ImportedHosts[i].Name == host.Name {
			w.settings.ImportedHosts[i] = host
			replaced = true
		}
	}
	if !replaced {
		w.settings.ImportedHosts = append(w.settings.ImportedHosts, host)
	}
	w.settings.save()
	w.connectRemote(host)
	return nil
}

func safeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "mac"
	}
	return b.String()
}

// remoteSSHArgs is what a pane runs to reach an imported Mac: the key from the
// code, the port the tunnel published, and the relay as the jump host.
func remoteSSHArgs(h remoteHost, tmux bool, session string) []string {
	args := []string{
		"ssh", "-t",
		"-i", h.Key,
		"-p", strconv.Itoa(h.Port),
		"-J", h.Relay,
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		h.User + "@127.0.0.1",
	}
	if tmux {
		args = append(args, "tmux new -A -s gexi-"+session+" || exec ${SHELL:-sh} -l")
	}
	return args
}

// connectRemote opens a session to an imported Mac through the relay.
func (w *workspace) connectRemote(h remoteHost) {
	pr := w.remoteProject(h.Name)
	if pr == nil {
		home, _ := os.UserHomeDir()
		pr = &project{ID: newID(), Dir: home, Label: h.Name, RemoteHost: h.Name}
		w.Projects = append(w.Projects, pr)
		delete(w.collapsed, pr.ID)
	}
	_, _ = w.newSession(pr, remoteSSHArgs(h, w.settings.RemoteTmux, newID()[:8]), pr.Dir)
}

// writeClipboard and readClipboard use the system tools rather than a UI
// context, so a menu action can reach the clipboard after the menu closes.
func writeClipboard(text string) {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}

func readClipboard() string {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// openRelayDialog prefills the one-time setup from what is already known.
func (w *workspace) openRelayDialog() {
	w.relayInput = w.settings.RemoteRelay
	if w.relayInput == "" {
		w.relayInput = "you@"
	}
	w.relayName = w.settings.RemoteName
	if w.relayName == "" {
		w.relayName = deviceName()
	}
	w.relayErr = ""
	w.relayBusy = false
	w.relayOpen = true
	w.invalidate()
}

// submitRelay checks the relay off the UI thread, then enables remote access
// on it and copies the code.
func (w *workspace) submitRelay() {
	if w.relayBusy {
		return
	}
	relay := strings.TrimSpace(w.relayInput)
	name := strings.TrimSpace(w.relayName)
	if !strings.Contains(relay, "@") || strings.HasPrefix(relay, "@") || strings.HasSuffix(relay, "@") {
		w.relayErr = "Write the relay as user@host"
		w.invalidate()
		return
	}
	w.relayBusy = true
	w.relayErr = ""
	w.invalidate()

	go func() {
		if !relayReachable(relay) {
			w.onMain(func() {
				w.relayBusy = false
				w.relayErr = "Cannot reach " + relay + ". Check the address, or run ssh-copy-id once."
				w.invalidate()
			})
			return
		}
		w.onMain(func() {
			w.relayName = name
			code, err := w.enableRemote(relay)
			w.relayBusy = false
			if err != nil {
				w.relayErr = err.Error()
				w.invalidate()
				return
			}
			writeClipboard(code)
			w.relayOpen = false
			w.invalidate()
			notifyUser(locales.NotifyCodeCopiedTitle(), "", locales.NotifyCodeCopiedBody())
		})
	}()
}

// openImportDialog starts from the clipboard when it already holds a code.
func (w *workspace) openImportDialog() {
	clip := strings.TrimSpace(readClipboard())
	if strings.HasPrefix(clip, remoteCodePrefix) {
		w.importText = clip
	} else {
		w.importText = ""
	}
	w.importErr = ""
	w.importOpen = true
	w.invalidate()
}

func (w *workspace) submitImport() {
	if err := w.importRemoteCode(strings.TrimSpace(w.importText)); err != nil {
		w.importErr = err.Error()
		w.invalidate()
		return
	}
	w.importOpen = false
	w.invalidate()
}
