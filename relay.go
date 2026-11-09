package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// The self-hosted relay: this Mac keeps one control connection open to a
// server the user runs, and the server asks it for a data connection per
// client. Nothing here needs a public address or a router rule, and the
// relay never chooses a destination: it only reaches this Mac's own HTTP
// server on loopback.

// relaySettings is the saved half of the link: where the relay is, which
// tunnel this Mac claims, and the certificate it must present.
type relaySettings struct {
	Enabled     bool
	Address     string
	Tunnel      string
	Token       string
	Fingerprint string
}

func (s appSettings) relay() relaySettings {
	return relaySettings{
		Enabled:     s.RelayEnabled,
		Address:     strings.TrimSpace(s.RelayAddress),
		Tunnel:      strings.TrimSpace(s.RelayTunnel),
		Token:       strings.TrimSpace(s.RelayToken),
		Fingerprint: normalizeFingerprint(s.RelayFingerprint),
	}
}

// ready reports whether the link has everything it needs to dial.
func (r relaySettings) ready() bool {
	return r.Address != "" && r.Tunnel != "" && r.Token != "" && r.Fingerprint != ""
}

const (
	relayOffline    = "offline"
	relayConnecting = "connecting"
	relayOnline     = "online"
)

// relayLink is one run of the link. A new one starts whenever the settings
// change, so the old connection never answers for the new config.
type relayLink struct {
	ws   *workspace
	cfg  relaySettings
	stop chan struct{}
	once sync.Once

	mu      sync.Mutex
	conn    net.Conn
	streams int

	// local is where this Mac's own HTTP server listens. Empty means the
	// one gexi always uses; tests point it at a stand-in.
	local string
}

func (w *workspace) startRelay() {
	w.stopRelay()
	cfg := w.settings.relay()
	if !cfg.Enabled {
		w.setRelayStatus(relayOffline, "")
		return
	}
	if !cfg.ready() {
		w.setRelayStatus(relayOffline, locales.SettingsRelayIncomplete())
		return
	}
	link := &relayLink{ws: w, cfg: cfg, stop: make(chan struct{})}
	w.relay = link
	w.setRelayStatus(relayConnecting, "")
	go link.run()
}

func (w *workspace) stopRelay() {
	if w.relay == nil {
		return
	}
	w.relay.close()
	w.relay = nil
}

func (w *workspace) restartRelay() {
	w.syncRemoteServer()
	w.startRelay()
	w.invalidateSettings()
}

// setRelayStatus moves the settings page's status line, on the main thread.
func (w *workspace) setRelayStatus(state, detail string) {
	w.remoteOnMain(func() {
		w.relayState = state
		w.relayDetail = detail
		w.invalidateSettings()
	})
}

func (l *relayLink) close() {
	l.once.Do(func() {
		close(l.stop)
		l.mu.Lock()
		if l.conn != nil {
			_ = l.conn.Close()
			l.conn = nil
		}
		l.mu.Unlock()
	})
}

func (l *relayLink) stopped() bool {
	select {
	case <-l.stop:
		return true
	default:
		return false
	}
}

