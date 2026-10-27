package comic

import (
	"archive/zip"
	"bytes"
	"testing"
)

func archive(t *testing.T, names ...string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x" + n))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func names(fs []*zip.File) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

func TestPagesAreTheImagesInReadingOrder(t *testing.T) {
	zr := archive(t,
		"page10.jpg", "page2.jpg", "page1.JPG", "ComicInfo.xml", "notes.txt",
		"__MACOSX/._page1.jpg", ".DS_Store", "._page3.jpg", "folder/",
		"page3.png", "page4.webp",
	)
	pages, err := Pages(zr)
	if err != nil {
		t.Fatal(err)
	}
	got := names(pages)
	want := []string{"page1.JPG", "page2.jpg", "page3.png", "page4.webp", "page10.jpg"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestNaturalOrderAcrossFoldersAndPadding(t *testing.T) {
	zr := archive(t, "b/01.jpg", "a/10.jpg", "a/9.jpg", "a/009b.jpg", "cover.jpg")
	pages, _ := Pages(zr)
	got := names(pages)
	want := []string{"a/9.jpg", "a/009b.jpg", "a/10.jpg", "b/01.jpg", "cover.jpg"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestAnArchiveWithoutPagesSaysSo(t *testing.T) {
	if _, err := Pages(archive(t, "readme.txt")); err != ErrNoPages {
		t.Fatalf("err = %v", err)
	}
}

func TestReadPage(t *testing.T) {
	pages, _ := Pages(archive(t, "1.jpg"))
	b, err := ReadPage(pages[0])
	if err != nil || string(b) != "x1.jpg" {
		t.Fatalf("%q %v", b, err)
	}
	if ContentType("a/B.JPEG") != "image/jpeg" || ContentType("x.txt") != "" {
		t.Fatal("content types")
	}
}
