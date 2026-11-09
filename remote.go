package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Remote access, on the local network: another gexi, or a phone's browser,
// reaches this Mac with a pairing code and then talks the same JSON protocol
// the control socket speaks. There is no relay, no ssh, and no public port:
// both ends sit on the same Wi-Fi, and only a paired device gets in.

const remotePort = 47823

//go:embed web/remote
var remoteWeb embed.FS

// remoteDevice is one client allowed to reach this Mac. The token stays with
// the client; this keeps only its hash.
type remoteDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"`
	PairedAt  string `json:"pairedAt"`
}

// remoteHost is another Mac this one reaches. The token is the one it gave us
// when we paired with it.
type remoteHost struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Token   string `json:"token"`
	// LAN are the addresses the Mac had on its own networks when we paired;
	// the link tries them before the relay, and drops them when it moves.
	LAN []string `json:"lan,omitempty"`
	// Secure dials the host over TLS, as a self-hosted relay or any other
	// https:// address does.
	Secure bool `json:"secure,omitempty"`
}

// remoteMethods is what a paired device may call: read a session and type
// into a pane, make or close one, and the pane actions its own window shows.
// It may read the projects so its sidebar can draw the host's sessions where
// they belong, but it may not change them, nor the layout beyond splitting
// and closing a pane, and the pane's checkout and its pull request, which is
// what the pane's own header offers.
var remoteMethods = map[string]bool{
	"ping":           true,
	"host.info":      true,
	"workspace.info": true,
	"project.list":   true,
	"session.list":   true,
	"session.new":    true,
	"session.close":  true,
	"pane.list":      true,
	"pane.text":      true,
	"pane.stream":    true,
	"pane.send":      true,
	"pane.split":     true,
	"pane.close":     true,
	"pane.focus":     true,
	"pane.worktree":  true,
}

// remoteBackend is what the HTTP layer needs from the app. The workspace
// implements it by marshalling state reads onto the main thread; tests
// implement it without a window.
type remoteBackend interface {
	remoteEnabled() bool
	remoteCode() string
	remoteDevices() []remoteDevice
	remoteAddDevice(remoteDevice)
	remoteDispatch(controlRequest) controlResponse
	remoteName() string
}

type remoteHandler struct {
	backend remoteBackend
	limiter *pairLimiter
	mux     *http.ServeMux
}

func newRemoteHandler(b remoteBackend) *remoteHandler {
	sub, err := fs.Sub(remoteWeb, "web/remote")
	if err != nil {
		panic(err)
	}
	h := &remoteHandler{backend: b, limiter: newPairLimiter()}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pair", h.pair)
	mux.HandleFunc("/api/rpc", h.rpc)
	mux.Handle("/", noStore(http.FileServer(http.FS(sub))))
	h.mux = mux
	return h
}

func (h *remoteHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Referrer-Policy", "no-referrer")
	rw.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
	h.mux.ServeHTTP(rw, r)
}

