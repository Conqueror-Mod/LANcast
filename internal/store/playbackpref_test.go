package store

import (
	"context"
	"testing"
)

// An account that never chose follows the default; a choice either way sticks.
func TestPlaybackPrefsDefaultAndChoice(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "Dana", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.PlaybackPrefsFor(ctx, u.ID)
	if err != nil || p.SkipIntros != DefaultSkipIntros {
		t.Fatalf("fresh account = %+v, %v; want the default", p, err)
	}
	for _, want := range []bool{false, true} {
		if err := st.SetPlaybackPrefs(ctx, u.ID, PlaybackPrefs{SkipIntros: want}); err != nil {
			t.Fatal(err)
		}
		if p, _ := st.PlaybackPrefsFor(ctx, u.ID); p.SkipIntros != want {
			t.Errorf("after choosing %v, read %v", want, p.SkipIntros)
		}
	}
	if _, err := st.PlaybackPrefsFor(ctx, "u_nobody"); err != ErrNotFound {
		t.Errorf("unknown account err = %v, want ErrNotFound", err)
	}
}
