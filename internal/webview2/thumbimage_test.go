package webview2

import (
	"image"
	"testing"
)

func solid(w, h int, r, g, b byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = r, g, b, 255
	}
	return img
}

func TestFitWithinKeepsTheShape(t *testing.T) {
	cases := []struct{ w, h, mw, mh, ww, wh int }{
		{2575, 1455, 200, 120, 200, 113}, // wide window, limited by width
		{1000, 2000, 200, 120, 60, 120},  // tall, limited by height
		{100, 50, 200, 120, 100, 50},     // already small enough: never enlarged
		{3000, 1, 200, 120, 200, 1},      // never collapses to zero
		{0, 10, 200, 120, 1, 1},          // nothing to fit
	}
	for _, c := range cases {
		if w, h := fitWithin(c.w, c.h, c.mw, c.mh); w != c.ww || h != c.wh {
			t.Errorf("fitWithin(%d,%d into %d,%d) = %d,%d, want %d,%d", c.w, c.h, c.mw, c.mh, w, h, c.ww, c.wh)
		}
	}
}

// Averaging, not picking: a 2×1 black-and-white image shrunk to 1×1 is grey.
func TestScaleBoxAverages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	copy(img.Pix, []byte{0, 0, 0, 255, 255, 255, 255, 255})
	out := scaleBox(img, 1, 1)
	if p := out.Pix[:4]; p[0] != 127 || p[1] != 127 || p[2] != 127 || p[3] != 255 {
		t.Errorf("got %v, want mid grey", p)
	}
	if big := scaleBox(solid(1000, 500, 10, 20, 30), 200, 100); big.Pix[0] != 10 || big.Pix[len(big.Pix)-2] != 30 {
		t.Errorf("a solid colour changed on scaling: %v", big.Pix[:4])
	}
}

// The film's picture lands where its window is over the page, clipped to it.
func TestDrawAtPlacesAndClips(t *testing.T) {
	page := solid(10, 10, 0, 0, 0)
	film := solid(4, 4, 200, 0, 0)
	drawAt(page, film, 8, -2) // hangs off the right and the top
	at := func(x, y int) byte { return page.Pix[y*page.Stride+x*4] }
	if at(8, 0) != 200 || at(9, 1) != 200 {
		t.Error("the visible part of the picture was not drawn")
	}
	if at(7, 0) != 0 || at(8, 2) != 0 {
		t.Error("drawn outside where the picture is")
	}
}

func TestToBGRAIsOpaqueBGRA(t *testing.T) {
	img := solid(1, 1, 10, 20, 30)
	img.Pix[3] = 0 // a capture's alpha says nothing; the DIB is opaque anyway
	dst := make([]byte, 4)
	toBGRA(dst, img)
	if dst[0] != 30 || dst[1] != 20 || dst[2] != 10 || dst[3] != 255 {
		t.Errorf("got %v", dst)
	}
}
