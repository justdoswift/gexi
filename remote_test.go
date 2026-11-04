package main

import (
	"strings"
	"testing"
)

func TestRemoteCodeRoundTrip(t *testing.T) {
	in := remoteCode{
		Version: 1,
		Name:    "company",
		Relay:   "peng@relay.example.com",
		Port:    22345,
		User:    "peng",
		Key:     "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
	}
	out, err := parseRemoteCode(in.encode())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out != in {
		t.Fatalf("round trip lost data: %+v", out)
	}
}

func TestParseRemoteCodeRejectsJunk(t *testing.T) {
	for _, code := range []string{"", "hello", "gexi1.!!!!", remoteCode{Version: 2}.encode()} {
		if _, err := parseRemoteCode(code); err == nil {
			t.Fatalf("%q should not parse", code)
		}
	}
}

func TestRelayPortIsStableAndInRange(t *testing.T) {
	port := relayPort("company")
	if port != relayPort("company") {
		t.Fatal("relayPort is not stable for one name")
	}
	if port < 22000 || port >= 24000 {
		t.Fatalf("relayPort(%q) = %d out of range", "company", port)
	}
	if relayPort("company") == relayPort("home") {
		t.Fatal("different names should not collide in this sample")
	}
}

func TestStripAuthorizedKeyKeepsOuterLines(t *testing.T) {
	text := strings.Join([]string{
		"ssh-ed25519 AAAA someone@else",
		authorizedBegin,
		"ssh-ed25519 BBBB gexi-remote",
		authorizedEnd,
		"",
	}, "\n")
	got := stripAuthorizedKey(text)
	if strings.Contains(got, "gexi-remote") || strings.Contains(got, authorizedBegin) {
		t.Fatalf("block survived: %q", got)
	}
	if !strings.Contains(got, "someone@else") {
		t.Fatalf("foreign key was removed: %q", got)
	}
}

func TestSafeName(t *testing.T) {
	if got := safeName("Peng's MacBook Pro"); got != "Peng-s-MacBook-Pro" {
		t.Fatalf("safeName = %q", got)
	}
	if got := safeName(""); got != "mac" {
		t.Fatalf("safeName empty = %q", got)
	}
}

func TestRemoteSSHArgs(t *testing.T) {
	h := remoteHost{Name: "company", Relay: "peng@relay", Port: 22345, User: "peng", Key: "/tmp/key"}
	args := strings.Join(remoteSSHArgs(h, true, "abcd1234"), " ")
	for _, want := range []string{"-i /tmp/key", "-p 22345", "-J peng@relay", "peng@127.0.0.1", "tmux new -A -s gexi-abcd1234"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if got := strings.Join(remoteSSHArgs(h, false, "x"), " "); strings.Contains(got, "tmux") {
		t.Fatalf("tmux off still ran tmux: %q", got)
	}
}
