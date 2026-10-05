package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * A face is cut from the picture its box was measured in.
 *
 * The file is painted one colour and its display copy another, so the crop's
 * colour says which one it came from. A box laid over the wrong picture still
 * returns a perfectly good JPEG -- of somebody's shoulder, or of nothing --
 * which is why the test reads the pixels rather than the status.
 */

func solidJPEG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var (
	fileRed     = color.RGBA{220, 20, 20, 255}
	displayBlue = color.RGBA{20, 20, 220, 255}
)

// facePhoto adds a photo whose file is body, with a blue display copy cached
// as its poster and one face recorded in frame.
func facePhoto(t *testing.T, h *harness, name string, body []byte, frame string) int64 {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(h.dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: path, Kind: "photo",
		Title: name, SortTitle: name, Container: "jpeg", SizeBytes: int64(len(body)), MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, w, hh, size, err := h.art.Put(solidJPEG(t, displayBlue))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.PutArtwork(ctx, id, hash, "poster", "", w, hh, size); err != nil {
		t.Fatal(err)
	}
	if err := h.st.RecordFaces(ctx, id, []store.Face{{
		X: 30, Y: 30, W: 40, H: 40, Score: 0.9, Embedding: []float32{1, 0}, Frame: frame,
	}}); err != nil {
		t.Fatal(err)
	}
	// A fresh database holds this one face, so it is the first id -- checked
	// rather than assumed, so a fixture change cannot point the test elsewhere.
	f, _, err := h.st.GetFace(ctx, 1, "")
	if err != nil || f.ItemID != id {
		t.Fatalf("face 1 = %+v, %v; want the face just recorded on item %d", f, err, id)
	}
	return f.ID
}

// cropColour fetches a face's crop and returns the colour at its centre.
func cropColour(t *testing.T, h *harness, faceID int64) color.RGBA {
	t.Helper()
	resp := h.do(t, "GET", "/api/faces/"+itoa(faceID)+"/thumb", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumb status = %d", resp.StatusCode)
	}
	img, err := jpeg.Decode(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	r, g, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}
}

func isBlue(c color.RGBA) bool { return c.B > 150 && c.R < 100 }
func isRed(c color.RGBA) bool  { return c.R > 150 && c.B < 100 }

// A HEIC nothing here decodes: its face was found in the display copy and is
// cut from it, without the file being opened as a picture at all.
func TestAFaceFromTheDisplayCopyIsCutFromIt(t *testing.T) {
	h := newHarness(t)
	id := facePhoto(t, h, "IMG_0706.HEIC", []byte("not a picture Go can read"), store.FrameDisplay)
	if c := cropColour(t, h, id); !isBlue(c) {
		t.Errorf("crop centre = %v, want the display copy's blue", c)
	}
}

// And a face found in the file is still cut from the file.
func TestAFaceFromTheFileIsCutFromTheFile(t *testing.T) {
	h := newHarness(t)
	id := facePhoto(t, h, "upright.jpg", solidJPEG(t, fileRed), "")
	if c := cropColour(t, h, id); !isRed(c) {
		t.Errorf("crop centre = %v, want the file's red", c)
	}
}
