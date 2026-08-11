package playlist

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fake library: paths that exist, and what they resolve to. No database — the
// behaviour under test is which lines resolve and what is reported about the
// ones that do not, none of which is about SQL.
type fakeStore struct {
	known   map[string]int64
	entries []int64
	pid     int64
	path    string
	title   string
}

func (f *fakeStore) ItemIDByPath(_ context.Context, p string) (int64, error) {
	return f.known[p], nil
}

func (f *fakeStore) EnsurePlaylist(_ context.Context, _ int64, path, title, _ string) (int64, error) {
	f.path, f.title = path, title
	if f.pid == 0 {
		f.pid = 99
	}
	return f.pid, nil
}

func (f *fakeStore) SetPlaylistEntries(_ context.Context, _ int64, ids []int64) error {
	f.entries = ids
	return nil
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	m3u := write(t, dir, "Road Trip.m3u", "a.mp3\nsub/b.mp3\n")

	st := &fakeStore{known: map[string]int64{
		filepath.Join(dir, "a.mp3"):        11,
		filepath.Join(dir, "sub", "b.mp3"): 22,
	}}

	res, err := ImportFile(context.Background(), st, 1, m3u)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || len(st.entries) != 2 {
		t.Fatalf("imported %d, entries %v", res.Imported, st.entries)
	}
	if st.entries[0] != 11 || st.entries[1] != 22 {
		t.Errorf("entries = %v, want [11 22] in file order", st.entries)
	}
	if res.Title != "Road Trip" {
		t.Errorf("Title = %q, want the file name without its extension", res.Title)
	}
	if st.path != m3u {
		t.Errorf("playlist keyed on %q, want the .m3u path so a re-import updates it", st.path)
	}
}

// The requirement from ADR 0030: what could not be found is counted and
// reported, never silently dropped. Importing 1 of 3 and saying nothing
// produces a playlist that looks complete and is not.
func TestImportReportsWhatItCouldNotFind(t *testing.T) {
	dir := t.TempDir()
	m3u := write(t, dir, "p.m3u", "here.mp3\ngone.mp3\nalso-gone.mp3\n")
	st := &fakeStore{known: map[string]int64{filepath.Join(dir, "here.mp3"): 7}}

	res, err := ImportFile(context.Background(), st, 1, m3u)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Errorf("Imported = %d, want 1", res.Imported)
	}
	if res.MissingCount != 2 {
		t.Errorf("MissingCount = %d, want 2", res.MissingCount)
	}
	if len(res.Missing) != 2 || res.Missing[0] != "gone.mp3" {
		t.Errorf("Missing = %v, want the paths as written", res.Missing)
	}
}

// A URL is not a missing file. Reporting a radio stream as something we could
// not find sends someone looking for a file that was never meant to be there.
func TestImportSkipsURLsWithoutCallingThemMissing(t *testing.T) {
	dir := t.TempDir()
	m3u := write(t, dir, "p.m3u", "http://example.com/live\nhere.mp3\n")
	st := &fakeStore{known: map[string]int64{filepath.Join(dir, "here.mp3"): 7}}

	res, _ := ImportFile(context.Background(), st, 1, m3u)
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.MissingCount != 0 {
		t.Errorf("MissingCount = %d, want 0 — a stream is not a missing file", res.MissingCount)
	}
	if res.Imported != 1 {
		t.Errorf("Imported = %d, want 1", res.Imported)
	}
}

// A playlist that resolves to nothing is still created. A scan that finds
// "Road Trip.m3u", imports nothing and says nothing leaves someone wondering
// why their playlist never appeared.
func TestImportCreatesAnEmptyPlaylistRatherThanNothing(t *testing.T) {
	dir := t.TempDir()
	m3u := write(t, dir, "Ghosts.m3u", "one.mp3\ntwo.mp3\n")
	st := &fakeStore{known: map[string]int64{}}

	res, err := ImportFile(context.Background(), st, 1, m3u)
	if err != nil {
		t.Fatal(err)
	}
	if res.PlaylistID == 0 {
		t.Error("no playlist was created")
	}
	if res.Imported != 0 || res.MissingCount != 2 {
		t.Errorf("got %+v", res)
	}
}

// Repeats survive the import. This is the property the whole schema revision
// exists for, checked end to end rather than only at the table.
func TestImportKeepsRepeats(t *testing.T) {
	dir := t.TempDir()
	m3u := write(t, dir, "set.m3u", "a.mp3\nb.mp3\na.mp3\n")
	st := &fakeStore{known: map[string]int64{
		filepath.Join(dir, "a.mp3"): 1,
		filepath.Join(dir, "b.mp3"): 2,
	}}

	res, _ := ImportFile(context.Background(), st, 1, m3u)
	if res.Imported != 3 {
		t.Fatalf("Imported = %d, want 3", res.Imported)
	}
	if len(st.entries) != 3 || st.entries[0] != 1 || st.entries[2] != 1 {
		t.Errorf("entries = %v, want [1 2 1]", st.entries)
	}
}

// A long-dead playlist should not produce a thousand-line report.
func TestMissingListIsCapped(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("gone")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(".mp3\n")
	}
	m3u := write(t, dir, "old.m3u", b.String())

	res, _ := ImportFile(context.Background(), &fakeStore{known: map[string]int64{}}, 1, m3u)
	if res.MissingCount != 50 {
		t.Errorf("MissingCount = %d, want the true total 50", res.MissingCount)
	}
	if len(res.Missing) != maxReported {
		t.Errorf("len(Missing) = %d, want it capped at %d", len(res.Missing), maxReported)
	}
}

// Our own HLS playlists must not be importable, and the error has to be
// distinguishable so a scanner can ignore the file rather than report a fault.
func TestImportRefusesHLS(t *testing.T) {
	dir := t.TempDir()
	m3u := write(t, dir, "stream.m3u8", "#EXTM3U\n#EXT-X-TARGETDURATION:6\nseg0.m4s\n")
	_, err := ImportFile(context.Background(), &fakeStore{known: map[string]int64{}}, 1, m3u)
	if err == nil || !strings.Contains(err.Error(), "HLS") {
		t.Fatalf("err = %v, want an HLS refusal", err)
	}
}
