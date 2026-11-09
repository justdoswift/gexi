// Command gexi-relay is the server half of gexi's self-hosted remote access.
//
// A gexi on a Mac behind NAT keeps one control connection open to the relay
// and says hello with a tunnel name and a token. When a client reaches that
// tunnel's public listener, the relay asks the Mac for one data connection,
// then forwards bytes both ways. The relay has no destination to choose: a
// tunnel always points at the gexi that claimed it, so it cannot be turned
// into an open proxy.
//
// Hosts reach the relay over TLS with a self-signed certificate they pin.
// Clients reach a tunnel on a plain or TLS listener the operator exposes,
// often behind their own HTTPS proxy.
package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	version = "0.1.0"

	// maxHeader bounds one line of the relay's control protocol. Tokens and
	// stream ids are short, and anything longer is not gexi.
	maxHeader = 512

	// helloTimeout is how long a new connection has to say what it is.
	helloTimeout = 15 * time.Second

	// dataTimeout is how long a client waits for the host's data connection
	// before the relay gives up on that request.
	dataTimeout = 10 * time.Second

	// pingEvery and pongWait keep a control connection honest: a host that
	// stops answering is marked offline, so clients fail fast instead of
	// waiting on a dead link.
	pingEvery = 20 * time.Second
	pongWait  = 3 * pingEvery
)

// config is the relay's whole state, kept in one JSON file next to its
// certificate.
type config struct {
	// HostListen is where gexi hosts dial in, over TLS.
	HostListen string `json:"hostListen"`
	// Tunnels are the public listeners, one per Mac.
	Tunnels []tunnelConfig `json:"tunnels"`
}

type tunnelConfig struct {
	// Name is what a host says hello with, and Token must match it.
	Name   string `json:"name"`
	Token  string `json:"token"`
	Listen string `json:"listen"`
	// TLSCert and TLSKey, when set, serve this tunnel's clients over TLS.
	// Left empty, the listener is plain, for an HTTPS proxy in front of it.
	TLSCert string `json:"tlsCert,omitempty"`
	TLSKey  string `json:"tlsKey,omitempty"`
}

