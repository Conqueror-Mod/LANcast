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

// A hash match whose release traits agree scores 1.0 and wins: the hash still
// carries decisive weight, it is just no longer trusted blindly across releases.
func TestHashMatchWithAgreeingTraitsWins(t *testing.T) {
	target := Target{FileName: "Film.2020.1080p.BluRay-GROUP.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Film.2020.1080p.WEB-OTHER", FPS: 23.976, DownloadCount: 50000},
		{FileID: 2, Release: "Film.2020.1080p.BluRay-GROUP", FPS: 23.976, DownloadCount: 3, HashMatch: true},
	}
	Rank(target, cands)

	if cands[0].FileID != 2 {
		t.Fatalf("best = %d, want the agreeing hash match to win", cands[0].FileID)
	}
	if cands[0].Score != 1.0 {
		t.Errorf("agreeing hash match scored %.2f, want 1.0", cands[0].Score)
	}
	if _, auto := BestAutoMatch(cands); !auto {
		t.Error("a hash match with agreeing traits did not qualify for auto-apply")
	}
}

// A claimed hash match at a conflicting frame rate drifts through the film, so
// it must not win over — or auto-apply above — a release that matches this file,
// however many more downloads it has. OpenSubtitles marks several different
// encodes as matching one file's hash, so the flag alone is not proof.
func TestHashMatchDemotedOnFrameRateConflict(t *testing.T) {
	target := Target{FileName: "Film.2020.1080p.BluRay-GROUP.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Film.2020.1080p.BluRay-GROUP", FPS: 23.976, DownloadCount: 10},
		{FileID: 2, Release: "Film.2020.DVDRip", FPS: 25, DownloadCount: 90000, HashMatch: true},
	}
	Rank(target, cands)

	if cands[0].FileID != 1 {
		t.Fatalf("best = %d, want the matching frame rate over the claimed hash match", cands[0].FileID)
	}
	for _, c := range cands {
		if c.FileID == 2 && c.Score >= AutoApply {
			t.Errorf("a frame-rate-conflicting hash match scored %.2f and would auto-apply", c.Score)
		}
	}
}

// The Corpse Bride case from the field: OpenSubtitles returns several releases
// of the same film, all flagged as hash matches. They must not all tie at 1.0
// and sort by downloads — which puts a lower-resolution rip on top of a 1080p
// file. The release that matches this file ranks first.
func TestMultipleHashMatchesRankByRelease(t *testing.T) {
	target := Target{FileName: "Corpse.Bride.2005.1080p.BluRay.x264.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Corpse.Bride.2005.720p.BluRay.x264", FPS: 23.976, DownloadCount: 90000, HashMatch: true},
		{FileID: 2, Release: "Corpse.Bride.2005.1080p.BluRay.x264", FPS: 23.976, DownloadCount: 20000, HashMatch: true},
	}
	Rank(target, cands)

	if cands[0].FileID != 2 {
		t.Fatalf("best = %d, want the 1080p release over the more-downloaded 720p hash match", cands[0].FileID)
	}
	if cands[0].Score <= cands[1].Score {
		t.Error("the release-matching hash match should outscore the mismatched one, not tie")
	}
}

// The exact field case: a bare-named 1080p file, and a DVD-rip subtitle claiming
// a hash match with the same frame rate and no resolution token. Only the probe
// anchors the file at 1080p; the rip's DVD source is standard-definition, a
// different master, so it must rank below the 1080p release rather than lead on
// its 89k downloads.
func TestDVDRipHashDemotedOnHDFile(t *testing.T) {
	target := Target{FileName: "Corpse Bride.mkv", FPS: 23.976, Height: 1080}
	cands := []Candidate{
		{FileID: 1, Release: "Corpse.Bride[2005]DvDrip.AC3[Eng]-aXXo", FPS: 23.976, DownloadCount: 89000, HashMatch: true},
		{FileID: 2, Release: "Corpse.Bride.2005.1080p.BrRip.x264", FPS: 23.976, DownloadCount: 16000, HashMatch: true},
	}
	Rank(target, cands)

	if cands[0].FileID != 2 {
		t.Fatalf("best = %d, want the 1080p release over the more-downloaded DVD-rip", cands[0].FileID)
	}
	if cands[0].Score <= cands[1].Score {
		t.Error("the DVD-rip must not tie the 1080p release on a 1080p file")
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

// The field case that motivated this: "Aladdin (1992)" and an "Aladdin.2019"
// subtitle share a title, so the title gate passes them, but they are different
// films. A wrong-year candidate — even a claimed hash match — must never
// auto-apply, and must rank below the correct-year release.
func TestDifferentYearNeverAutoApplies(t *testing.T) {
	target := Target{
		FileName: "Aladdin.1992.1080p.BluRay-GRP.mkv",
		Title:    "Aladdin",
		Year:     1992,
		FPS:      23.976, Height: 1080,
	}
	cands := []Candidate{
		{FileID: 1, Release: "Aladdin.2019.1080p.WEBRip.x264", HashMatch: true, DownloadCount: 800000},
		{FileID: 2, Release: "Aladdin.1992.1080p.BluRay-GRP", FPS: 23.976, DownloadCount: 40},
	}
	Rank(target, cands)

	if cands[0].FileID != 2 {
		t.Fatalf("best = %d, want the 1992 release ranked over the popular 2019 hash match", cands[0].FileID)
	}
	// The 2019 hash match must be demoted and explained.
	for _, c := range cands {
		if c.FileID == 1 {
			if c.Score >= AutoApply {
				t.Errorf("the 2019 subtitle scored %.2f and would auto-apply", c.Score)
			}
			if !contains(c.Reason, "different year") {
				t.Errorf("reason %q does not explain the year mismatch", c.Reason)
			}
		}
	}
}

// The year gate must not block a correct match: same title, same year still
// auto-applies on a hash match.
func TestSameYearAutoApplies(t *testing.T) {
	target := Target{
		FileName: "Aladdin.1992.1080p.BluRay-GRP.mkv",
		Title:    "Aladdin",
		Year:     1992,
		FPS:      23.976, Height: 1080,
	}
	cands := []Candidate{
		{FileID: 1, Release: "Aladdin.1992.720p.WEB-DL", HashMatch: true, DownloadCount: 5},
	}
	Rank(target, cands)
	if _, auto := BestAutoMatch(cands); !auto {
		t.Errorf("a same-year hash match did not auto-apply (score %.2f, reason %q)",
			cands[0].Score, cands[0].Reason)
	}
}

// A candidate that omits its year is not penalised — a missing year is not a
// mismatch, so trait scoring still decides.
func TestUnknownCandidateYearNotPenalised(t *testing.T) {
	target := Target{
		FileName: "Aladdin.1992.1080p.BluRay-GRP.mkv",
		Title:    "Aladdin",
		Year:     1992,
		FPS:      23.976, Height: 1080,
	}
	cands := []Candidate{
		{FileID: 1, Release: "Aladdin.1080p.BluRay-GRP", HashMatch: true, DownloadCount: 5},
	}
	Rank(target, cands)
	if _, auto := BestAutoMatch(cands); !auto {
		t.Errorf("a yearless hash match was wrongly demoted (score %.2f, reason %q)",
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
