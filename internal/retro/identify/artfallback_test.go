package identify

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"lancast/internal/retro/retrodb"
	"lancast/internal/retro/romhash"
	"lancast/internal/store"
)

/*
 * The three ROMs a real library of 80-odd games showed without art
 * (2026-10-08), each for its own reason, through the worker.
 */

// shelfArt serves only the images a thumbnail set really holds; any other
// name is a miss, as a 404 is.
type shelfArt struct {
	mu    sync.Mutex
	has   map[string]bool // decoded "<set>/<name>"
	tried []string
}

func (a *shelfArt) Download(_ context.Context, u string) (string, int, int, int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, _ := url.PathUnescape(u)
	parts := strings.Split(strings.TrimSuffix(p, ".png"), "/")
	key := parts[len(parts)-2] + "/" + parts[len(parts)-1]
	a.tried = append(a.tried, key)
	if !a.has[key] {
		return "", 0, 0, 0, errors.New("404")
	}
	return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", 10, 10, 1, nil
}

func listingOf(names ...string) func(context.Context, string) ([]string, error) {
	return func(_ context.Context, u string) ([]string, error) {
		if !strings.Contains(u, "Named_Boxarts") {
			return nil, nil
		}
		return names, nil
	}
}

func poster(t *testing.T, f *fixture, id int64) string {
	t.Helper()
	a, err := f.st.ItemArtwork(context.Background(), id)
	if err != nil || a == nil {
		return ""
	}
	return a.Poster
}

// Road Rash II: matched, but the set names its box by build code, not by the
// DAT's revision.
func TestABoxMissingUnderItsDATNameIsFoundInTheListing(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	art := &shelfArt{has: map[string]bool{"Named_Boxarts/Super Mario 64 (USA) (RR1)": true}}
	f.w.Art, f.w.Artwork = art, func() bool { return true }
	f.w.Listing = listingOf("Super Mario 64 (Japan)", "Super Mario 64 (USA) (RR1)", "Super Mario 64 (USA) (Mini)")
	id := f.add(t, "sm64.z64", "n64")
	f.run(t)
	if poster(t, f, id) == "" {
		t.Fatalf("no box art; tried %v", art.tried)
	}
	if art.tried[0] != "Named_Boxarts/Super Mario 64 (USA)" {
		t.Errorf("the exact name was not tried first: %v", art.tried)
	}
}

// Without a listing the worker is as it was: exact names only.
func TestNoListingMeansExactNamesOnly(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	art := &shelfArt{has: map[string]bool{"Named_Boxarts/Super Mario 64 (USA) (RR1)": true}}
	f.w.Art, f.w.Artwork = art, func() bool { return true }
	id := f.add(t, "sm64.z64", "n64")
	f.run(t)
	if poster(t, f, id) != "" {
		t.Errorf("art found with no listing: %v", art.tried)
	}
}

// The Binding Blade (T): a translation no DAT knows, with a box under its
// English name. It gets the box and stays unmatched — art is not identity.
func TestAnUnmatchedROMGetsArtByItsFilesName(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"Fire Emblem - The Binding Blade (T).gba": {Sums: sums("99B8B6D7", "x")}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	art := &shelfArt{has: map[string]bool{"Named_Boxarts/Fire Emblem - The Binding Blade (USA)": true}}
	f.w.Art, f.w.Artwork = art, func() bool { return true }
	f.w.Listing = listingOf(gbaBoxes...)
	id := f.add(t, "Fire Emblem - The Binding Blade (T).gba", "gba")
	f.run(t)
	it := f.item(t, id)
	if it.MatchState != "unmatched" {
		t.Errorf("match state = %q; art must not make a match", it.MatchState)
	}
	if poster(t, f, id) == "" {
		t.Errorf("no box art; tried %v", art.tried)
	}
}

func TestAnUnmatchedROMFetchesNothingWithArtworkOff(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"x (T).gba": {Sums: sums("99B8B6D7", "x")}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	art := &shelfArt{has: map[string]bool{}}
	f.w.Art, f.w.Artwork = art, func() bool { return false }
	f.w.Listing = listingOf(gbaBoxes...)
	f.add(t, "x (T).gba", "gba")
	f.run(t)
	if len(art.tried) != 0 {
		t.Errorf("fetched %v with artwork off", art.tried)
	}
}

// Fire Emblem in "nes roms": a .gba inside a zip filed under the wrong
// console. The file inside decides.
func TestAZipInTheWrongConsolesFolderIsPlacedByTheFileInside(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"m.zip": {
		Sums: sums("3CE60709", marioSHA), InnerName: "Super Mario 64 (USA).z64", Platform: "n64",
	}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "m.zip", "nes")
	f.run(t)
	it := f.item(t, id)
	if it.Platform == nil || *it.Platform != "n64" || it.MatchState != "matched" {
		t.Errorf("platform %v state %q", it.Platform, it.MatchState)
	}
}

// One already hashed under the folder's console is read again under the
// right one, so the lookup uses that console's rules — once, not every pass.
func TestAHashTakenUnderTheWrongConsoleIsTakenAgain(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"m.zip": {
		Sums: sums("3CE60709", marioSHA), InnerName: "Super Mario 64 (USA).z64", Platform: "n64",
	}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "m.zip", "nes")
	ctx := context.Background()
	if err := f.st.PutROMHash(ctx, id, store.ROMHash{CRC32: "3CE60709", SHA1: marioSHA, InnerName: "Super Mario 64 (USA).z64"}); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	if it := f.item(t, id); it.Platform == nil || *it.Platform != "n64" || it.MatchState != "matched" {
		t.Fatalf("platform %v state %q", it.Platform, it.MatchState)
	}
	if f.reads["m.zip"] != 1 {
		t.Errorf("read %d times, want once", f.reads["m.zip"])
	}
	if _, err := f.st.RequeueROMs(ctx); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	if f.reads["m.zip"] != 1 {
		t.Errorf("read again on a later pass (%d), though the console now agrees", f.reads["m.zip"])
	}
}
