package api

import (
	"os"
	"path/filepath"
	"testing"
)

/*
 * Maintenance actions, and the line they must not cross.
 *
 * Everything here throws something away, so what is worth testing is not that
 * it went — it is what *stayed*. "Clear cache and data" means a dozen different
 * things across applications, several of them "lose your library", and the only
 * defence against drifting into one of those is a test that names the things
 * which must survive.
 */

func TestResetSettingsKeepsCredentialsAndPaths(t *testing.T) {
	h := newHarness(t)

	h.do(t, "PUT", "/api/settings", map[string]any{
		"tmdb_key":          "secret-tmdb",
		"omdb_key":          "secret-omdb",
		"opensubtitles_key": "secret-osdb",
		"ffmpeg_dir":        "C:/tools/ffmpeg",
		"rate_per_sec":      2,
		"write_nfo":         true,
		"watched_threshold": 60,
	}).Body.Close()

	// A password, the one thing whose loss would lock the operator out of their
	// own server.
	before := h.settings.Get()
	next := before
	next.PasswordHash = "$2a$10$notarealhash"
	if err := h.settings.Set(next); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "POST", "/api/settings/reset", nil)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	got := h.settings.Get()
	// Behaviour resets.
	if got.RatePerSec != 5 || got.WriteNFO || got.WatchedThreshold != 90 {
		t.Errorf("behaviour did not reset: rate=%v nfo=%v threshold=%d",
			got.RatePerSec, got.WriteNFO, got.WatchedThreshold)
	}
	// Credentials and machine facts do not.
	if got.PasswordHash != "$2a$10$notarealhash" {
		t.Error("reset wiped the password hash — that locks the operator out of their own server")
	}
	if got.TMDBKey != "secret-tmdb" || got.OMDbKey != "secret-omdb" || got.OpenSubtitlesKey != "secret-osdb" {
		t.Error("reset wiped provider keys, which a reset cannot restore and the user must retype")
	}
	if got.FFmpegDir != "C:/tools/ffmpeg" {
		t.Error("reset wiped the ffmpeg location, which is a fact about this machine rather than a preference")
	}
}

func TestClearArtworkCacheRemovesFilesAndNothingElse(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "film.mkv", []byte("f"))

	// Something in the cache directory to remove.
	dir := filepath.Join(h.dataDir, "artwork")
	if err := os.MkdirAll(filepath.Join(dir, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(dir, "ab", "abcdef.jpg")
	if err := os.WriteFile(blob, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "POST", "/api/cache/clear", map[string]any{"target": "artwork"})
	var body struct {
		Freed int64 `json:"freed_bytes"`
	}
	decode(t, resp, &body)
	if body.Freed < 2048 {
		t.Errorf("freed = %d bytes, want at least the 2048 that was there", body.Freed)
	}
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Error("the cached image is still there")
	}

	// The library is untouched. This is the assertion that matters: a cache is
	// a cache, and clearing one must not be able to cost anything but time.
	r2 := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Errorf("the item went with the cache: status = %d", r2.StatusCode)
	}
}

func TestClearCacheRejectsAnUnknownTarget(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "POST", "/api/cache/clear", map[string]any{"target": "database"}),
		400, "bad_request")
	wantError(t, h.do(t, "POST", "/api/cache/clear", map[string]any{}), 400, "bad_request")
}

// Debug logging is a setting so it survives a restart: the faults worth turning
// it on for are the intermittent ones, and losing the toggle on restart is how
// somebody ends up reproducing a bug three times.
func TestDebugLoggingIsPersisted(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"debug_logging": true}).Body.Close()

	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if body["debug_logging"] != true {
		t.Errorf("debug_logging = %v, want true", body["debug_logging"])
	}
	if !h.settings.Get().DebugLogging {
		t.Error("debug_logging did not reach the settings file")
	}
}
