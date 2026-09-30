package store

import (
	"context"
	"errors"
	"testing"
)

func TestAnAccountChoosesAndClearsItsAvatar(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	if _, err := st.CreateUser(ctx, "u1", "chris", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Avatar(ctx, "u1"); err != nil || got != "" {
		t.Fatalf("a new account's avatar = %q, %v; want none", got, err)
	}
	if err := st.SetAvatar(ctx, "u1", "owl"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Avatar(ctx, "u1"); got != "owl" {
		t.Errorf("avatar = %q, want owl", got)
	}
	if err := st.SetAvatar(ctx, "u1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Avatar(ctx, "u1"); got != "" {
		t.Errorf("a cleared avatar = %q, want none", got)
	}
}

// The column is read straight back to every client, so only the drawn set
// may be stored — never a URL or a string somebody typed.
func TestAnUnknownAvatarIsRefused(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	if _, err := st.CreateUser(ctx, "u1", "chris", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"dragon", "https://example.com/me.png", "OWL"} {
		if err := st.SetAvatar(ctx, "u1", bad); err == nil {
			t.Errorf("SetAvatar(%q) succeeded", bad)
		}
	}
	if err := st.SetAvatar(ctx, "nobody", "fox"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown account = %v, want ErrNotFound", err)
	}
}
