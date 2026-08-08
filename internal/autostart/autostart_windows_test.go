//go:build windows

package autostart

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// These touch the real per-user run key, so each one puts it back. HKCU needs no
// elevation, which is the reason this mechanism was chosen in the first place.
func restore(t *testing.T) {
	t.Helper()
	prev, hadPrev := readRaw(t)
	t.Cleanup(func() {
		if hadPrev {
			k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
			if err != nil {
				return
			}
			defer k.Close()
			_ = k.SetStringValue(Name, prev)
			return
		}
		_ = disable()
	})
}

func readRaw(t *testing.T) (string, bool) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(Name)
	if err != nil {
		return "", false
	}
	return v, true
}

func TestEnableThenDisable(t *testing.T) {
	restore(t)

	if err := Enable("-window"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	on, err := Enabled()
	if err != nil || !on {
		t.Fatalf("Enabled() = %v, %v; want true", on, err)
	}

	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	off, err := Enabled()
	if err != nil || off {
		t.Fatalf("Enabled() after Disable = %v, %v; want false", off, err)
	}
}

// The trap this package exists to avoid: the default install location is
// "C:\Program Files\LANcast", and an unquoted path with a space in it is parsed
// as a command plus arguments. Every morning, forever, at login.
func TestCommandIsQuoted(t *testing.T) {
	restore(t)

	if err := Enable("-window"); err != nil {
		t.Fatal(err)
	}
	raw, ok := readRaw(t)
	if !ok {
		t.Fatal("nothing was written")
	}
	if !strings.HasPrefix(raw, `"`) {
		t.Errorf("run command is not quoted: %s", raw)
	}
	exe, _ := os.Executable()
	if !strings.Contains(raw, exe) {
		t.Errorf("run command %q does not point at this executable %q", raw, exe)
	}
	if !strings.HasSuffix(raw, " -window") {
		t.Errorf("run command %q lost its arguments", raw)
	}
}

// Disabling something that was never enabled is what the user asked for, not an
// error — and it is the common case when the toggle is switched off twice or
// when an uninstall runs on a machine that never turned it on.
func TestDisableIsIdempotent(t *testing.T) {
	restore(t)
	if err := Disable(); err != nil {
		t.Fatalf("first Disable: %v", err)
	}
	if err := Disable(); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}

// Enable always rewrites, so a stale entry from a moved or reinstalled LANcast
// repairs itself rather than pointing at a path that no longer exists.
func TestEnableRewritesAStalePath(t *testing.T) {
	restore(t)

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue(Name, `"C:\Nowhere\LANcast-Client.exe"`); err != nil {
		t.Fatal(err)
	}
	k.Close()

	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	raw, _ := readRaw(t)
	if strings.Contains(raw, "Nowhere") {
		t.Errorf("stale path survived Enable: %s", raw)
	}
}
