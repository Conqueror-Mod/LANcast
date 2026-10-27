package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"lancast/internal/probe"
	"lancast/internal/store"
)

// chapteredFilm makes a six-second MKV with three named chapters in the
// harness library and registers it. Needs ffmpeg; the parse rules themselves
// are covered without it in internal/probe.
func chapteredFilm(t *testing.T, h *harness) int64 {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil || !h.srvAPI.prober.Available() {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	meta := filepath.Join(t.TempDir(), "chapters.txt")
	os.WriteFile(meta, []byte(";FFMETADATA1\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=2000\ntitle=Opening\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=2000\nEND=4000\ntitle=Middle\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=4000\nEND=6000\ntitle=End\n"), 0o644)
	path := filepath.Join(h.dir, "chaptered.mkv")
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:d=6",
		"-i", meta, "-map_metadata", "1", "-c:v", "mpeg4", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make fixture: %v %s", err, out)
	}
	fi, _ := os.Stat(path)
	id, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: path, Kind: "movie", Title: "Chaptered", SortTitle: "Chaptered",
		Container: "mkv", SizeBytes: fi.Size(), MTime: fi.ModTime().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChaptersAreReadFromTheFile(t *testing.T) {
	h := newHarness(t)
	id := chapteredFilm(t, h)

	var got chapterList
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/chapters", nil), &got)
	if len(got.Chapters) != 3 || got.Chapters[1].Title != "Middle" ||
		got.Chapters[1].StartMS != 2000 || got.Chapters[2].EndMS < 5900 {
		t.Fatalf("chapters = %+v", got.Chapters)
	}

	// The second ask is answered from memory; same answer.
	var again chapterList
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/chapters", nil), &again)
	if len(again.Chapters) != 3 {
		t.Fatalf("cached answer = %+v", again.Chapters)
	}
}

// A file without chapters, and a file ffprobe cannot read at all, both answer
// an empty list: the player carries on exactly as before chapters existed.
func TestNoChaptersIsAnEmptyListNotAnError(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "not-really-a-film.mkv", []byte("these bytes are not a video"))
	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/chapters", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got chapterList
	decode(t, resp, &got)
	if got.Chapters == nil || len(got.Chapters) != 0 {
		t.Fatalf("got %#v, want []", got.Chapters)
	}

	missing := h.do(t, "GET", "/api/items/99999/chapters", nil)
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown item = %d", missing.StatusCode)
	}
}

// The cache is keyed on the file as well as the item: a file replaced in place
// (a better copy dropped over the old one) is read again.
func TestTheCacheNoticesAReplacedFile(t *testing.T) {
	var c chapterCache
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	os.WriteFile(p, []byte("one"), 0o644)
	fi1, _ := os.Stat(p)
	c.put(1, fi1, []probe.Chapter{{Title: "old"}})
	if got, ok := c.get(1, fi1); !ok || got[0].Title != "old" {
		t.Fatal("miss on an unchanged file")
	}
	os.WriteFile(p, []byte("a longer replacement"), 0o644)
	fi2, _ := os.Stat(p)
	if _, ok := c.get(1, fi2); ok {
		t.Fatal("a replaced file was answered from the cache")
	}
}