func main() {
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("gexi-relay: ")

	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to config.json")
	force := fs.Bool("force", false, "init: replace an existing config and token")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	switch cmd {
	case "init":
		os.Exit(runInit(*cfgPath, *force))
	case "serve":
		os.Exit(runServe(*cfgPath))
	case "version":
		fmt.Println(version)
	case "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `gexi-relay %s

  gexi-relay init  [-config path] [-force]   write a config, token and certificate
  gexi-relay serve [-config path]            run the relay
  gexi-relay version

The default config path is $GEXI_RELAY_CONFIG, or config.json in the
working directory. In the container image it is /data/config.json.
`, version)
}

func defaultConfigPath() string {
	if p := os.Getenv("GEXI_RELAY_CONFIG"); p != "" {
		return p
	}
	return "config.json"
}

// MARK: init

func runInit(path string, force bool) int {
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Fprintf(os.Stderr, "config already exists: %s (use -force to replace it)\n", path)
		return 1
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", dir, err)
		return 1
	}
	cert, err := loadOrCreateCert(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "certificate: %v\n", err)
		return 1
	}
	cfg := config{
		HostListen: ":47825",
		Tunnels: []tunnelConfig{{
			Name:   "mac",
			Token:  newToken(),
			Listen: ":47824",
		}},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
		return 1
	}
	public := publicAddress()
	t := cfg.Tunnels[0]
	fmt.Printf(`gexi-relay is ready.

Config: %s

In gexi › Settings › Remote › Self-hosted relay, set:
  Address      %s
  Tunnel       %s
  Token        %s
  Fingerprint  %s

Clients then reach this Mac at:
  http://%s

To serve clients over HTTPS directly, give the tunnel a tlsCert and tlsKey,
or keep its listener on loopback and put your own HTTPS proxy in front.
`, path,
		net.JoinHostPort(public, portOf(cfg.HostListen)),
		t.Name,
		t.Token,
		fingerprint(cert),
		net.JoinHostPort(public, portOf(t.Listen)),
	)
	return 0
}

// MARK: serve

func runServe(path string) int {
	cfg, err := readConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\nRun `gexi-relay init` first.\n", err)
		return 1
	}
	cert, err := loadOrCreateCert(filepath.Dir(path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "certificate: %v\n", err)
		return 1
	}
	srv, err := newServer(cfg, cert)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := srv.start(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Printf("gexi-relay %s\n", version)
	fmt.Printf("host listener  %s\n", srv.hostAddr())
	fmt.Printf("fingerprint    %s\n", fingerprint(cert))
	for _, t := range srv.order {
		fmt.Printf("tunnel %-8s %s\n", t.cfg.Name, t.addr)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	srv.close()
	return 0
}

func readConfig(path string) (config, error) {
	var cfg config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.HostListen == "" {
		cfg.HostListen = ":47825"
	}
	if len(cfg.Tunnels) == 0 {
		return cfg, errors.New("config has no tunnels")
	}
	seen := map[string]bool{}
	for _, t := range cfg.Tunnels {
		if t.Name == "" || t.Token == "" || t.Listen == "" {
			return cfg, fmt.Errorf("tunnel %q needs a name, token and listen address", t.Name)
		}
		if seen[t.Name] {
			return cfg, fmt.Errorf("tunnel %q is listed twice", t.Name)
		}
		seen[t.Name] = true
	}
	return cfg, nil
}

// server owns the listeners and the tunnels they feed.
type server struct {
	cfg     config
	cert    tls.Certificate
	tunnels map[string]*tunnel
	order   []*tunnel

	hostLn    net.Listener
	listeners []net.Listener
	closed    chan struct{}
	once      sync.Once
	wg        sync.WaitGroup
}

func newServer(cfg config, cert tls.Certificate) (*server, error) {
	s := &server{cfg: cfg, cert: cert, tunnels: map[string]*tunnel{}, closed: make(chan struct{})}
	for _, tc := range cfg.Tunnels {
		t := &tunnel{cfg: tc, srv: s, waiting: map[uint64]chan dataConn{}}
		s.tunnels[tc.Name] = t
		s.order = append(s.order, t)
	}
	return s, nil
}

func (s *server) start() error {
	for _, t := range s.order {
		ln, err := net.Listen("tcp", t.cfg.Listen)
		if err != nil {
			s.close()
			return fmt.Errorf("listen %s for tunnel %s: %w", t.cfg.Listen, t.cfg.Name, err)
		}
		if t.cfg.TLSCert != "" {
			cert, err := tls.LoadX509KeyPair(t.cfg.TLSCert, t.cfg.TLSKey)
			if err != nil {
				_ = ln.Close()
				s.close()
				return fmt.Errorf("tunnel %s certificate: %w", t.cfg.Name, err)
			}
			ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		}
		t.ln = ln
		t.addr = ln.Addr().String()
		s.listeners = append(s.listeners, ln)
		s.wg.Add(1)
		go t.acceptLoop()
	}
	ln, err := net.Listen("tcp", s.cfg.HostListen)
	if err != nil {
		s.close()
		return fmt.Errorf("listen %s for hosts: %w", s.cfg.HostListen, err)
	}
	s.hostLn = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
	s.wg.Add(1)
	go s.acceptHosts()
	return nil
}

func (s *server) hostAddr() string { return s.hostLn.Addr().String() }

func (s *server) close() {
	s.once.Do(func() {
		close(s.closed)
		if s.hostLn != nil {
			_ = s.hostLn.Close()
		}
		for _, ln := range s.listeners {
			_ = ln.Close()
		}
		for _, t := range s.order {
			t.close()
		}
	})
	s.wg.Wait()
}

func (s *server) acceptHosts() {
	defer s.wg.Done()
	for {
		conn, err := s.hostLn.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go s.handleHost(conn)
	}
}

// handleHost reads the first line to learn what a connection is for: HELLO
// keeps it as a tunnel's control channel, DATA answers one OPEN.
func (s *server) handleHost(conn net.Conn) {
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(helloTimeout))
	line, err := readLine(br)
	if err != nil {
		_ = conn.Close()
		return
	}
	fields := strings.Fields(line)
	switch {
	case len(fields) == 3 && fields[0] == "HELLO":
		s.serveControl(conn, br, fields[1], fields[2])
	case len(fields) == 4 && fields[0] == "DATA":
		s.serveData(conn, br, fields[1], fields[2], fields[3])
	default:
		writeLine(conn, "ERR expected HELLO or DATA")
		_ = conn.Close()
	}
}

// serveControl keeps a host's control connection for as long as it answers.
// A new one replaces the old, so a host that reconnects after a network
// change is online again at once.
func (s *server) serveControl(conn net.Conn, br *bufio.Reader, name, token string) {
	t := s.tunnels[name]
	if t == nil || !tokenEqual(token, t.cfg.Token) {
		// One answer for a missing tunnel and a wrong token: a prober learns
		// nothing about which names exist.
		writeLine(conn, "ERR not authorized")
		_ = conn.Close()
		log.Printf("host hello refused from %s", hostOf(conn))
		return
	}
	if !t.attach(conn) {
		_ = conn.Close()
		return
	}
	if err := writeLine(conn, "OK"); err != nil {
		t.detach(conn)
		_ = conn.Close()
		return
	}
	log.Printf("tunnel %s online from %s", t.cfg.Name, hostOf(conn))

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := t.write("PING"); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	for {
		line, err := readLine(br)
		if err != nil {
			break
		}
		if line == "PONG" {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		}
	}
	close(done)
	t.detach(conn)
	_ = conn.Close()
	log.Printf("tunnel %s offline", t.cfg.Name)
}

// serveData joins one client to the host's answer for its OPEN. The client
// side has been waiting since the OPEN went out; the two are paired through
// the tunnel's waiting map.
func (s *server) serveData(conn net.Conn, br *bufio.Reader, name, idText, token string) {
	t := s.tunnels[name]
	if t == nil || !tokenEqual(token, t.cfg.Token) {
		writeLine(conn, "ERR not authorized")
		_ = conn.Close()
		return
	}
	var id uint64
	if _, err := fmt.Sscanf(idText, "%d", &id); err != nil {
		writeLine(conn, "ERR bad stream")
		_ = conn.Close()
		return
	}
	t.mu.Lock()
	ch := t.waiting[id]
	delete(t.waiting, id)
	t.mu.Unlock()
	if ch == nil {
		writeLine(conn, "ERR no such stream")
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	ch <- dataConn{conn: conn, r: br}
}

// MARK: tunnels

type tunnel struct {
	cfg  tunnelConfig
	srv  *server
	ln   net.Listener
	addr string

	writeMu sync.Mutex // serializes lines on the control connection
	mu      sync.Mutex // guards the fields below
	control net.Conn
	nextID  uint64
	waiting map[uint64]chan dataConn
}

// dataConn is the host's answer to one OPEN: the connection plus the reader
// that may already hold bytes the host sent right behind its DATA line.
type dataConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func (t *tunnel) close() {
	t.mu.Lock()
	if t.control != nil {
		_ = t.control.Close()
		t.control = nil
	}
	for id, ch := range t.waiting {
		close(ch)
		delete(t.waiting, id)
	}
	t.mu.Unlock()
}

// attach makes conn the tunnel's control connection and ends the one it
// replaces. It reports false when the relay is shutting down.
func (t *tunnel) attach(conn net.Conn) bool {
	select {
	case <-t.srv.closed:
		return false
	default:
	}
	t.mu.Lock()
	old := t.control
	t.control = conn
	t.mu.Unlock()
	if old != nil && old != conn {
		_ = old.Close()
	}
	return true
}

func (t *tunnel) detach(conn net.Conn) {
	t.mu.Lock()
	if t.control == conn {
		t.control = nil
	}
	t.mu.Unlock()
}

// write sends one line to the host.
func (t *tunnel) write(line string) error {
	t.mu.Lock()
	conn := t.control
	t.mu.Unlock()
	if conn == nil {
		return errOffline
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := writeLine(conn, line); err != nil {
		return err
	}
	return nil
}

var errOffline = errors.New("tunnel is offline")

func (t *tunnel) acceptLoop() {
	defer t.srv.wg.Done()
	for {
		conn, err := t.ln.Accept()
		if err != nil {
			select {
			case <-t.srv.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go t.handleClient(conn)
	}
}

// handleClient asks the host for a data connection and joins the two.
func (t *tunnel) handleClient(client net.Conn) {
	defer client.Close()

	t.mu.Lock()
	if t.control == nil {
		t.mu.Unlock()
		return
	}
	id := t.nextID
	t.nextID++
	ch := make(chan dataConn, 1)
	t.waiting[id] = ch
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.waiting, id)
		t.mu.Unlock()
	}()

	if err := t.write(fmt.Sprintf("OPEN %d", id)); err != nil {
		return
	}
	select {
	case dc, ok := <-ch:
		if !ok || dc.conn == nil {
			return
		}
		defer dc.conn.Close()
		if err := writeLine(dc.conn, "OK"); err != nil {
			return
		}
		pipe(client, dc)
	case <-time.After(dataTimeout):
		log.Printf("tunnel %s: host did not answer stream %d", t.cfg.Name, id)
	case <-t.srv.closed:
	}
}

// pipe copies a client and the host's data connection into each other until
// either side ends.
func pipe(client net.Conn, dc dataConn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(dc.conn, client)
		closeWrite(dc.conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, io.MultiReader(dc.r, dc.conn))
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
}

// MARK: helpers

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maxHeader {
		return "", errors.New("line too long")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func writeLine(w io.Writer, line string) error {
	_, err := io.WriteString(w, line+"\n")
	return err
}

func closeWrite(conn net.Conn) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = conn.Close()
}

func tokenEqual(got, want string) bool {
	if got == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func newToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func hostOf(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "47825"
	}
	return port
}

// publicAddress is the first non-loopback IPv4 this machine has, so init can
// print an address the operator can paste. A server usually has a private
// interface only; its public name is the operator's to fill in.
func publicAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
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
			return ip.String()
		}
	}
	return "127.0.0.1"
}

// MARK: certificate

// loadOrCreateCert reads the relay's self-signed certificate, making one the
// first time. Hosts pin its fingerprint, so it needs no authority behind it:
// it only has to stay the same across restarts.
func loadOrCreateCert(dir string) (tls.Certificate, error) {
	crtPath := filepath.Join(dir, "relay.crt")
	keyPath := filepath.Join(dir, "relay.key")
	if _, err := os.Stat(crtPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return tls.LoadX509KeyPair(crtPath, keyPath)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "gexi-relay"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"gexi-relay", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	crtPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(crtPath, crtPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(crtPEM, keyPEM)
}

func fingerprint(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:])
}