// run keeps a control connection up: it dials, waits for the relay to ask
// for data connections, and starts over when the link drops.
func (l *relayLink) run() {
	backoff := time.Second
	for !l.stopped() {
		conn, br, err := l.connect()
		if err != nil {
			l.ws.setRelayStatus(relayConnecting, err.Error())
			if !sleepOrStop(l.stop, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		l.ws.setRelayStatus(relayOnline, "")
		l.readControl(conn, br)
		l.mu.Lock()
		if l.conn == conn {
			l.conn = nil
		}
		l.mu.Unlock()
		if l.stopped() {
			return
		}
		l.ws.setRelayStatus(relayConnecting, locales.SettingsRelayReconnecting())
		if !sleepOrStop(l.stop, time.Second) {
			return
		}
	}
}

func (l *relayLink) connect() (net.Conn, *bufio.Reader, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", l.cfg.Address, relayTLSConfig(l.cfg.Fingerprint))
	if err != nil {
		return nil, nil, relayDialError(err)
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintf(conn, "HELLO %s %s\n", l.cfg.Tunnel, l.cfg.Token); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	br := bufio.NewReader(conn)
	line, err := readRelayLine(br)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if line != "OK" {
		_ = conn.Close()
		return nil, nil, errors.New(strings.TrimPrefix(line, "ERR "))
	}
	_ = conn.SetDeadline(time.Time{})
	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()
	return conn, br, nil
}

// readControl answers the relay's pings and opens a data connection for
// every stream it asks for.
func (l *relayLink) readControl(conn net.Conn, br *bufio.Reader) {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		line, err := readRelayLine(br)
		if err != nil {
			_ = conn.Close()
			return
		}
		switch {
		case line == "PING":
			if _, err := io.WriteString(conn, "PONG\n"); err != nil {
				_ = conn.Close()
				return
			}
		case strings.HasPrefix(line, "OPEN "):
			id := strings.TrimSpace(strings.TrimPrefix(line, "OPEN "))
			if id == "" {
				continue
			}
			go l.openData(id)
		}
	}
}

// openData answers one OPEN: a fresh TLS connection to the relay, the DATA
// line with the stream id, then this Mac's own HTTP server.
func (l *relayLink) openData(id string) {
	if l.stopped() {
		return
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", l.cfg.Address, relayTLSConfig(l.cfg.Fingerprint))
	if err != nil {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintf(conn, "DATA %s %s %s\n", l.cfg.Tunnel, id, l.cfg.Token); err != nil {
		_ = conn.Close()
		return
	}
	br := bufio.NewReader(conn)
	line, err := readRelayLine(br)
	if err != nil || line != "OK" {
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})

	local, err := net.DialTimeout("tcp", l.localAddr(), 10*time.Second)
	if err != nil {
		_ = conn.Close()
		return
	}
	pipeRelay(local, conn, br)
}

// localAddr is the loopback address of this Mac's own HTTP server.
func (l *relayLink) localAddr() string {
	if l.local != "" {
		return l.local
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(remotePort))
}

// pipeRelay joins the relay's connection and the local HTTP server until
// either side ends.
func pipeRelay(local, remote net.Conn, remoteR *bufio.Reader) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		closeRelayWrite(remote)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, io.MultiReader(remoteR, remote))
		closeRelayWrite(local)
		done <- struct{}{}
	}()
	<-done
	_ = local.Close()
	_ = remote.Close()
}

func closeRelayWrite(conn net.Conn) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = conn.Close()
}

// relayTLSConfig pins the relay's self-signed certificate to the fingerprint
// saved with the settings. A relay that presents another certificate is not
// the one the user set up, and the link refuses to speak to it.
func relayTLSConfig(fingerprint string) *tls.Config {
	want := normalizeFingerprint(fingerprint)
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // the fingerprint below is the check
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("relay sent no certificate")
			}
			sum := sha256.Sum256(raw[0])
			got := hex.EncodeToString(sum[:])
			if want == "" || !strings.EqualFold(got, want) {
				return fmt.Errorf("relay certificate changed: got %s", got)
			}
			return nil
		},
	}
}

// relayDialError turns a transport failure into something the settings page
// can act on.
func relayDialError(err error) error {
	if errors.Is(err, io.EOF) {
		return errors.New(locales.SettingsRelayClosed())
	}
	return err
}

