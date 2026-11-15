package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSRT(t *testing.T, path, text string, mtime time.Time) {
	t.Helper()
	body := "1\n00:00:01,000 --> 00:00:02,000\n" + text + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func cacheEntries(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".vtt") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// R09-5: a replaced or edited sidecar must be served as it now is, not as the
// conversion cached from the file that used to be there.
func TestSidecarCacheFollowsItsSource(t *testing.T) {
	ctx := context.Background()
	cache := t.TempDir()
	e := NewExtractor(cache)
	src := filepath.Join(t.TempDir(), "Film.en.srt")
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeSRT(t, src, "OLD TEXT", t0)
	got, err := e.Sidecar(ctx, src, "srt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "OLD TEXT") {
		t.Fatalf("first conversion = %q", got)
	}

	// A different length: a re-synced copy dropped over the old one.
	writeSRT(t, src, "NEW TEXT, re-synced", t0.Add(time.Hour))
	got, err = e.Sidecar(ctx, src, "srt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "NEW TEXT, re-synced") || strings.Contains(string(got), "OLD TEXT") {
		t.Fatalf("after replacing the file, served %q", got)
	}

	// The same length, only the time changed: an edit that keeps the size.
	writeSRT(t, src, "SAME TEXT, re-synced", t0.Add(2*time.Hour))
	got, err = e.Sidecar(ctx, src, "srt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "SAME TEXT, re-synced") {
		t.Fatalf("after a same-length edit, served %q", got)
	}

	// A second request for the unchanged file is served from the cache.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Sidecar(ctx, src, "srt"); err == nil {
		t.Fatal("a deleted source was served from the cache")
	}
}

// Old versions do not pile up: each source keeps one cache entry.
func TestSidecarCacheKeepsOneEntryPerSource(t *testing.T) {
	ctx := context.Background()
	cache := t.TempDir()
	e := NewExtractor(cache)
	dir := t.TempDir()
	a := filepath.Join(dir, "A.srt")
	b := filepath.Join(dir, "B.srt")
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeSRT(t, b, "B", t0)
	if _, err := e.Sidecar(ctx, b, "srt"); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		writeSRT(t, a, strings.Repeat("x", i+1), t0.Add(time.Duration(i)*time.Minute))
		if _, err := e.Sidecar(ctx, a, "srt"); err != nil {
			t.Fatal(err)
		}
	}
	if got := cacheEntries(t, cache); len(got) != 2 {
		t.Fatalf("cache holds %d entries after five edits of one file and one of another, want 2: %v", len(got), got)
	}

	// An entry from before versioned keys is swept on the next write.
	legacy := filepath.Join(cache, cacheKey(b, "sidecar")[:2], cacheKey(b, "sidecar")+".vtt")
	if err := os.WriteFile(legacy, []byte("WEBVTT\n\nstale"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSRT(t, b, "B2", t0.Add(time.Hour))
	got, err := e.Sidecar(ctx, b, "srt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "stale") {
		t.Fatal("served an unversioned entry from before the fix")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("unversioned entry was not pruned")
	}
}
