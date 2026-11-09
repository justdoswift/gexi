package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// startTestRelay brings up a relay on loopback with one tunnel, and returns
// it with a TLS config that accepts its self-signed certificate.
func startTestRelay(t *testing.T) (*server, *tunnel) {
	t.Helper()
	cert, err := loadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	cfg := config{
		HostListen: "127.0.0.1:0",
		Tunnels:    []tunnelConfig{{Name: "mac", Token: "secret-token", Listen: "127.0.0.1:0"}},
	}
	srv, err := newServer(cfg, cert)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	if err := srv.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(srv.close)
	return srv, srv.tunnels["mac"]
}

func dialHost(t *testing.T, srv *server) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := tls.Dial("tcp", srv.hostAddr(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("dial host: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, bufio.NewReader(conn)
}

func TestRelayForwardsBytes(t *testing.T) {
	srv, tun := startTestRelay(t)

	control, controlR := dialHost(t, srv)
	if _, err := fmt.Fprintf(control, "HELLO mac secret-token\n"); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if line, err := readLine(controlR); err != nil || line != "OK" {
		t.Fatalf("hello answer = %q, %v", line, err)
	}

	client, err := net.Dial("tcp", tun.addr)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))

	line, err := readLine(controlR)
	if err != nil {
		t.Fatalf("read open: %v", err)
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "OPEN" {
		t.Fatalf("control line = %q, want OPEN <id>", line)
	}

	data, err := tls.Dial("tcp", srv.hostAddr(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("dial data: %v", err)
	}
	defer data.Close()
	_ = data.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(data, "DATA mac %s secret-token\n", fields[1]); err != nil {
		t.Fatalf("data hello: %v", err)
	}
	dataR := bufio.NewReader(data)
	if line, err := readLine(dataR); err != nil || line != "OK" {
		t.Fatalf("data answer = %q, %v", line, err)
	}

	// Client to host.
	if _, err := io.WriteString(client, "ping\n"); err != nil {
		t.Fatalf("client write: %v", err)
	}
	if got, err := readLine(dataR); err != nil || got != "ping" {
		t.Fatalf("host read = %q, %v", got, err)
	}

	// Host to client.
	if _, err := io.WriteString(data, "pong\n"); err != nil {
		t.Fatalf("host write: %v", err)
	}
	if got, err := readLine(bufio.NewReader(client)); err != nil || got != "pong" {
		t.Fatalf("client read = %q, %v", got, err)
	}
}

func TestRelayRefusesWrongToken(t *testing.T) {
	srv, _ := startTestRelay(t)
	conn, r := dialHost(t, srv)
	if _, err := fmt.Fprintf(conn, "HELLO mac not-the-token\n"); err != nil {
		t.Fatalf("hello: %v", err)
	}
	line, err := readLine(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if line != "ERR not authorized" {
		t.Fatalf("answer = %q", line)
	}
}

// TestRelayRefusesUnknownTunnel says the same thing for a name that is not
// configured, so a prober cannot tell the two cases apart.
func TestRelayRefusesUnknownTunnel(t *testing.T) {
	srv, _ := startTestRelay(t)
	conn, r := dialHost(t, srv)
	if _, err := fmt.Fprintf(conn, "HELLO other secret-token\n"); err != nil {
		t.Fatalf("hello: %v", err)
	}
	line, err := readLine(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if line != "ERR not authorized" {
		t.Fatalf("answer = %q", line)
	}
}

// TestRelayClosesClientWhenOffline makes an offline tunnel fail fast instead
// of leaving the client to time out.
func TestRelayClosesClientWhenOffline(t *testing.T) {
	_, tun := startTestRelay(t)
	client, err := net.Dial("tcp", tun.addr)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("offline tunnel kept the client open")
	}
}

func TestCertificateFingerprintIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if fingerprint(first) != fingerprint(second) {
		t.Fatal("a restart changed the relay's certificate")
	}
	if len(fingerprint(first)) != 64 {
		t.Fatalf("fingerprint = %q, want 64 hex characters", fingerprint(first))
	}
}
