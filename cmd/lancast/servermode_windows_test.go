//go:build windows

package main

import (
	"strings"
	"testing"
	"unsafe"
)

// A switch moves this machine's library into this person's folder, so a window
// showing another computer's server must never offer one, and neither may a
// window whose -data folder is neither of the two a switch moves between.
func TestSwitchBlockedOutsideThisMachinesOwnFolders(t *testing.T) {
	remote := &launcher{target: target{address: "192.0.2.10:8080"}}
	if got := remote.switchBlocked("service", false); !strings.Contains(got, "another computer") {
		t.Errorf("remote window: blocked = %q", got)
	}
	ownData := &launcher{target: target{local: true}, dataDir: `D:\somewhere`}
	if got := ownData.switchBlocked("service", false); !strings.Contains(got, "-data") {
		t.Errorf("-data window: blocked = %q", got)
	}
	local := &launcher{target: target{local: true}}
	if got := local.switchBlocked("service", false); got != "" {
		t.Errorf("this machine's own window, as a service: blocked = %q", got)
	}
}

// ShellExecuteExW reads cbSize and refuses a struct of the wrong size, so the
// layout is pinned to the documented one rather than trusted to alignment.
func TestShellExecuteInfoIsTheDocumentedSize(t *testing.T) {
	want := uintptr(112) // SHELLEXECUTEINFOW on 64-bit Windows
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = 60
	}
	if got := unsafe.Sizeof(shellExecuteInfo{}); got != want {
		t.Errorf("sizeof(shellExecuteInfo) = %d, want %d", got, want)
	}
}