// normalizeFingerprint accepts a hash pasted with colons or spaces, in any
// case, and keeps the 64 hex characters.
func normalizeFingerprint(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func readRelayLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > 512 {
		return "", errors.New("relay sent an overlong line")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func sleepOrStop(stop <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-timer.C:
		return true
	}
}

// MARK: settings and the dialog

// setRelayEnabled turns the link on or off. Main thread.
func (w *workspace) setRelayEnabled(on bool) {
	w.settings.RelayEnabled = on
	if on && !codeFresh(w.settings.RemoteCodeAt) {
		w.regenerateRemoteCodeLocked()
	}
	w.settings.save()
	w.restartRelay()
	w.invalidate()
}

// openRelayDialog fills the dialog from the saved settings.
func (w *workspace) openRelayDialog() {
	w.relayOpen = true
	w.relayErr = ""
	w.relayAddr = w.settings.RelayAddress
	w.relayTunnel = w.settings.RelayTunnel
	w.relayURL = w.settings.RelayURL
	w.relayToken = w.settings.RelayToken
	w.relayPrint = w.settings.RelayFingerprint
	w.invalidate()
}

// submitRelay checks the dialog's fields, saves them, and starts the link
// over with the new values.
func (w *workspace) submitRelay() {
	address := normalizeRelayAddress(w.relayAddr)
	tunnel := strings.TrimSpace(w.relayTunnel)
	token := strings.TrimSpace(w.relayToken)
	print := normalizeFingerprint(w.relayPrint)
	switch {
	case address == "":
		w.relayErr = locales.RelayAddressMissing()
		w.invalidate()
		return
	case tunnel == "":
		w.relayErr = locales.RelayTunnelMissing()
		w.invalidate()
		return
	case token == "":
		w.relayErr = locales.RelayTokenMissing()
		w.invalidate()
		return
	case len(print) != 64:
		w.relayErr = locales.RelayFingerprintMissing()
		w.invalidate()
		return
	}
	w.settings.RelayAddress = address
	w.settings.RelayTunnel = tunnel
	w.settings.RelayToken = token
	w.settings.RelayFingerprint = print
	w.settings.RelayURL = strings.TrimSpace(w.relayURL)
	w.settings.RelayEnabled = true
	w.settings.save()
	w.relayOpen = false
	w.restartRelay()
	w.invalidate()
}

// normalizeRelayAddress takes what the user pasted and makes it host:port.
// A bare host gets the relay's default port, and a scheme, which belongs to
// the client address rather than the link, is dropped.
func normalizeRelayAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if raw == "" {
		return ""
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		if host == "" || port == "" {
			return ""
		}
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(raw, ":") && strings.Count(raw, ":") > 1 {
		// An IPv6 literal without a port.
		return net.JoinHostPort(raw, strconv.Itoa(relayDefaultPort))
	}
	if strings.Contains(raw, "/") {
		raw = strings.SplitN(raw, "/", 2)[0]
	}
	return net.JoinHostPort(raw, strconv.Itoa(relayDefaultPort))
}

// relayDefaultPort is the port gexi-relay's init writes for hosts.
const relayDefaultPort = 47825

// MARK: the settings page

// relayStatus names the link's state for the settings row, with the colour
// it reads in.
func (w *workspace) relayStatus(tk tokens) (string, ui.Color) {
	switch w.relayState {
	case relayOnline:
		return locales.SettingsRelayOnline(), tk.Working
	case relayConnecting:
		return locales.SettingsRelayConnecting(), tk.Waiting
	default:
		return locales.SettingsRelayOffline(), tk.MutedForeground
	}
}

func (w *workspace) relayDialog(c *ui.Context, tk tokens) {
	dialogFrame(c, tk, &w.relayOpen, 520, func() {
		dialogHeading(c, tk, locales.RelayDialogTitle(), locales.RelayDialogBody())
		dialogBody(c, func() {
			field := func(label, placeholder string, value *string) {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					dialogLabel(c, tk, label)
					dialogInput(c, tk, value, placeholder)
				})
			}
			field(locales.SettingsRelayAddressLabel(), locales.RelayAddressPlaceholder(), &w.relayAddr)
			field(locales.SettingsRelayTunnelLabel(), locales.RelayTunnelPlaceholder(), &w.relayTunnel)
			field(locales.SettingsRelayURLLabel(), locales.RelayURLPlaceholder(), &w.relayURL)
			field(locales.RelayTokenLabel(), locales.RelayTokenPlaceholder(), &w.relayToken)
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				dialogLabel(c, tk, locales.RelayFingerprintLabel())
				dialogInput(c, tk, &w.relayPrint, locales.RelayFingerprintPlaceholder()).
					OnSubmit(func() { w.submitRelay() })
			})
			dialogError(c, tk, w.relayErr)
		})
		dialogFooter(c, tk,
			dialogAction{label: locales.Cancel(), kind: dialogQuiet, run: func() {
				w.relayOpen = false
				w.invalidate()
			}},
			dialogAction{label: locales.RelaySave(), kind: dialogPrimary, run: w.submitRelay},
		)
	})
}
