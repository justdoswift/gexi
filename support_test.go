package main

import "testing"

// A GEXI_HOME that names the pane's own app folder was inherited from the
// shell the build was started in, not chosen for this instance.
func TestInheritedHome(t *testing.T) {
	t.Setenv("GEXI_SOCKET", "/x/Gexi/control.sock")
	if !inheritedHome("/x/Gexi") {
		t.Fatal("the pane's own folder reads as chosen")
	}
	if inheritedHome("/tmp/gexi-test") {
		t.Fatal("a folder the pane does not use reads as inherited")
	}
}

// An explicit GEXI_HOME outside gexi still chooses the folder, as a test
// harness relies on.
func TestSupportDirHonorsAnExplicitHome(t *testing.T) {
	t.Setenv("GEXI_HOME", "/tmp/gexi-test")
	t.Setenv("GEXI_SOCKET", "/x/Gexi/control.sock")
	if got := supportDir(); got != "/tmp/gexi-test" {
		t.Fatalf("support dir: %q", got)
	}
}

// An inherited GEXI_HOME is the other app's; the build falls back to its own
// folder rather than sharing the installed app's state.
func TestSupportDirIgnoresAnInheritedHome(t *testing.T) {
	t.Setenv("GEXI_HOME", "/x/Gexi")
	t.Setenv("GEXI_SOCKET", "/x/Gexi/control.sock")
	if got := supportDir(); got != installedSupportDir() {
		t.Fatalf("support dir: %q", got)
	}
}
