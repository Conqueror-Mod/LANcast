//go:build windows

package autostart

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// These touch the real per-user run key, so each one puts it back — both values,
// since the two targets own one each. HKCU needs no elevation, which is the
// reason this mechanism was chosen in the first place.
func restore(t *testing.T) {
	t.Helper()
	saved := map[string]string{}
	for _, n := range Names() {
		if v, ok := readValue(n); ok {
			saved[n] = v
		}
	}
	t.Cleanup(func() {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey,
			registry.SET_VALUE|registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		for _, n := range Names() {
			if v, ok := saved[n]; ok {
				_ = k.SetStringValue(n, v)
				continue
			}
			_ = k.DeleteValue(n)
		}
	})
}

func readValue(name string) (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}

func writeValue(t *testing.T, name, v string) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, v); err != nil {
		t.Fatal(err)
	}
}

func TestEnableThenDisable(t *testing.T) {
	restore(t)

	if err := Enable(Client, "-window"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	on, err := Enabled(Client)
	if err != nil || !on {
		t.Fatalf("Enabled() = %v, %v; want true", on, err)
	}

	if err := Disable(Client); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	off, err := Enabled(Client)
	if err != nil || off {
		t.Fatalf("Enabled() after Disable = %v, %v; want false", off, err)
	}
}

// The trap this package exists to avoid: the default install location is
// "C:\Program Files\LANcast", and an unquoted path with a space in it is parsed
// as a command plus arguments. Every morning, forever, at login.
func TestCommandIsQuoted(t *testing.T) {
	restore(t)

	if err := Enable(Client, "-window"); err != nil {
		t.Fatal(err)
	}
	raw, ok := readValue(Client.value)
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
	if err := Disable(Client); err != nil {
		t.Fatalf("first Disable: %v", err)
	}
	if err := Disable(Client); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}

// Enable always rewrites, so a stale entry from a moved or reinstalled LANcast
// repairs itself rather than pointing at a path that no longer exists.
func TestEnableRewritesAStalePath(t *testing.T) {
	restore(t)
	writeValue(t, Client.value, `"C:\Nowhere\LANcast-Client.exe"`)

	if err := Enable(Client); err != nil {
		t.Fatal(err)
	}
	raw, _ := readValue(Client.value)
	if strings.Contains(raw, "Nowhere") {
		t.Errorf("stale path survived Enable: %s", raw)
	}
}

/*
 * The two targets cannot reach each other's switch.
 *
 * They used to share one value name, so whichever asked last won: turning it on
 * in the tray replaced the client's entry, and turning it off anywhere cleared
 * whatever the other had meant. Two switches, one wire, and both checkboxes read
 * the same value — so both showed "on" while describing different programs.
 */
func TestTheTwoTargetsDoNotShareASwitch(t *testing.T) {
	restore(t)

	if err := Enable(Client); err != nil {
		t.Fatal(err)
	}
	if err := Enable(Tray, "tray"); err != nil {
		t.Fatal(err)
	}

	onClient, _ := Enabled(Client)
	onTray, _ := Enabled(Tray)
	if !onClient || !onTray {
		t.Fatalf("both were enabled but read back client=%v tray=%v", onClient, onTray)
	}

	// And turning one off leaves the other alone, which is the half that used
	// to silently undo somebody else's setting.
	if err := Disable(Tray); err != nil {
		t.Fatal(err)
	}
	stillClient, _ := Enabled(Client)
	if !stillClient {
		t.Error("disabling the tray turned the client's autostart off too")
	}
}

/*
 * A legacy entry belonging to the tray is not read as the client's.
 *
 * Before the split, a tray that had been set to start at login wrote its own
 * path under the shared name. After it, the client reads that same name — and
 * reporting "the client starts at login" for an entry that plainly starts the
 * server is the exact confusion this change removes. Found on a real install as
 * a preferences file saying so beside a run key that said otherwise.
 */
func TestALegacyTrayEntryIsNotTheClientStartingAtLogin(t *testing.T) {
	restore(t)
	writeValue(t, Client.value, `"C:\Program Files\LANcast\LANcast-Server.exe" tray`)

	on, err := Enabled(Client)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Error("an entry that starts the server was read as the client " +
			"starting at login")
	}
}

// And enabling the tray clears that legacy entry, or the same program would be
// launched twice at login — a worse bug than the one being fixed.
func TestEnablingTheTrayClearsItsLegacyEntry(t *testing.T) {
	restore(t)
	writeValue(t, Client.value, `"C:\Program Files\LANcast\LANcast-Server.exe" tray`)

	if err := Enable(Tray, "tray"); err != nil {
		t.Fatal(err)
	}
	if _, ok := readValue(Client.value); ok {
		t.Error("the legacy shared entry survived, so the tray now starts twice")
	}
	on, _ := Enabled(Tray)
	if !on {
		t.Error("the tray did not end up enabled under its own name")
	}
}

// Disabling the tray clears it too, or a switch turned off still starts the
// program at login — which is what people report as the setting not working.
func TestDisablingTheTrayClearsItsLegacyEntry(t *testing.T) {
	restore(t)
	writeValue(t, Client.value, `"C:\Program Files\LANcast\LANcast-Server.exe" tray`)

	if err := Disable(Tray); err != nil {
		t.Fatal(err)
	}
	if _, ok := readValue(Client.value); ok {
		t.Error("a disabled tray still starts at login through the legacy entry")
	}
}

// A client entry under the shared name is left alone by the tray: it belongs to
// the other target and is not the tray's to delete.
func TestTheTrayDoesNotDeleteTheClientsEntry(t *testing.T) {
	restore(t)
	writeValue(t, Client.value, `"C:\Program Files\LANcast\LANcast-Client.exe"`)

	if err := Disable(Tray); err != nil {
		t.Fatal(err)
	}
	if _, ok := readValue(Client.value); !ok {
		t.Error("the tray deleted the client's autostart entry")
	}
}

// The uninstaller deletes by name, so the list has to hold every name that can
// be written. One forgotten leaves a login entry pointing at a removed
// executable — an error dialog every morning with nothing obvious to blame.
func TestNamesCoversEveryTarget(t *testing.T) {
	names := Names()
	for _, want := range []string{Client.value, Tray.value} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Names() is missing %q, which the uninstaller will leave behind", want)
		}
	}
}
