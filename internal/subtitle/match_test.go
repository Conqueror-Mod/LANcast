package subtitle

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRelease(t *testing.T) {
	tests := []struct {
		name string
		want Traits
	}{
		{"Event.Horizon.1997.2160p.BluRay.REMUX.HEVC.DTS-HD.MA.TrueHD.5.1-FGT.mkv",
			Traits{Source: "remux", Resolution: "2160p", Group: "fgt"}},
		{"Blade.Runner.2049.2017.1080p.WEB-DL.DD5.1.H264-FGT.mkv",
			Traits{Source: "web", Resolution: "1080p", Group: "fgt"}},
		{"Aliens.1986.Special.Edition.1080p.BluRay.x264-AMIABLE.mkv",
			Traits{Source: "bluray", Edition: "special", Resolution: "1080p", Group: "amiable"}},
		{"Blade.Runner.1982.Directors.Cut.1080p.BluRay-SPARKS.mkv",
			Traits{Source: "bluray", Edition: "directors", Resolution: "1080p", Group: "sparks"}},
	}
	for _, tt := range tests {
		got := ParseRelease(tt.name)
		if got != tt.want {
			t.Errorf("ParseRelease(%q)\n got %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
}

// Real transfers are rarely exactly 1080 tall — the probe of a real library
// showed heights of 1040, 1036, 802. Matching on equality would classify most
// of a library as unknown.
func TestHeightToResolution(t *testing.T) {
	tests := map[int]string{
		2160: "2160p", 1600: "2160p", 1080: "1080p", 1040: "1080p",
		1012: "1080p", 800: "720p", 720: "720p", 544: "576p", 480: "480p", 0: "",
	}
	for h, want := range tests {
		if got := HeightToResolution(h); got != want {
			t.Errorf("HeightToResolution(%d) = %q, want %q", h, got, want)
		}
	}
}

// A hash match means the subtitle was timed against these exact bytes. Nothing
// inferred from names can beat that.
func TestHashMatchWinsOutright(t *testing.T) {
	target := Target{FileName: "Film.2020.1080p.BluRay-GROUP.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Film.2020.1080p.BluRay-GROUP", FPS: 23.976, DownloadCount: 50000},
		{FileID: 2, Release: "Film.2020.720p.WEB-OTHER", FPS: 25, DownloadCount: 3, HashMatch: true},
	}
	Rank(target, cands)

	if cands[0].FileID != 2 {
		t.Fatalf("best = %d, want the hash match to win", cands[0].FileID)
	}
	if cands[0].Score != 1.0 {
		t.Errorf("hash match scored %.2f, want 1.0", cands[0].Score)
	}
	if _, auto := BestAutoMatch(cands); !auto {
		t.Error("a hash match did not qualify for auto-apply")
	}
}

// Frame rate mismatch drifts progressively worse through a film rather than
// being a constant offset a viewer could ignore.
func TestFrameRateMismatchDemoted(t *testing.T) {
	target := Target{FileName: "Film.2020.1080p.BluRay-GROUP.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Film.2020.1080p.BluRay-GROUP", FPS: 25, DownloadCount: 9000},
		{FileID: 2, Release: "Film.2020.1080p.BluRay-GROUP", FPS: 23.976, DownloadCount: 10},
	}
	Rank(target, cands)
	if cands[0].FileID != 2 {
		t.Errorf("best = %d, want the matching frame rate despite far fewer downloads", cands[0].FileID)
	}
}

// A different cut is a different film as far as timings go.
func TestEditionMismatchDemoted(t *testing.T) {
	target := Target{FileName: "Aliens.1986.Special.Edition.1080p.BluRay-AMIABLE.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Aliens.1986.Theatrical.1080p.BluRay-OTHER", FPS: 23.976, DownloadCount: 50000},
		{FileID: 2, Release: "Aliens.1986.Special.Edition.1080p.BluRay-AMIABLE", FPS: 23.976, DownloadCount: 20},
	}
	Rank(target, cands)
	if cands[0].FileID != 2 {
		t.Errorf("best = %d, want the matching edition over the more popular wrong cut", cands[0].FileID)
	}
}

