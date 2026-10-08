package retrodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/retro/romhash"
)

// A fixture in the shape libretro's DATs have, with invented games and hashes.
const gamesDAT = `clrmamepro (
	name "Nintendo - Nintendo 64"
	description "Nintendo - Nintendo 64"
	version "2026.08.01"
)

game (
	name "Test Racer (USA)"
	region "USA"
	serial "NTRE"
	rom ( name "Test Racer (USA).z64" size 8388608 crc 0A1B2C3D md5 00 sha1 1111111111111111111111111111111111111111 serial "NTRE" )
)
game (
	name "Test Racer (Europe) (En,Fr,De)"
	region "Europe"
	rom ( name "Test Racer (Europe) (En,Fr,De).z64" size 8388608 crc 0a1b2c3e sha1 2222222222222222222222222222222222222222 )
)
game (
	name "Odd (Name) - With Parens (Japan)"
	region "Japan"
	rom ( name "x.z64" size 1 crc DEADBEEF sha1 3333333333333333333333333333333333333333 )
)
`

const yearDAT = `clrmamepro (
	name "Nintendo - Nintendo 64"
)

game (
	comment "Test Racer (USA)"
	releaseyear "1997"
	rom ( crc 0A1B2C3D )
)

game (
	comment "Test Racer (Europe) (En,Fr,De)"
	releaseyear "1998"
	rom ( crc FFFFFFFF )
)
`

const genreDAT = `game (
	comment "Test Racer (USA)"
	genre "Racing"
	rom ( crc 0A1B2C3D )
)
`

const ps1DAT = `game (
	name "Test Quest (USA) (Disc 1)"
	region "USA"
	serial "SLUS-01234, SLUS-01235"
	releaseyear "1999"
	rom ( name "Test Quest (USA) (Disc 1).bin" size 700000000 crc 12345678 sha1 4444444444444444444444444444444444444444 serial "SLUS-01234" )
)
`

func mustParse(t *testing.T, s string) []Entry {
	t.Helper()
	e, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestParseGames(t *testing.T) {
	e := mustParse(t, gamesDAT)
	if len(e) != 3 {
		t.Fatalf("got %d games, want 3 (the header is not a game)", len(e))
	}
	g := e[0]
	if g.Name != "Test Racer (USA)" || g.Region != "USA" || g.Serial != "NTRE" {
		t.Errorf("game = %+v", g)
	}
	if len(g.ROMs) != 1 || g.ROMs[0].CRC != "0A1B2C3D" || g.ROMs[0].SHA1 != strings.Repeat("1", 40) {
		t.Errorf("rom = %+v", g.ROMs)
	}
	// Lower-case hex in a DAT is normalised to the case romhash writes.
	if e[1].ROMs[0].CRC != "0A1B2C3E" {
		t.Errorf("crc not upper-cased: %q", e[1].ROMs[0].CRC)
	}
}

func TestParseRejectsUnclosedBlock(t *testing.T) {
	if _, err := Parse(strings.NewReader("game (\n name \"x\"\n")); err == nil {
		t.Error("an unclosed block parsed")
	}
}

func n64Index(t *testing.T) *Index {
	ix := &Index{}
	ix.Build("n64", mustParse(t, gamesDAT), mustParse(t, yearDAT), mustParse(t, genreDAT))
	ix.Build("ps1", mustParse(t, ps1DAT))
	return ix
}

func TestLookupBySHA1(t *testing.T) {
	g := n64Index(t).Lookup("n64", []romhash.Sums{{CRC32: "00000000", SHA1: strings.Repeat("2", 40)}}, "")
	if g == nil || g.Name != "Test Racer (Europe) (En,Fr,De)" {
		t.Fatalf("got %+v", g)
	}
}

func TestLookupByCRCWhenSHA1Misses(t *testing.T) {
	g := n64Index(t).Lookup("n64", []romhash.Sums{{CRC32: "0A1B2C3D", SHA1: strings.Repeat("9", 40)}}, "")
	if g == nil || g.Name != "Test Racer (USA)" {
		t.Fatalf("got %+v", g)
	}
}

// The second layout — a headered dump's body — matches when the first does
// not.
func TestLookupTriesEachLayout(t *testing.T) {
	g := n64Index(t).Lookup("n64", []romhash.Sums{
		{CRC32: "99999999", SHA1: strings.Repeat("9", 40)},
		{CRC32: "DEADBEEF", SHA1: strings.Repeat("8", 40)},
	}, "")
	if g == nil || g.Name != "Odd (Name) - With Parens (Japan)" {
		t.Fatalf("got %+v", g)
	}
}

// Metadata DATs join by CRC, and by name when the CRC differs — libretro's
// year DAT and its game DAT do not always agree on a dump's CRC.
func TestMetadataJoins(t *testing.T) {
	ix := n64Index(t)
	usa := ix.Lookup("n64", []romhash.Sums{{CRC32: "0A1B2C3D"}}, "")
	if usa.Year != 1997 || usa.Genre != "Racing" {
		t.Errorf("by crc: %+v", usa)
	}
	eur := ix.Lookup("n64", []romhash.Sums{{CRC32: "0A1B2C3E"}}, "")
	if eur.Year != 1998 {
		t.Errorf("by name: year %d, want 1998", eur.Year)
	}
}

func TestLookupBySerial(t *testing.T) {
	ix := n64Index(t)
	for _, s := range []string{"SLUS-01234", "slus-01235"} {
		g := ix.Lookup("ps1", nil, s)
		if g == nil || g.Name != "Test Quest (USA) (Disc 1)" || g.Year != 1999 {
			t.Errorf("serial %s: got %+v", s, g)
		}
	}
}

// With no platform, a SHA-1 places the ROM; a CRC alone does not, because a
// CRC32 is only unique within one console's DAT.
func TestLookupWithoutPlatform(t *testing.T) {
	ix := n64Index(t)
	g := ix.Lookup("", []romhash.Sums{{SHA1: strings.Repeat("1", 40)}}, "")
	if g == nil || g.Platform != "n64" {
		t.Fatalf("sha1 without platform: %+v", g)
	}
	if g := ix.Lookup("", []romhash.Sums{{CRC32: "0A1B2C3D"}}, ""); g != nil {
		t.Errorf("a bare CRC placed a ROM across consoles: %+v", g)
	}
}

func TestLookupOnNilIndex(t *testing.T) {
	var ix *Index
	if ix.Lookup("n64", []romhash.Sums{{SHA1: "x"}}, "") != nil {
		t.Error("nil index matched")
	}
}

// Every pinned URL is on the pinned commit, and no two files share a name.
func TestPinnedFiles(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Files() {
		if seen[f.Name] {
			t.Errorf("duplicate name %s", f.Name)
		}
		seen[f.Name] = true
		if !strings.HasPrefix(f.URL(), "https://raw.githubusercontent.com/libretro/libretro-database/"+Commit+"/metadat/") {
			t.Errorf("%s: unpinned URL %s", f.Name, f.URL())
		}
		if strings.Contains(f.URL(), " ") {
			t.Errorf("%s: unescaped URL %s", f.Name, f.URL())
		}
		if len(f.SHA256) != 64 || f.SizeBytes <= 0 {
			t.Errorf("%s: incomplete pin", f.Name)
		}
	}
	if len(platformFiles) != 9 {
		t.Errorf("%d platforms pinned, want 9", len(platformFiles))
	}
}

// withFakeSet swaps the pinned set for two small files served by a test
// server, with digests computed from what it serves.
func withFakeSet(t *testing.T, bodies map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := bodies[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(b))
	}))
	t.Cleanup(srv.Close)

	pin := func(name, body string) File {
		s := sha256.Sum256([]byte(body))
		return File{Name: name, Source: name, SHA256: hex.EncodeToString(s[:]), SizeBytes: int64(len(body))}
	}
	oldSet, oldURL := platformFiles, fileURL
	platformFiles = []platformSet{{
		Platform: "n64",
		Games:    pin("n64.dat", gamesDAT),
		Metadata: []File{pin("n64-releaseyear.dat", yearDAT)},
	}}
	fileURL = func(f File) string { return srv.URL + "/" + f.Source }
	t.Cleanup(func() { platformFiles, fileURL = oldSet, oldURL })
	return srv
}

