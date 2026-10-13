package store

import (
	"context"
	"testing"
)

// An account's subtitle style is kept, read back whole, and refused when no
// player could draw it; an account that never set one has none.
func TestSubtitleStyleIsPerAccount(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	u, err := st.CreateUser(ctx, "u_viewer", "viewer", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	got, err := st.SubtitleStyleFor(ctx, u.ID)
	if err != nil || got != nil {
		t.Fatalf("a new account's style = %+v, %v; want none", got, err)
	}

	want := SubtitleStyle{Color: "#ffe066", Font: "serif", Size: 1.35, Position: 12}
	if err := st.SetSubtitleStyle(ctx, u.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err = st.SubtitleStyleFor(ctx, u.ID)
	if err != nil || got == nil || *got != want {
		t.Fatalf("read back %+v, %v; want %+v", got, err, want)
	}

	for _, bad := range []SubtitleStyle{
		{Color: "yellow", Font: "serif", Size: 1, Position: 8},
		{Color: "#ffffff", Font: "comic", Size: 1, Position: 8},
		{Color: "#ffffff", Font: "sans", Size: 9, Position: 8},
		{Color: "#ffffff", Font: "sans", Size: 1, Position: 90},
	} {
		if err := st.SetSubtitleStyle(ctx, u.ID, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	if err := st.SetSubtitleStyle(ctx, "nobody", want); err != ErrNotFound {
		t.Errorf("an unknown account answered %v, want ErrNotFound", err)
	}
}
