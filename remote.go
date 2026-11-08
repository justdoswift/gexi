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
}

// remoteMethods is what a paired device may call: read a session and type
// into a pane. It may not change the app, the projects, or the layout.
var remoteMethods = map[string]bool{
	"ping":           true,
	"host.info":      true,
	"workspace.info": true,
	"session.list":   true,
	"pane.list":      true,
	"pane.text":      true,
	"pane.stream":    true,
	"pane.send":      true,
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
	writeJSON(rw, http.StatusOK, map[string]any{
		"token": token,
		"host":  h.backend.remoteName(),
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
}

const (
	pairWindow   = time.Minute
	pairMaxFails = 5
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
	return len(kept) < pairMaxFails
}

func (l *pairLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[ip] = append(l.fails[ip], time.Now())
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
	if !w.settings.RemoteEnabled {
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
	if on && w.settings.RemoteCode == "" {
		w.settings.RemoteCode = newPairingCode()
	}
	w.settings.save()
	if on {
		w.startRemoteServer()
	} else {
		w.stopRemoteServer()
	}
	w.invalidate()
	w.invalidateSettings()
}

func (w *workspace) regenerateRemoteCode() {
	w.settings.RemoteCode = newPairingCode()
	w.settings.save()
	w.invalidateSettings()
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
			return
		}
	}
	w.settings.RemoteHosts = append(w.settings.RemoteHosts, host)
	w.settings.save()
	w.remoteOpen[host.Address] = true
	w.remoteConn(host)
}

func (w *workspace) removeRemoteHost(address string) {
	kept := w.settings.RemoteHosts[:0]
	for _, h := range w.settings.RemoteHosts {
		if h.Address != address {
			kept = append(kept, h)
		}
	}
	w.settings.RemoteHosts = kept
	w.settings.save()
	w.dropRemoteConn(address)
	delete(w.remoteOpen, address)
	if w.remoteActive == address {
		w.remoteActive = ""
	}
	w.invalidate()
	w.invalidateSettings()
}

// dropRemoteConn ends and forgets the link to a host. Main thread.
func (w *workspace) dropRemoteConn(address string) {
	if rc := w.remoteConns[address]; rc != nil {
		rc.close()
		delete(w.remoteConns, address)
	}
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
	w.remoteOnMain(func() { on = w.settings.RemoteEnabled })
	return on
}

func (w *workspace) remoteCode() string {
	var code string
	w.remoteOnMain(func() { code = w.settings.RemoteCode })
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
