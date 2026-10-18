package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

/*
 * Home's "Rate what you've watched" shelf. What is on it: films finished
 * lately and shows through an episode finished lately, unrated, newest
 * first. What is not: anything rated, anything finished too long ago,
 * anything only started, and anything in a library this account cannot see.
 */
func TestWhatIsOfferedForRating(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	// A member, because hidden libraries never apply to an administrator.
	const kid = "u_kid"
	if _, err := st.CreateUser(ctx, kid, "kid", "x", RoleMember); err != nil {
		t.Fatal(err)
	}
	films, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tv, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	add := func(lib *Library, kind, title string, parent *int64) int64 {
		t.Helper()
		id, err := st.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: filepath.Join(lib.Path, title),
			Kind: kind, Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if parent != nil {
			if err := st.SetParent(ctx, id, parent); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	finish := func(id int64, ago time.Duration) {
		t.Helper()
		if err := st.SaveProgress(ctx, id, kid, 0, true); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `UPDATE playback_state SET updated_at = ? WHERE item_id = ?`,
			time.Now().Add(-ago).Unix(), id); err != nil {
			t.Fatal(err)
		}
	}

	finished := add(films, "movie", "Finished", nil)
	rated := add(films, "movie", "Rated", nil)
	old := add(films, "movie", "Long Ago", nil)
	started := add(films, "movie", "Started", nil)
	show := add(tv, "show", "A Show", nil)
	season := add(tv, "season", "Season 1", &show)
	ep1 := add(tv, "episode", "Episode 1", &season)
	ep2 := add(tv, "episode", "Episode 2", &season)

	finish(finished, 2*time.Hour)
	finish(rated, time.Hour)
	if err := st.SetRating(ctx, rated, kid, 8, ""); err != nil {
		t.Fatal(err)
	}
	finish(old, 40*24*time.Hour)
	if err := st.SaveProgress(ctx, started, kid, 60_000, false); err != nil {
		t.Fatal(err)
	}
	finish(ep1, 3*time.Hour)
	finish(ep2, time.Hour)

	got := toRateTitles(t, st, kid)
	// The show once, by its latest episode, ahead of the film finished earlier.
	if len(got) != 2 || got[0] != "A Show" || got[1] != "Finished" {
		t.Fatalf("to rate = %v, want [A Show Finished]", got)
	}

	// Rating the show takes it off.
	if err := st.SetRating(ctx, show, kid, 7, ""); err != nil {
		t.Fatal(err)
	}
	if got := toRateTitles(t, st, kid); len(got) != 1 || got[0] != "Finished" {
		t.Errorf("after rating the show = %v", got)
	}

	// A library this account cannot see is not offered from.
	if err := st.SetHiddenLibraries(ctx, kid, []int64{films.ID}); err != nil {
		t.Fatal(err)
	}
	if got := toRateTitles(t, st, kid); len(got) != 0 {
		t.Errorf("with Films hidden = %v, want nothing", got)
	}
}

func toRateTitles(t *testing.T, st *Store, user string) []string {
	t.Helper()
	items, err := st.ToRate(context.Background(), user, 20)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}
