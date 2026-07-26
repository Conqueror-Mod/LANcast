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