// Popularity is a tiebreak. The most-downloaded entry is frequently for a
// different release entirely.
func TestPopularityCannotCarryABadMatch(t *testing.T) {
	target := Target{FileName: "Film.2020.1080p.BluRay-GROUP.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Film.2020.720p.HDTV-WRONG", FPS: 25, DownloadCount: 999999},
	}
	Rank(target, cands)
	if _, auto := BestAutoMatch(cands); auto {
		t.Errorf("a mismatched release scored %.2f and would auto-apply on popularity alone", cands[0].Score)
	}
}

func TestNoComparableDetailsStaysBelowAutoApply(t *testing.T) {
	target := Target{FileName: "Film.mkv"}
	cands := []Candidate{{FileID: 1, Release: "Film", DownloadCount: 100000}}
	Rank(target, cands)

	if cands[0].Score >= AutoApply {
		t.Errorf("score %.2f with nothing to compare would auto-apply", cands[0].Score)
	}
	if cands[0].Reason == "" {
		t.Error("no reason given")
	}
}

// The direct example from the field: a Deadpool 2 subtitle whose frame rate and
// source agree with Avengers: Infinity War must never auto-apply, because it is
// a different film. Release-trait agreement is meaningless across movies.
func TestDifferentTitleNeverAutoApplies(t *testing.T) {
	target := Target{
		FileName: "Avengers.Infinity.War.2018.1080p.WEB-DL.mkv",
		Title:    "Avengers: Infinity War",
		FPS:      23.976, Height: 1080,
	}
	cands := []Candidate{
		{FileID: 1, Release: "Deadpool.2.2018.720p.WEB-DL.DD5", FPS: 23.976, DownloadCount: 90000},
	}
	Rank(target, cands)

	if _, auto := BestAutoMatch(cands); auto {
		t.Errorf("a different film scored %.2f and would auto-apply: %q",
			cands[0].Score, cands[0].Reason)
	}
	if !contains(cands[0].Reason, "different title") {
		t.Errorf("reason %q does not explain the title mismatch", cands[0].Reason)
	}
}

// A poisoned hash match — the provider claims a hash match but the release names
// a different movie — must not short-circuit to 1.0. The bytes of one film are
// not the bytes of another, so this is bad data, not proof.
func TestHashMatchToWrongTitleIsRejected(t *testing.T) {
	target := Target{
		FileName: "Avengers.Infinity.War.2018.1080p.mkv",
		Title:    "Avengers: Infinity War",
		FPS:      23.976, Height: 1080,
	}
	cands := []Candidate{
		{FileID: 1, Release: "Deadpool.2.2018.720p.WEB-DL", HashMatch: true, DownloadCount: 5},
	}
	Rank(target, cands)

	if cands[0].Score >= AutoApply {
		t.Errorf("a hash match to the wrong film scored %.2f and would auto-apply", cands[0].Score)
	}
}

// The guard must not block correct matches: a subtitle whose release names the
// same film still ranks and auto-applies on a hash or strong trait agreement,
// including when the subtitle release name is shorter than the item title.
func TestSameTitleStillMatches(t *testing.T) {
	target := Target{
		FileName: "Avengers.Infinity.War.2018.1080p.BluRay-GRP.mkv",
		Title:    "Avengers: Infinity War",
		FPS:      23.976, Height: 1080,
	}
	cands := []Candidate{
		{FileID: 1, Release: "Infinity.War.2018.1080p.BluRay-GRP", FPS: 23.976, HashMatch: true, DownloadCount: 20},
	}
	Rank(target, cands)

	if _, auto := BestAutoMatch(cands); !auto {
		t.Errorf("a correct same-film hash match did not auto-apply (score %.2f, reason %q)",
			cands[0].Score, cands[0].Reason)
	}
}

