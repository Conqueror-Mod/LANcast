package identify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lancast/internal/retro/retrodb"
	"lancast/internal/retro/romhash"
	"lancast/internal/store"
)

const (
	marioSHA = "9BEF1128717F958171A4AFAC3ED78EE2BB4E86CE"
	zeldaCRC = "3B5C6C08"
)

// testIndex is two invented entries in the shape the real DATs give.
func testIndex(t *testing.T) *retrodb.Index {
	t.Helper()
	games, err := retrodb.Parse(strings.NewReader(`
game (
	name "Super Mario 64 (USA)"
	region "USA"
	rom ( name "Super Mario 64 (USA).z64" size 1 crc 3CE60709 sha1 ` + marioSHA + ` )
)
game (
	name "Legend of Zelda, The - Ocarina of Time (USA) (Rev 1)"
	region "USA"
	rom ( name "x.z64" size 1 crc ` + zeldaCRC + ` sha1 0000000000000000000000000000000000000001 )
)`))
	if err != nil {
		t.Fatal(err)
	}
	year, _ := retrodb.Parse(strings.NewReader(`game ( comment "Super Mario 64 (USA)" releaseyear "1996" genre "Platform" rom ( crc 3CE60709 ) )`))
	ix := &retrodb.Index{}
	ix.Build("n64", games, year)
	return ix
}

type fixture struct {
	st    *store.Store
	lib   store.Library
	w     *Worker
	reads map[string]int
	mu    sync.Mutex
}

// newFixture builds a store with a retro library, and a worker whose reader
// answers from a table keyed by file name rather than touching any file.
func newFixture(t *testing.T, results map[string]romhash.Result) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	lib, err := st.CreateLibrary(context.Background(), "Games", "retro", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{st: st, lib: *lib, reads: map[string]int{}}
	f.w = NewWorker(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.w.Read = func(path, platform string) (romhash.Result, error) {
		f.mu.Lock()
		f.reads[filepath.Base(path)]++
		f.mu.Unlock()
		r, ok := results[filepath.Base(path)]
		if !ok {
			return romhash.Result{}, errors.New("unreadable")
		}
		if r.Platform == "" {
			r.Platform = platform
		}
		return r, nil
	}
	return f
}

func (f *fixture) add(t *testing.T, name, platform string) int64 {
	t.Helper()
	var p *string
	if platform != "" {
		p = &platform
	}
	id, err := f.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: f.lib.ID, Path: filepath.Join(f.lib.Path, name), Kind: "rom",
		Title: "filename title", SortTitle: "filename title", Platform: p,
		Container: strings.TrimPrefix(filepath.Ext(name), "."), SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) item(t *testing.T, id int64) *store.Item {
	t.Helper()
	it, err := f.st.GetItem(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func (f *fixture) run(t *testing.T) {
	t.Helper()
	if err := f.w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func sums(crc, sha string) []romhash.Sums { return []romhash.Sums{{CRC32: crc, SHA1: sha}} }

func TestMatchedROMTakesTheDATsName(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "sm64.z64", "n64")
	f.run(t)

	it := f.item(t, id)
	if it.Title != "Super Mario 64" {
		t.Errorf("title = %q", it.Title)
	}
	if it.Year == nil || *it.Year != 1996 {
		t.Errorf("year = %v", it.Year)
	}
	if it.MatchState != "matched" || it.Provider == nil || *it.Provider != Provider ||
		it.ExternalID == nil || *it.ExternalID != "Super Mario 64 (USA)" {
		t.Errorf("match = %q %v %v", it.MatchState, it.Provider, it.ExternalID)
	}
	genres, _ := f.st.Genres(context.Background(), id)
	if len(genres) != 1 || genres[0] != "Platform" {
		t.Errorf("genres = %v", genres)
	}
	if s := f.w.Stats(); s.Matched != 1 || s.Remaining != 0 {
		t.Errorf("stats = %+v", s)
	}
}

// Identity comes from the bytes: a file renamed to nonsense is the same game.
func TestRenamedROMIsStillIdentified(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"asdfgh.z64": {Sums: sums("", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "asdfgh.z64", "n64")
	f.run(t)
	if got := f.item(t, id).Title; got != "Super Mario 64" {
		t.Errorf("title = %q", got)
	}
}

// The second layout — a header stripped — is looked up when the first misses.
func TestAltLayoutMatches(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"z.z64": {Sums: []romhash.Sums{
		{CRC32: "FFFFFFFF", SHA1: "nope"}, {CRC32: zeldaCRC, SHA1: "nope2"},
	}}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "z.z64", "n64")
	f.run(t)
	if got := f.item(t, id).Title; got != "Legend of Zelda, The - Ocarina of Time" {
		t.Errorf("title = %q", got)
	}
}

func TestLockedFieldsAreNotTouched(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "sm64.z64", "n64")
	ctx := context.Background()
	if err := f.st.LockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	it := f.item(t, id)
	if it.Title != "filename title" {
		t.Errorf("a locked title was overwritten: %q", it.Title)
	}
	if it.Year == nil || *it.Year != 1996 {
		t.Error("an unlocked field was not written beside a locked one")
	}
}

// A locked match is never re-scored, whatever the DAT says.
func TestLockedMatchIsNotRelitigated(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "sm64.z64", "n64")
	ctx := context.Background()
	if err := f.st.SetMatch(ctx, id, "manual", "My Mario", "locked", 1); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	it := f.item(t, id)
	if it.MatchState != "locked" || *it.ExternalID != "My Mario" || it.Title != "filename title" {
		t.Errorf("a locked match changed: %q %v %q", it.MatchState, *it.ExternalID, it.Title)
	}
}

func TestUnmatchedROMIsStampedUnmatched(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"hb.nes": {Sums: sums("12345678", "abc")}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "hb.nes", "nes")
	f.run(t)
	it := f.item(t, id)
	if it.MatchState != "unmatched" || it.Title != "filename title" {
		t.Errorf("got %q %q", it.MatchState, it.Title)
	}
	if n, _ := f.st.PendingROMCount(context.Background()); n != 0 {
		t.Error("an unmatched ROM stayed in the queue")
	}
}

