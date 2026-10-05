package faces

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * Which picture the detector reads.
 *
 * The file when the detector sees it upright, the cached display copy when it
 * would not: a format it cannot decode, or an EXIF turn it does not apply.
 * Measured on the live library before this existed: a face found on 61% of
 * upright photos, 10% of sideways ones, and none of 27 HEIC, BMP or WebP.
 */

// planWorker answers "upright?" from a set rather than from real files, and
// finds a display copy for every item unless told otherwise.
func planWorker(upright map[string]bool, copies map[int64]string) *Worker {
	w := quietWorker(newFakeStore())
	w.upright = func(p string) bool { return upright[p] }
	w.Display = func(_ context.Context, it store.Item) (string, bool) {
		p, ok := copies[it.ID]
		return p, ok
	}
	return w
}

func TestTheDetectorReadsTheUprightCopyWhenTheFileIsNot(t *testing.T) {
	w := planWorker(
		map[string]bool{`C:\pics\upright.jpg`: true},
		map[int64]string{1: `D:\art\1.jpg`, 2: `D:\art\2.jpg`, 3: `D:\art\3.jpg`},
	)
	items := []store.Item{
		{ID: 1, Path: `C:\pics\upright.jpg`},
		{ID: 2, Path: `C:\pics\sideways.jpg`},
		{ID: 3, Path: `C:\pics\IMG_0706.HEIC`},
	}
	byPath, order := w.plan(context.Background(), items)

	want := map[string]struct {
		id    int64
		frame string
	}{
		`C:\pics\upright.jpg`: {1, ""},
		`D:\art\2.jpg`:        {2, store.FrameDisplay},
		`D:\art\3.jpg`:        {3, store.FrameDisplay},
	}
	if len(order) != len(want) {
		t.Fatalf("sent %v, want %d paths", order, len(want))
	}
	for path, w := range want {
		got := byPath[path]
		if len(got) != 1 || got[0].item.ID != w.id || got[0].frame != w.frame {
			t.Errorf("%s -> %+v, want item %d frame %q", path, got, w.id, w.frame)
		}
	}
}

// Exact duplicates share one cached copy. It is sent once and both photos get
// what is found in it -- keyed by path, a second entry would overwrite the first.
func TestDuplicatesSharingACopyBothGetItsFaces(t *testing.T) {
	st := newFakeStore()
	w := quietWorker(st)
	w.upright = func(string) bool { return false }
	w.Display = func(context.Context, store.Item) (string, bool) { return `D:\art\same.jpg`, true }

	byPath, order := w.plan(context.Background(), []store.Item{
		{ID: 10, Path: `C:\pics\a.heic`}, {ID: 11, Path: `C:\pics\copy of a.heic`},
	})
	if len(order) != 1 {
		t.Fatalf("sent %v, want the shared copy once", order)
	}

	var r result
	_ = json.Unmarshal([]byte(`{"path":"D:\\art\\same.jpg","faces":[{"x":1,"y":2,"w":3,"h":4,`+
		`"score":0.9,"embedding":[1,0]}]}`), &r)
	for _, tg := range byPath[r.Path] {
		w.record(context.Background(), tg.item, tg.frame, r)
	}
	for _, id := range []int64{10, 11} {
		if got := st.recorded[id]; len(got) != 1 || got[0].Frame != store.FrameDisplay {
			t.Errorf("item %d recorded %+v, want one face in the display frame", id, got)
		}
	}
}

// No copy yet -- the photo worker has not reached it -- and the file is read
// as before, rather than the photograph being skipped or left pending.
func TestWithNoCopyTheFileIsReadAsBefore(t *testing.T) {
	w := planWorker(nil, nil)
	byPath, _ := w.plan(context.Background(), []store.Item{{ID: 4, Path: `C:\pics\x.heic`}})
	if got := byPath[`C:\pics\x.heic`]; len(got) != 1 || got[0].frame != "" {
		t.Errorf("plan = %+v, want the file in its own frame", byPath)
	}
}

// The real check: formats the detector cannot decode are not upright, and an
// EXIF turn is read from the file.
func TestReadsUpright(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, body []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, name := range []string{"a.heic", "a.HEIF", "a.bmp", "a.webp"} {
		if readsUpright(write(name, []byte("x"))) {
			t.Errorf("%s reads upright; the detector cannot decode it", name)
		}
	}
	// A JPEG with no EXIF is stored the way it is seen.
	if !readsUpright(write("plain.JPG", []byte{0xFF, 0xD8, 0xFF, 0xD9})) {
		t.Error("a JPEG with no orientation should read upright")
	}
	// Orientation 6, a phone held upright, in a minimal EXIF segment.
	if readsUpright(write("turned.jpg", exifJPEG(6))) {
		t.Error("a JPEG turned by EXIF read upright")
	}
	if !readsUpright(write("one.jpg", exifJPEG(1))) {
		t.Error("orientation 1 is upright")
	}
}

// exifJPEG is the smallest JPEG prefix carrying one orientation tag: enough for
// the reader, which never decodes the picture.
func exifJPEG(orientation byte) []byte {
	tiff := []byte{
		'M', 'M', 0, 42, 0, 0, 0, 8, // header, IFD0 at 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, orientation, 0, 0, // orientation, SHORT
		0, 0, 0, 0, // no next IFD
	}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	n := len(seg) + 2
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte(n >> 8), byte(n)}
	out = append(out, seg...)
	return append(out, 0xFF, 0xD9)
}
