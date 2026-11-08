package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

// The spotlight's tone (ADR 0118 §5): worked out from a cached picture, and
// cached as immutably as the picture, because the answer cannot change.
func TestArtworkToneIsServedImmutably(t *testing.T) {
	h := newHarness(t)
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{220, 130, 40, 255}) // desert orange
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	hash, _, _, _, err := h.art.Put(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "GET", "/api/artwork/"+hash+"/tone", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("tone = %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	var tone struct{ Violet, Blue, Teal, Ember, Light float64 }
	if err := json.NewDecoder(resp.Body).Decode(&tone); err != nil {
		t.Fatal(err)
	}
	if tone.Ember < 0.9 {
		t.Errorf("tone = %+v, want an orange picture to lean on the ember", tone)
	}

	missing := h.do(t, "GET", "/api/artwork/"+strings.Repeat("0", 64)+"/tone", nil)
	missing.Body.Close()
	if missing.StatusCode != 404 {
		t.Errorf("unknown hash = %d, want 404", missing.StatusCode)
	}
}
