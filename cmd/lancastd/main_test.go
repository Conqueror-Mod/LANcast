package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"lancast/internal/config"
	"lancast/internal/store"
)

// A pre-multi-user install has a password in config.json and no user rows.
// Startup must turn that into a single 'local' admin — the id every existing
// session and playback_state row already carries — and clear the legacy
// password so credentials live in one place (ADR 0015).
func TestSeedLegacyOwner(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	st, err := store.Open(filepath.Join(dir, "lancast.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	settings, err := config.LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Get()
	s.PasswordHash = "bcrypt-hash-standin"
	if err := settings.Set(s); err != nil {
		t.Fatal(err)
	}

	if err := seedLegacyOwner(st, settings, log); err != nil {
		t.Fatalf("seedLegacyOwner: %v", err)
	}

	u, err := st.UserByID(ctx, store.LocalUserID)
	if err != nil {
		t.Fatalf("expected a 'local' admin: %v", err)
	}
	if u.Role != store.RoleAdmin {
		t.Errorf("seeded role = %q, want admin", u.Role)
	}
	if u.PasswordHash != "bcrypt-hash-standin" {
		t.Errorf("seeded hash = %q, want the legacy hash carried over", u.PasswordHash)
	}
	if got := settings.Get().PasswordHash; got != "" {
		t.Errorf("legacy password still in settings: %q", got)
	}

	// Idempotent: a second run with a user already present is a no-op.
	if err := seedLegacyOwner(st, settings, log); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if n, _ := st.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers = %d after second seed, want 1", n)
	}
}

// A fresh install — no password, no users — is left untouched for setup to
// create the first admin.
func TestSeedLegacyOwnerFreshInstallNoop(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	st, err := store.Open(filepath.Join(dir, "lancast.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	settings, err := config.LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := seedLegacyOwner(st, settings, log); err != nil {
		t.Fatalf("seedLegacyOwner: %v", err)
	}
	if n, _ := st.CountUsers(context.Background()); n != 0 {
		t.Errorf("CountUsers = %d on fresh install, want 0", n)
	}
}

// An unsecured server binds loopback and nothing else, whatever was asked for.
// The API exposes filesystem browsing and library creation at arbitrary paths,
// so an open port on a passwordless server is arbitrary read access. This gate
// is not a configuration convenience and must not be relaxed.
func TestUnsecuredServerIsAlwaysForcedToLoopback(t *testing.T) {
	for _, requested := range []string{
		":8080", "0.0.0.0:8080", "192.168.1.50:8080", "[::]:8080",
		"example.local:9000", "127.0.0.1:8080", "garbage",
	} {
		addr, lanBound := bindAddr(requested, false)
		if lanBound {
			t.Errorf("bindAddr(%q, unsecured) reported lanBound — an unsecured server must never be LAN-bound", requested)
		}
		if !loopbackOnly(addr) {
			t.Errorf("bindAddr(%q, unsecured) = %q, which is not loopback-only", requested, addr)
		}
	}
}

// The bug this replaced: lanBound was true whenever the server was secured,
// regardless of the address. A server the operator explicitly bound to
// loopback then announced itself as LAN-reachable and served a self-signed
// certificate on localhost — the friction ADR 0014 exists to avoid.
func TestSecuredLoopbackBindIsNotLANBound(t *testing.T) {
	for _, requested := range []string{
		"127.0.0.1:8080", "127.0.0.1:8099", "localhost:8080",
		"LOCALHOST:8080", "[::1]:8080", "127.0.0.2:8080",
	} {
		addr, lanBound := bindAddr(requested, true)
		if addr != requested {
			t.Errorf("bindAddr(%q, secured) = %q, want the address honoured verbatim", requested, addr)
		}
		if lanBound {
			t.Errorf("bindAddr(%q, secured) reported lanBound; it reaches only this machine", requested)
		}
	}
}

// A secured server that does reach the network still gets TLS and still says
// so. This is the case the whole mechanism exists for.
func TestSecuredNetworkBindIsLANBound(t *testing.T) {
	for _, requested := range []string{
		":8080", "0.0.0.0:8080", "[::]:8080", "192.168.1.50:8080", "10.0.0.7:9000",
	} {
		addr, lanBound := bindAddr(requested, true)
		if addr != requested {
			t.Errorf("bindAddr(%q, secured) = %q, want the address honoured verbatim", requested, addr)
		}
		if !lanBound {
			t.Errorf("bindAddr(%q, secured) reported loopback-only; it is reachable from the network", requested)
		}
	}
}

func TestLoopbackOnly(t *testing.T) {
	loopback := []string{"127.0.0.1:8080", "127.0.0.5:1", "localhost:80", "[::1]:8080"}
	for _, a := range loopback {
		if !loopbackOnly(a) {
			t.Errorf("loopbackOnly(%q) = false, want true", a)
		}
	}

	// An empty host means every interface — the case most worth getting right,
	// because reading it as "local" would put credentials on the wire.
	reachable := []string{":8080", "0.0.0.0:8080", "[::]:8080", "192.168.1.50:8080", "8.8.8.8:80"}
	for _, a := range reachable {
		if loopbackOnly(a) {
			t.Errorf("loopbackOnly(%q) = true, want false", a)
		}
	}

	// Unparseable is treated as reachable: guessing "local" wrongly serves
	// plaintext credentials, guessing "reachable" wrongly costs a warning.
	for _, a := range []string{"garbage", "", "127.0.0.1"} {
		if loopbackOnly(a) {
			t.Errorf("loopbackOnly(%q) = true; an address we cannot parse must not be assumed local", a)
		}
	}
}

// restart_required promises the operator that restarting will let other
// devices connect. It must only be made when a restart would actually bind
// wider — otherwise they restart, see no change, and cannot tell whether the
// advice or their configuration is wrong.
func TestRestartWidensBind(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		lanBound  bool
		want      bool
	}{
		// Unsecured and forced onto loopback, with a configured address that
		// does reach further. This is the case the promise exists for.
		{"unsecured, wildcard configured", ":8080", false, true},
		{"unsecured, all interfaces", "0.0.0.0:8080", false, true},
		{"unsecured, LAN ip configured", "192.168.1.50:8080", false, true},

		// The bug: loopback configured deliberately. A restart changes nothing,
		// so no promise may be made.
		{"unsecured, loopback configured", "127.0.0.1:8080", false, false},
		{"unsecured, localhost configured", "localhost:8080", false, false},
		{"unsecured, ipv6 loopback configured", "[::1]:8080", false, false},

		// Already reaching the network — nothing left for a restart to widen.
		{"already LAN-bound", ":8080", true, false},
		{"already LAN-bound, LAN ip", "192.168.1.50:8080", true, false},
	}
	for _, tc := range cases {
		if got := restartWidensBind(tc.requested, tc.lanBound); got != tc.want {
			t.Errorf("%s: restartWidensBind(%q, lanBound=%v) = %v, want %v",
				tc.name, tc.requested, tc.lanBound, got, tc.want)
		}
	}
}

// The pairing that matters: a server started unsecured with a loopback address
// is loopback-bound, stays loopback-bound after a restart, and must never
// claim otherwise.
func TestLoopbackConfigMakesNoRestartPromise(t *testing.T) {
	const configured = "127.0.0.1:8099"

	addr, lanBound := bindAddr(configured, false)
	if lanBound {
		t.Fatalf("unsecured bindAddr reported lanBound")
	}
	if restartWidensBind(configured, lanBound) {
		t.Error("promised a restart would widen the bind; the configured address is loopback")
	}

	// After the restart the operator was not promised: same address, still not
	// LAN-bound, still no promise.
	addr2, lanBound2 := bindAddr(configured, true)
	if addr2 != addr || lanBound2 {
		t.Errorf("after restart: addr=%q lanBound=%v, want %q and loopback-only", addr2, lanBound2, addr)
	}
	if restartWidensBind(configured, lanBound2) {
		t.Error("still promising a widening restart after the restart happened")
	}
}