// pair takes the code the host shows and turns it into a long-lived token.
func (h *remoteHandler) pair(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(rw, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
		return
	}
	if !h.backend.remoteEnabled() {
		writeJSON(rw, http.StatusForbidden, map[string]any{"error": "remote access is off"})
		return
	}
	ip := clientIP(r)
	if !h.limiter.allow(ip) {
		writeJSON(rw, http.StatusTooManyRequests, map[string]any{"error": "too many tries; wait a minute"})
		return
	}
	var body struct {
		Code   string `json:"code"`
		Device string `json:"device"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 4<<10)).Decode(&body); err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	want := h.backend.remoteCode()
	if want == "" || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(body.Code)), []byte(want)) != 1 {
		h.limiter.fail(ip)
		writeJSON(rw, http.StatusUnauthorized, map[string]any{"error": "wrong pairing code"})
		return
	}
	h.limiter.reset(ip)
	token := newRemoteToken()
	h.backend.remoteAddDevice(remoteDevice{
		ID:        newID(),
		Name:      deviceLabel(body.Device),
		TokenHash: hashRemoteToken(token),
		PairedAt:  time.Now().Format(time.RFC3339),
	})
	// The Mac's LAN addresses come back with the token, so a client that
	// paired through the relay can try the local network first next time.
	writeJSON(rw, http.StatusOK, map[string]any{
		"token":     token,
		"host":      h.backend.remoteName(),
		"addresses": lanAddresses(),
	})
}

// rpc is the control protocol over HTTP: one request, one response.
func (h *remoteHandler) rpc(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(rw, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
		return
	}
	token := bearerToken(r)
	if token == "" || !h.authorized(token) {
		writeJSON(rw, http.StatusUnauthorized, map[string]any{"error": "not paired"})
		return
	}
	var req controlRequest
	if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	if !remoteMethods[req.Method] {
		writeJSON(rw, http.StatusForbidden, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(rw, http.StatusOK, h.backend.remoteDispatch(req))
}

func (h *remoteHandler) authorized(token string) bool {
	sum := sha256.Sum256([]byte(token))
	got := hex.EncodeToString(sum[:])
	for _, d := range h.backend.remoteDevices() {
		if subtle.ConstantTimeCompare([]byte(got), []byte(d.TokenHash)) == 1 {
			return true
		}
	}
	return false
}

// pairLimiter keeps a 6-digit code from being guessed: five failures a
// minute from one address, then it waits.
type pairLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
	// all counts every refusal in the window, whatever address it came
	// from: one host walking a six-digit space must not be stoppable only
	// by the per-address cap, which a spread of addresses gets around.
	all []time.Time
}

const (
	pairWindow = time.Minute
	// pairMaxFails is one address's share of wrong codes a minute;
	// pairGlobalFails is the whole server's, so a distributed guess runs
	// into a wall too.
	pairMaxFails    = 5
	pairGlobalFails = 30
)

func newPairLimiter() *pairLimiter {
	return &pairLimiter{fails: map[string][]time.Time{}}
}

func (l *pairLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-pairWindow)
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.fails[ip] = kept
	all := l.all[:0]
	for _, t := range l.all {
		if t.After(cutoff) {
			all = append(all, t)
		}
	}
	l.all = all
	return len(kept) < pairMaxFails && len(all) < pairGlobalFails
}

func (l *pairLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.fails[ip] = append(l.fails[ip], now)
	l.all = append(l.all, now)
}

func (l *pairLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

func newRemoteToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func hashRemoteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// pairingCodeTTL is how long the six digits stay good. A code is meant to be
// read off one screen and typed on another; one that has been in a settings
// file for a week is a password, and a weak one.
const pairingCodeTTL = 10 * time.Minute

// codeFresh reports whether a pairing code made at rfc is still good.
func codeFresh(rfc string) bool {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return false
	}
	return time.Since(t) < pairingCodeTTL
}

// newPairingCode is the six digits the settings page shows.
func newPairingCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1_000_000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// remotePairedLabel is the short time a paired device is listed with.
func remotePairedLabel(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// writeClipboard puts text on the system clipboard through pbcopy, so a
// settings row can copy an address or a code without a UI context.
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

// deviceLabel is what the devices list calls a client: short, and always
// something.
func deviceLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "device"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(rw, r)
	})
}

// deviceName is the short host name other Macs show.
func deviceName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "mac"
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

// lanAddresses lists where this Mac can be reached, as ip:port, the primary
// interface first.
func lanAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var primary, others []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			entry := net.JoinHostPort(ip.String(), fmt.Sprint(remotePort))
			if iface.Name == "en0" || iface.Name == "en1" {
				primary = append(primary, entry)
			} else {
				others = append(others, entry)
			}
		}
	}
	return append(primary, others...)
}

// startRemoteServer opens the LAN port when remote access is on. It is
// started after the window exists, and stopped when the app quits.
func (w *workspace) startRemoteServer() {
	w.stopRemoteServer()
	w.remoteErr = ""
	if !w.remoteServerNeeded() {
		return
	}
	// IPv4, which is what the LAN addresses shown in Settings are: a plain
	// "tcp" listen on macOS is IPv6-only, and the addresses it prints would
	// not answer.
	ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", remotePort))
	if err != nil {
		w.remoteErr = err.Error()
		return
	}
	srv := &http.Server{
		Handler:           newRemoteHandler(w),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	w.remoteSrv = srv
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			w.remoteOnMain(func() {
				w.remoteErr = err.Error()
				w.invalidateSettings()
			})
		}
	}()
}

func (w *workspace) stopRemoteServer() {
	if w.remoteSrv == nil {
		return
	}
	_ = w.remoteSrv.Close()
	w.remoteSrv = nil
}

// setRemoteEnabled turns the LAN server on or off. Main thread.
func (w *workspace) setRemoteEnabled(on bool) {
	w.settings.RemoteEnabled = on
	if on && !codeFresh(w.settings.RemoteCodeAt) {
		w.regenerateRemoteCodeLocked()
	}
	w.settings.save()
	// The relay may still need the server: turning LAN access off must not
	// take it away from a link that is up.
	w.syncRemoteServer()
	w.invalidate()
	w.invalidateSettings()
}

// remoteServerNeeded reports whether anything is asking for this Mac's HTTP
// server: the local network, or a self-hosted relay.
func (w *workspace) remoteServerNeeded() bool {
	return w.settings.RemoteEnabled || w.settings.RelayEnabled
}

// syncRemoteServer starts or stops the server to match the settings.
func (w *workspace) syncRemoteServer() {
	if w.remoteServerNeeded() {
		w.startRemoteServer()
	} else {
		w.stopRemoteServer()
	}
}

func (w *workspace) regenerateRemoteCode() {
	w.regenerateRemoteCodeLocked()
	w.invalidateSettings()
}

// regenerateRemoteCodeLocked makes a new code with its clock and saves both.
func (w *workspace) regenerateRemoteCodeLocked() {
	w.settings.RemoteCode = newPairingCode()
	w.settings.RemoteCodeAt = time.Now().Format(time.RFC3339)
	w.settings.save()
}

func (w *workspace) removeRemoteDevice(id string) {
	kept := w.settings.RemoteDevices[:0]
	for _, d := range w.settings.RemoteDevices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	w.settings.RemoteDevices = kept
	w.settings.save()
	w.invalidateSettings()
}

func (w *workspace) addRemoteHost(host remoteHost) {
	for i := range w.settings.RemoteHosts {
		if w.settings.RemoteHosts[i].Address == host.Address {
			w.settings.RemoteHosts[i] = host
			w.settings.save()
			// A new pairing replaces the token the old link carries, so the
			// poll loop starts over with it.
			w.dropRemoteConn(host.Address)
			w.remoteConn(host)
			w.syncRemoteProjects()
			return
		}
	}
	w.settings.RemoteHosts = append(w.settings.RemoteHosts, host)
	w.settings.save()
	w.remoteConn(host)
	w.syncRemoteProjects()
}

// dropRemoteConn ends and forgets the link to a host. Main thread.
func (w *workspace) dropRemoteConn(address string) {
	if rc := w.remoteConns[address]; rc != nil {
		rc.close()
		delete(w.remoteConns, address)
	}
	w.syncRemoteProjects()
}

// remoteOnMain runs fn on the main thread when there is a window, and
// directly before one exists.
func (w *workspace) remoteOnMain(fn func()) {
	if w.win == nil {
		fn()
		return
	}
	w.onMain(fn)
}

// The workspace satisfies remoteBackend. Every read of the window's own
// state comes back to the main thread, so the HTTP goroutines never touch
// the app's structs beside it.

func (w *workspace) remoteEnabled() bool {
	var on bool
	// A relay serves the same pages: a Mac reachable only through one is
	// still on, even with the LAN switch off (or hidden).
	w.remoteOnMain(func() { on = w.settings.RemoteEnabled || w.settings.RelayEnabled })
	return on
}

func (w *workspace) remoteCode() string {
	var code string
	w.remoteOnMain(func() {
		if codeFresh(w.settings.RemoteCodeAt) {
			code = w.settings.RemoteCode
		}
	})
	return code
}

func (w *workspace) remoteDevices() []remoteDevice {
	var out []remoteDevice
	w.remoteOnMain(func() {
		out = append([]remoteDevice(nil), w.settings.RemoteDevices...)
	})
	return out
}

func (w *workspace) remoteAddDevice(d remoteDevice) {
	w.remoteOnMain(func() {
		w.settings.RemoteDevices = append(w.settings.RemoteDevices, d)
		w.settings.save()
		w.invalidateSettings()
	})
}

func (w *workspace) remoteDispatch(req controlRequest) controlResponse {
	if w.win == nil {
		return controlResponse{ID: req.ID, Error: "gexi is not ready"}
	}
	return w.handle(req)
}

func (w *workspace) remoteName() string { return deviceName() }

// remoteWatchTTL is how long a pane keeps the size a paired Mac asked for
// after its last look. The client polls while it shows the pane, so the size
// lapses a moment after it looks away and the pane takes the card's again.
const remoteWatchTTL = 3 * time.Second

// watchRemoteSize gives p the size the paired Mac watching it fits: the
// other Mac's terminal then has the columns and rows of its own window, and
// its lines wrap where that window ends, at this Mac's text size. Main
// thread; pane.stream runs there.
func (p *pane) watchRemoteSize(cols, rows int) {
	if p == nil || p.term == nil || cols < 1 || rows < 1 {
		return
	}
	// The pane's view lays the terminal out at the held grid on every
	// frame, and its rounding of the card's size is what the terminal ends
	// up at. Asking again for the grid the last poll already named must
	// leave that settled size alone: resizing it back on every poll would
	// send the agent a SIGWINCH at the poll's rate, and a full-screen
	// display redraws its whole screen each time.
	changed := !p.remoteWatched() || p.remoteCols != cols || p.remoteRows != rows
	p.remoteCols, p.remoteRows = cols, rows
	p.remoteWatchAt = time.Now()
	if changed {
		if c, r := p.term.Size(); c != cols || r != rows {
			p.term.Resize(cols, rows)
		}
	}
	// One timer while the Mac is watching lets the pane take the card's size
	// again once it stops; it is set at most once a second.
	if p.ws != nil && time.Since(p.remoteWatchTick) > time.Second {
		p.remoteWatchTick = time.Now()
		time.AfterFunc(remoteWatchTTL, func() {
			p.ws.onMain(func() { p.ws.invalidate() })
		})
	}
}

// remoteWatched reports whether a paired Mac asked for this pane's size
// recently enough that the view should hold it.
func (p *pane) remoteWatched() bool {
	return p != nil && p.remoteCols > 0 && p.remoteRows > 0 &&
		time.Since(p.remoteWatchAt) < remoteWatchTTL
}

// streamViewSize is the size the pane's view is laid out at, for the stream
// to report. While a Mac watches, the view holds the grid the terminal now
// has, and its size follows from that grid; the card's last size would pair
// an old width with the new grid, and the watching Mac measures a cell from
// the two. Otherwise it is the size the last frame laid the view out at.
func (p *pane) streamViewSize(cols, rows int) (float32, float32) {
	if p.remoteWatched() {
		return remoteViewSize(cols, rows, p.cellW, p.cellH)
	}
	return p.viewW, p.viewH
}

// remoteMirrorTop is where a mirrored screen starts inside its card: 0 when
// the whole screen fits, and otherwise high enough that the lowest row with
// text, anchor, sits at the card's bottom. A full-screen program draws its
// input row there, so a card smaller than the host's screen keeps the input
// in sight instead of the top rows.
func remoteMirrorTop(availH, natH float32, rows, anchor int) float32 {
	if availH <= 0 || natH <= availH || rows < 1 {
		return 0
	}
	anchor = clampInt(anchor, 0, rows-1)
	// The view's padding, 10 above and 10 below the grid.
	cellH := (natH - 20) / float32(rows)
	top := availH - float32(anchor+1)*cellH - 10
	if full := availH - natH; top < full {
		top = full
	}
	if top > 0 {
		top = 0
	}
	return top
}

// remoteViewSize is the view's size for a pane held at cols by rows, with a
// cell cw by ch: the metrics come from the last layout at the card's size,
// and the terminal's own padding, 12 by 10 a side, is added around them.
func remoteViewSize(cols, rows int, cw, ch float32) (float32, float32) {
	if cw <= 0 {
		cw = 7.55
	}
	if ch <= 0 {
		ch = 19.2
	}
	return float32(cols)*cw + 24, float32(rows)*ch + 20
}
