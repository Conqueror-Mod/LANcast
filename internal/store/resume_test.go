package store

import (
	"context"
	"testing"
)

/*
 * The console's candidates, kind by kind (ADR 0118 §2): an album you are
 * halfway through is one card however many of its tracks you played, a comic
 * is on it until it is finished, and a retro game is on it once it has a save.
 */
func TestResumeCandidatesCollapseAndKinds(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	add := func(path, title, kind string) int64 {
		t.Helper()
		f := file(lib.ID, path, title)
		f.Kind = kind
		if _, err := st.UpsertItem(ctx, f); err != nil {
			t.Fatal(err)
		}
		known, _ := st.KnownFiles(ctx, lib.ID)
		return known[path].ID
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	played := func(id, at int64) {
		t.Helper()
		if err := st.SaveProgress(ctx, id, "local", 60_000, false); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE playback_state SET updated_at = ? WHERE item_id = ?`, at, id)
	}

	album := add("album", "An Album", "album")
	var tracks []int64
	for i, name := range []string{"t1.flac", "t2.flac", "t3.flac"} {
		id := add(name, name, "track")
		exec(`UPDATE media_item SET parent_id = ?, episode = ? WHERE id = ?`, album, i+1, id)
		tracks = append(tracks, id)
	}
	played(tracks[0], 1_800_000_001)
	played(tracks[1], 1_800_000_002) // the newer one is where you are

	reading := add("reading.cbz", "Reading", "comic")
	finished := add("finished.cbz", "Finished", "comic")
	if err := st.SetComicPlace(ctx, "local", reading, 4, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.SetComicPlace(ctx, "local", finished, 9, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, finished, "local", 0, true); err != nil {
		t.Fatal(err)
	}

	game := add("game.sfc", "A Game", "rom")
	exec(`INSERT INTO rom_save (user_id, item_id, slot, size_bytes, sha256, updated_at)
	      VALUES ('local', ?, 'sram', 8, 'x', 1_800_000_010)`, game)

	cards, err := st.ResumeCandidates(ctx, "local", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[string][]ResumeCard{}
	for _, c := range cards {
		byKind[c.Kind] = append(byKind[c.Kind], c)
	}

	music := byKind[ResumeMusic]
	if len(music) != 1 {
		t.Fatalf("music cards = %d, want 1 — two tracks of one album are one card", len(music))
	}
	m := music[0]
	if m.Item.ID != album || m.Track == nil || m.Track.Item.ID != tracks[1] ||
		m.Track.Number != 2 || m.Track.Of != 3 {
		t.Errorf("music card = item %d track %+v, want album %d resumed at track 2 of 3 (id %d)",
			m.Item.ID, m.Track, album, tracks[1])
	}
	if m.TouchedAt != 1_800_000_002 {
		t.Errorf("music touched_at = %d, want the newest track's time", m.TouchedAt)
	}

	comics := byKind[ResumeComic]
	if len(comics) != 1 || comics[0].Item.ID != reading || comics[0].Page == nil ||
		comics[0].Page.Page != 4 || comics[0].Page.Pages != 10 {
		t.Errorf("comic cards = %+v, want only Reading at page 4 of 10 — a finished comic is off", comics)
	}

	games := byKind[ResumeGame]
	if len(games) != 1 || games[0].Item.ID != game || games[0].TouchedAt != 1_800_000_010 {
		t.Errorf("game cards = %+v, want the game by its save's time", games)
	}
}
