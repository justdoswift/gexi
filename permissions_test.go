package main

import (
	"testing"

	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/ui"
)

// TestPermissionsPageListsWhatTheAppNeeds: the page must offer the three
// permissions the app and the shells inside it depend on, with a title and
// a detail each, and must not offer a prompt System Settings does not have.
func TestPermissionsPageListsWhatTheAppNeeds(t *testing.T) {
	want := []permissionflow.Permission{
		permissionflow.ScreenRecording,
		permissionflow.FullDiskAccess,
		permissionflow.Accessibility,
	}
	got := permissionsAsk()
	if len(got) != len(want) {
		t.Fatalf("permissionsAsk() = %v, want %v", got, want)
	}
	for i, p := range want {
		if got[i] != p {
			t.Fatalf("permissionsAsk()[%d] = %q, want %q", i, got[i], p)
		}
		if permissionTitle(p) == "" || permissionDetail(p) == "" {
			t.Errorf("%s has no title or detail", p)
		}
	}
	if permissionHasPrompt(permissionflow.FullDiskAccess) {
		t.Error("Full Disk Access has no system prompt; the page offered one")
	}
	if !permissionHasPrompt(permissionflow.ScreenRecording) ||
		!permissionHasPrompt(permissionflow.Accessibility) {
		t.Error("a permission with a system prompt was treated as having none")
	}
}

// TestPermissionStatusText: every status reads as something, and a failed
// check says why instead of showing a stale answer.
func TestPermissionStatusText(t *testing.T) {
	tk := defaultTokens(false, ui.RGB(0x0a, 0x84, 0xff))
	row := &permRow{perm: permissionflow.ScreenRecording}
	for _, st := range []permissionflow.Status{
		permissionflow.Unknown,
		permissionflow.NotDetermined,
		permissionflow.Denied,
		permissionflow.Granted,
	} {
		row.status, row.err = st, nil
		if text, _ := permissionStatusText(tk, row); text == "" {
			t.Errorf("status %v has no text", st)
		}
	}
	row.err = errCheck{}
	if text, _ := permissionStatusText(tk, row); text == "" {
		t.Error("a failed check has no text")
	}
}

type errCheck struct{}

func (errCheck) Error() string { return "denied by the system" }
