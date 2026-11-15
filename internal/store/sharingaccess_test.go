package store

import (
	"context"
	"testing"
)

// SharedActivity applies the viewer's limits, not the sharer's (ADR 0084,
// R06-1), and inside the query: the limit must count only what the viewer
// may see, or a hidden title takes a slot and the list comes back short.
func TestSharedActivityIsFilteredForTheViewer(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	sharer := f.admin
	if err := f.st.SetShareActivity(ctx, sharer.ID, true); err != nil {
		t.Fatal(err)
	}
	// The open film first, the private one last, so the private film is the
	// newest row and the first a limit of one would take.
	for _, id := range []int64{f.openFilm, f.privateFilm} {
		if err := f.st.SaveProgress(ctx, id, sharer.ID, 0, true); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(viewer string, limit int) []int64 {
		t.Helper()
		got, err := f.st.SharedActivity(ctx, sharer.ID, viewer, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, e := range got {
			out = append(out, e.Item.ID)
		}
		return out
	}

	if got := ids(f.other.ID, 20); len(got) != 2 {
		t.Fatalf("control: an unrestricted viewer sees %v, want both films", got)
	}
	if got := ids(f.member.ID, 20); len(got) != 1 || got[0] != f.openFilm {
		t.Errorf("a viewer the library is hidden from sees %v, want only the open film %d", got, f.openFilm)
	}
	if got := ids(f.member.ID, 1); len(got) != 1 || got[0] != f.openFilm {
		t.Errorf("at limit 1 the viewer sees %v: the hidden title took the only slot", got)
	}
}
