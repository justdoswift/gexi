package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testRelayCert makes the certificate a stand-in relay presents.
func testRelayCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "gexi-relay-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("key der: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	sum := sha256.Sum256(der)
	return cert, hex.EncodeToString(sum[:])
}

// fakeRelay speaks the relay's side of the link: it greets the host, asks for
// one stream, joins the data connection, and then behaves like a client that
// speaks HTTP at the far end.
type fakeRelay struct {
	ln     net.Listener
	result chan string
	errs   chan error
}

func newFakeRelay(t *testing.T, cert tls.Certificate) *fakeRelay {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r := &fakeRelay{ln: ln, result: make(chan string, 1), errs: make(chan error, 4)}
	t.Cleanup(func() { _ = ln.Close() })
	go r.serve()
	return r
}

func (r *fakeRelay) addr() string { return r.ln.Addr().String() }

func (r *fakeRelay) serve() {
	control, err := r.ln.Accept()
	if err != nil {
		return
	}
	defer control.Close()
	cr := bufio.NewReader(control)
	line, err := cr.ReadString('\n')
	if err != nil {
		r.fail(err)
		return
	}
	if !strings.HasPrefix(line, "HELLO mac ") {
		r.fail(fmt.Errorf("hello = %q", strings.TrimSpace(line)))
		return
	}
	if _, err := io.WriteString(control, "OK\nPING\n"); err != nil {
		r.fail(err)
		return
	}
	pong, err := cr.ReadString('\n')
	if err != nil {
		r.fail(err)
		return
	}
	if strings.TrimSpace(pong) != "PONG" {
		r.fail(fmt.Errorf("pong = %q", strings.TrimSpace(pong)))
		return
	}
	if _, err := io.WriteString(control, "OPEN 7\n"); err != nil {
		r.fail(err)
		return
	}

	data, err := r.ln.Accept()
	if err != nil {
		return
	}
	defer data.Close()
	dr := bufio.NewReader(data)
	line, err = dr.ReadString('\n')
	if err != nil {
		r.fail(err)
		return
	}
	if !strings.HasPrefix(line, "DATA mac 7 ") {
		r.fail(fmt.Errorf("data = %q", strings.TrimSpace(line)))
		return
	}
	if _, err := io.WriteString(data, "OK\n"); err != nil {
		r.fail(err)
		return
	}
	if _, err := io.WriteString(data, "GET / HTTP/1.1\r\nHost: gexi\r\nConnection: close\r\n\r\n"); err != nil {
		r.fail(err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(dr, 1<<16))
	if err != nil {
		r.fail(err)
		return
	}
	r.result <- string(body)
}

func (r *fakeRelay) fail(err error) { r.errs <- err }

func TestRelayLinkForwardsToLocalServer(t *testing.T) {
	cert, fingerprint := testRelayCert(t)
	relay := newFakeRelay(t, cert)

	local := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(rw, "hello from gexi")
	}))
	defer local.Close()

	link := &relayLink{
		ws: &workspace{},
		cfg: relaySettings{
			Enabled:     true,
			Address:     relay.addr(),
			Tunnel:      "mac",
			Token:       "secret",
			Fingerprint: fingerprint,
		},
		stop:  make(chan struct{}),
		local: strings.TrimPrefix(local.URL, "http://"),
	}
	go link.run()
	defer link.close()

	select {
	case body := <-relay.result:
		if !strings.Contains(body, "hello from gexi") {
			t.Fatalf("response = %q", body)
		}
	case err := <-relay.errs:
		t.Fatalf("relay: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the host link")
	}
}

// TestRelayLinkPinsCertificate: a relay that presents another certificate is
// refused, even though it is the address the user saved.
func TestRelayLinkPinsCertificate(t *testing.T) {
	cert, _ := testRelayCert(t)
	relay := newFakeRelay(t, cert)

	link := &relayLink{
		ws:   &workspace{},
		cfg:  relaySettings{Address: relay.addr(), Tunnel: "mac", Token: "secret", Fingerprint: strings.Repeat("0", 64)},
		stop: make(chan struct{}),
	}
	if _, _, err := link.connect(); err == nil {
		t.Fatal("connected to a relay whose certificate does not match")
	}
}

func TestNormalizeRelayAddress(t *testing.T) {
	cases := map[string]string{
		"relay.example.com":          "relay.example.com:47825",
		"relay.example.com:9999":     "relay.example.com:9999",
		"https://relay.example.com":  "relay.example.com:47825",
		"http://relay.example.com/x": "relay.example.com:47825",
		"":                           "",
	}
	for in, want := range cases {
		if got := normalizeRelayAddress(in); got != want {
			t.Errorf("normalizeRelayAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	in := "AB:CD:EF:01 23 45"
	if got := normalizeFingerprint(in); got != "abcdef012345" {
		t.Fatalf("normalizeFingerprint(%q) = %q", in, got)
	}
}
