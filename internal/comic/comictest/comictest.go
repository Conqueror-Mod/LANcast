// Package comictest writes real comic archives for tests in other packages.
package comictest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Page is a small PNG whose top-left pixel's red channel is i, so a test can
// tell the pages apart after decoding.
func Page(t testing.TB, i int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: uint8(i), G: 80, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Write makes a .cbz at path with n pages, named so a plain sort would put
// them in the wrong order (p1, p2 … p10), plus a ComicInfo.xml that is not a
// page.
func Write(t testing.TB, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := n; i >= 1; i-- {
		w, err := zw.Create(fmt.Sprintf("p%d.png", i))
		if err != nil {
			t.Fatal(err)
		}
		w.Write(Page(t, i))
	}
	w, _ := zw.Create("ComicInfo.xml")
	w.Write([]byte("<ComicInfo/>"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