func TestInstallThenLoad(t *testing.T) {
	withFakeSet(t, map[string]string{"n64.dat": gamesDAT, "n64-releaseyear.dat": yearDAT})
	dir := filepath.Join(t.TempDir(), "retrodb")
	if Installed(dir) {
		t.Fatal("installed before install")
	}
	var last Progress
	if err := Install(context.Background(), dir, func(p Progress) { last = p }); err != nil {
		t.Fatal(err)
	}
	if !Installed(dir) {
		t.Fatal("not installed after install")
	}
	if last.BytesDone != last.BytesTotal {
		t.Errorf("progress ended at %d of %d", last.BytesDone, last.BytesTotal)
	}
	ix, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g := ix.Lookup("n64", []romhash.Sums{{CRC32: "0A1B2C3D"}}, ""); g == nil || g.Year != 1997 {
		t.Errorf("lookup after load: %+v", g)
	}
}

// A file that does not match its pin fails the install, and leaves the
// previous install exactly as it was.
func TestInstallChecksumMismatchKeepsPreviousInstall(t *testing.T) {
	withFakeSet(t, map[string]string{"n64.dat": gamesDAT, "n64-releaseyear.dat": yearDAT})
	dir := filepath.Join(t.TempDir(), "retrodb")
	if err := Install(context.Background(), dir, nil); err != nil {
		t.Fatal(err)
	}

	// Same length, different bytes: past the size check, caught by the digest.
	tampered := strings.Replace(yearDAT, "1997", "2001", 1)
	withFakeSet(t, map[string]string{"n64.dat": gamesDAT, "n64-releaseyear.dat": tampered})
	platformFiles[0].Metadata[0].SHA256 = func() string {
		s := sha256.Sum256([]byte(yearDAT))
		return hex.EncodeToString(s[:])
	}()

	err := Install(context.Background(), dir, nil)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	if !Installed(dir) {
		t.Error("a failed reinstall removed the previous install")
	}
	if _, err := os.Stat(dir + ".part"); !os.IsNotExist(err) {
		t.Error("the staging directory was left behind")
	}
}

func TestPartialInstallIsAbsent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "n64.dat"), []byte(gamesDAT), 0o644)
	if Installed(dir) {
		t.Error("files with no manifest counted as installed")
	}
	os.WriteFile(filepath.Join(dir, manifestName), []byte("someothercommit\n"), 0o644)
	if Installed(dir) {
		t.Error("an install of another commit counted as this one")
	}
}