// An unreadable file is stamped, not retried for ever.
func TestUnreadableROMIsStamped(t *testing.T) {
	f := newFixture(t, nil)
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	f.add(t, "broken.zip", "")
	f.run(t)
	if n, _ := f.st.PendingROMCount(context.Background()); n != 0 {
		t.Error("an unreadable ROM stayed in the queue")
	}
	if f.w.Stats().Failed != 1 {
		t.Errorf("stats = %+v", f.w.Stats())
	}
}

// Before the DATs are installed a ROM is hashed and stamped; installing them
// re-queues it, and the second pass identifies it without reading the file
// again.
func TestInstallingDATsIdentifiesWithoutRereading(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	id := f.add(t, "sm64.z64", "n64")
	f.run(t)
	if it := f.item(t, id); it.Title != "filename title" || it.MatchState == "matched" {
		t.Fatalf("matched with no index: %+v", it)
	}

	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	if _, err := f.st.RequeueROMs(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	if got := f.item(t, id).Title; got != "Super Mario 64" {
		t.Errorf("title = %q after install", got)
	}
	if f.reads["sm64.z64"] != 1 {
		t.Errorf("file read %d times, want 1", f.reads["sm64.z64"])
	}
}

// A zip no folder placed takes its platform from the file inside it.
func TestZipPlacedByItsInnerFile(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"m.zip": {
		Sums: sums("3CE60709", marioSHA), InnerName: "Super Mario 64 (USA).z64", Platform: "n64",
	}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "m.zip", "")
	f.run(t)
	it := f.item(t, id)
	if it.Platform == nil || *it.Platform != "n64" || it.Title != "Super Mario 64" {
		t.Errorf("platform %v title %q", it.Platform, it.Title)
	}
}

// A .bin no folder placed and no extension inside could name is placed by
// its SHA-1.
func TestUnplacedROMPlacedBySHA1(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"x.bin": {Sums: sums("", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	id := f.add(t, "x.bin", "")
	f.run(t)
	if it := f.item(t, id); it.Platform == nil || *it.Platform != "n64" {
		t.Errorf("platform = %v", it.Platform)
	}
}

type fakeArt struct {
	mu   sync.Mutex
	urls []string
}

func (a *fakeArt) Download(_ context.Context, url string) (string, int, int, int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.urls = append(a.urls, url)
	return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", 10, 10, 1, nil
}

// Box art is a network fetch and is off until the setting is on.
func TestArtworkOnlyWhenEnabled(t *testing.T) {
	results := map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}}
	for _, on := range []bool{false, true} {
		f := newFixture(t, results)
		art := &fakeArt{}
		f.w.Index = func() *retrodb.Index { return testIndex(t) }
		f.w.Art = art
		f.w.Artwork = func() bool { return on }
		f.add(t, "sm64.z64", "n64")
		f.run(t)
		if !on && len(art.urls) != 0 {
			t.Errorf("fetched %v with artwork off", art.urls)
		}
		if on {
			want := "https://thumbnails.libretro.com/Nintendo%20-%20Nintendo%2064/Named_Boxarts/Super%20Mario%2064%20%28USA%29.png"
			if len(art.urls) != 2 || art.urls[0] != want {
				t.Errorf("fetched %v, want box art then snap", art.urls)
			}
		}
	}
}

// A pass that changed something records when it finished; a later pass with
// nothing to do keeps that stamp rather than hiding it.
func TestFinishedAtSurvivesAnEmptyPass(t *testing.T) {
	f := newFixture(t, map[string]romhash.Result{"sm64.z64": {Sums: sums("3CE60709", marioSHA)}})
	f.w.Index = func() *retrodb.Index { return testIndex(t) }
	f.add(t, "sm64.z64", "n64")
	f.run(t)
	first := f.w.Stats().FinishedAt
	if first == 0 {
		t.Fatal("no finish recorded for a pass that identified a game")
	}
	f.run(t)
	if got := f.w.Stats().FinishedAt; got != first {
		t.Errorf("an empty pass moved FinishedAt from %d to %d", first, got)
	}
}
