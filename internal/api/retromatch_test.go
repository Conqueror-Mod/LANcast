package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/retro/identify"
	"lancast/internal/retro/retrodb"
	"lancast/internal/store"
)

/*
 * Fix match on a game (ADR 0073). Reported from the window: searching "Fire
 * Emblem - The Binding Blade" in Fix match found nothing, because a ROM was
 * being searched for among films and TV. It could equally have offered a
 * film called Fire Emblem, and applied it to a cartridge.
 */

const gbaDAT = `
game ( name "Fire Emblem (USA, Australia)" region "USA" rom ( crc 2A524221 ) )
game ( name "Fire Emblem - Fuuin no Tsurugi (Japan)" region "Japan" rom ( crc 99999999 ) )
`

type romHarness struct {
	*harness
	stub *stubProvider
	id   int64
}

// newROMHarness: a retro library holding one unmatched GBA ROM, a ROM
// database, and a film provider standing by with a film of the same name —
// which a game must never be offered.
func newROMHarness(t *testing.T, withDB bool) *romHarness {
	t.Helper()
	h := newHarness(t)
	stub := &stubProvider{id: "stub", cands: []meta.Candidate{
		{ExternalID: "1", Kind: meta.KindMovie, Title: "Fire Emblem", Year: 2003, Popularity: 50},
	}, fetchTitle: "Fire Emblem (the film)"}
	h.reg.AddProvider(stub)

	ctx := context.Background()
	dir := t.TempDir()
	lib, err := h.st.CreateLibrary(ctx, "Games", "retro", dir)
	if err != nil {
		t.Fatal(err)
	}
	platform := "gba"
	id, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(dir, "Fire Emblem - The Binding Blade (T).zip"), Kind: "rom",
		Title: "Fire Emblem - The Binding Blade", SortTitle: "fire emblem - the binding blade",
		Platform: &platform, Container: "zip", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	var cache *retrodb.Cache
	if withDB {
		games, err := retrodb.Parse(strings.NewReader(gbaDAT))
		if err != nil {
			t.Fatal(err)
		}
		ix := &retrodb.Index{}
		ix.Build("gba", games)
		cache = retrodb.Preloaded(ix)
	} else {
		cache = retrodb.Preloaded(nil)
	}
	w := identify.NewWorker(h.st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Index = cache.Index
	h.srvAPI.retroDB, h.srvAPI.retro = cache, w
	return &romHarness{h, stub, id}
}

func (r *romHarness) candidates(t *testing.T, q string) (int, []meta.Candidate) {
	t.Helper()
	path := "/api/items/" + itoa(r.id) + "/candidates"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	resp := r.do(t, "GET", path, nil)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return resp.StatusCode, nil
	}
	var c []meta.Candidate
	decode(t, resp, &c)
	return resp.StatusCode, c
}

func TestAGameIsSearchedInTheROMDatabaseNotAmongFilms(t *testing.T) {
	r := newROMHarness(t, true)
	status, cands := r.candidates(t, "fire emblem")
	if status != 200 || len(cands) != 2 {
		t.Fatalf("status %d, candidates %+v", status, cands)
	}
	for _, c := range cands {
		if c.Provider != identify.Provider || c.Kind != "rom" {
			t.Errorf("a game was offered %s/%s %q", c.Provider, c.Kind, c.Title)
		}
	}
	if cands[0].ExternalID != "Fire Emblem (USA, Australia)" {
		t.Errorf("the exact title did not come first: %q", cands[0].ExternalID)
	}
	if !strings.Contains(cands[0].PosterURL, "Named_Boxarts") {
		t.Errorf("no box art address: %q", cands[0].PosterURL)
	}
	if r.stub.lastQuery.Title != "" {
		t.Errorf("the film provider was asked %q for a game", r.stub.lastQuery.Title)
	}
}

// With no query, the filename's title is searched: here the translation's
// English name, which no DAT lists — an honest nothing, not every Fire Emblem.
func TestAGamesDefaultSearchIsItsFilesTitle(t *testing.T) {
	r := newROMHarness(t, true)
	status, cands := r.candidates(t, "")
	if status != 200 || len(cands) != 0 {
		t.Errorf("status %d, candidates %+v", status, cands)
	}
}

func TestAGameWithNoROMDatabaseSaysSo(t *testing.T) {
	r := newROMHarness(t, false)
	wantError(t, r.do(t, "GET", "/api/items/"+itoa(r.id)+"/candidates", nil), 503, "unavailable")
	wantError(t, r.do(t, "POST", "/api/items/"+itoa(r.id)+"/match",
		map[string]any{"provider": identify.Provider, "external_id": "Fire Emblem (USA, Australia)"}), 503, "unavailable")
}

func TestChoosingADATEntryAppliesAndLocksIt(t *testing.T) {
	r := newROMHarness(t, true)
	resp := r.do(t, "POST", "/api/items/"+itoa(r.id)+"/match",
		map[string]any{"provider": identify.Provider, "external_id": "Fire Emblem - Fuuin no Tsurugi (Japan)"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var it store.Item
	decode(t, resp, &it)
	if it.MatchState != meta.StateLocked {
		t.Errorf("match state %q, want locked", it.MatchState)
	}
	if it.ExternalID == nil || *it.ExternalID != "Fire Emblem - Fuuin no Tsurugi (Japan)" ||
		it.Provider == nil || *it.Provider != identify.Provider {
		t.Errorf("identity %v %v", it.Provider, it.ExternalID)
	}
	if it.Title != "Fire Emblem - Fuuin no Tsurugi" {
		t.Errorf("title %q, want the DAT's", it.Title)
	}
}

func TestAGameCannotBeGivenAFilmOrAnUnlistedName(t *testing.T) {
	r := newROMHarness(t, true)
	wantError(t, r.do(t, "POST", "/api/items/"+itoa(r.id)+"/match",
		map[string]any{"provider": "stub", "external_id": "1"}), 400, "bad_request")
	wantError(t, r.do(t, "POST", "/api/items/"+itoa(r.id)+"/match",
		map[string]any{"provider": identify.Provider, "external_id": "Advance Wars (USA)"}), 400, "not_found")
	it, err := r.st.GetItem(context.Background(), r.id, "")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Fire Emblem - The Binding Blade" || it.MatchState == meta.StateLocked {
		t.Errorf("a refused match changed the game: %q %q", it.Title, it.MatchState)
	}
}