func TestBestAutoMatchEmpty(t *testing.T) {
	if _, ok := BestAutoMatch(nil); ok {
		t.Error("BestAutoMatch reported a match for an empty list")
	}
}

// The hash is size + the 64-bit words of the first and last 64KB.
func TestMovieHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "film.mkv")

	size := 256 * 1024
	body := make([]byte, size)
	for i := 0; i+8 <= len(body); i += 8 {
		binary.LittleEndian.PutUint64(body[i:], uint64(i))
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := MovieHash(path)
	if err != nil {
		t.Fatalf("MovieHash: %v", err)
	}
	if len(got) != 16 {
		t.Errorf("hash = %q, want 16 hex characters", got)
	}

	// Stable across calls, and different content gives a different hash.
	again, _ := MovieHash(path)
	if again != got {
		t.Error("hash is not stable")
	}

	body[0] ^= 0xFF
	os.WriteFile(path, body, 0o644)
	changed, _ := MovieHash(path)
	if changed == got {
		t.Error("changing the first bytes did not change the hash")
	}
}

func TestMovieHashTooSmall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tiny.mkv")
	os.WriteFile(path, make([]byte, 1024), 0o644)
	if _, err := MovieHash(path); err == nil {
		t.Error("MovieHash accepted a file smaller than two chunks")
	}
}

// LANcast must stay usable with no subtitle provider configured.
func TestUnconfiguredProvider(t *testing.T) {
	c := NewOpenSubtitles("")
	if c.Configured() {
		t.Error("Configured() is true with an empty key")
	}
	if _, err := c.Search(context.Background(), SearchQuery{Query: "x"}); err != ErrNoProvider {
		t.Errorf("Search error = %v, want ErrNoProvider", err)
	}
}

func TestSearchSendsHashAndKey(t *testing.T) {
	var gotHash, gotKey, gotAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHash = r.URL.Query().Get("moviehash")
		gotKey = r.Header.Get("Api-Key")
		gotAgent = r.Header.Get("User-Agent")
		w.Write([]byte(`{"data":[{"attributes":{"language":"en","download_count":42,
		 "fps":23.976,"release":"Film.2020.1080p.BluRay-GROUP","moviehash_match":true,
		 "files":[{"file_id":99,"file_name":"Film.srt"}]}}]}`))
	}))
	defer srv.Close()

	c := NewOpenSubtitles("test-key")
	c.SetBaseURL(srv.URL)

	cands, err := c.Search(context.Background(), SearchQuery{
		Query: "Film", MovieHash: "abc123", Languages: []string{"en"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotHash != "abc123" {
		t.Errorf("moviehash = %q, want it sent", gotHash)
	}
	if gotKey != "test-key" {
		t.Errorf("Api-Key = %q", gotKey)
	}
	// OpenSubtitles rejects requests without a descriptive User-Agent.
	if gotAgent == "" {
		t.Error("no User-Agent sent")
	}
	if len(cands) != 1 || cands[0].FileID != 99 || !cands[0].HashMatch {
		t.Errorf("candidates = %+v", cands)
	}
}

// A free-tier quota running out is expected, not a server fault.
func TestQuotaExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewOpenSubtitles("k")
	c.SetBaseURL(srv.URL)

	if _, _, err := c.DownloadLink(context.Background(), 1); err != ErrQuotaExhausted {
		t.Errorf("error = %v, want ErrQuotaExhausted", err)
	}
}

func TestRejectedKeyIsClear(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewOpenSubtitles("bad")
	c.SetBaseURL(srv.URL)

	_, err := c.Search(context.Background(), SearchQuery{Query: "x"})
	if err == nil || !contains(err.Error(), "API key") {
		t.Errorf("error = %v, want it to name the API key", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
