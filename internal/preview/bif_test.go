package preview

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// sheetOf paints each tile of a sheet a grey that names its frame, so a
// frame cut from the wrong place shows up as the wrong shade.
func sheetOf(m *Manifest, n int) image.Image {
	img := image.NewGray(image.Rect(0, 0, m.Columns*m.Width, m.Rows*m.Height))
	for k := range m.Columns * m.Rows {
		x, y := (k%m.Columns)*m.Width, (k/m.Columns)*m.Height
		shade := uint8((n*m.Columns*m.Rows + k) * 10 % 250)
		for yy := y; yy < y+m.Height; yy++ {
			for xx := x; xx < x+m.Width; xx++ {
				img.SetGray(xx, yy, color.Gray{shade})
			}
		}
	}
	return img
}

type bifEntry struct{ stamp, off uint32 }

func readBIF(t *testing.T, raw []byte) (mult uint32, idx []bifEntry) {
	t.Helper()
	if !bytes.Equal(raw[:8], bifMagic) {
		t.Fatalf("magic %x", raw[:8])
	}
	if v := binary.LittleEndian.Uint32(raw[8:]); v != 0 {
		t.Fatalf("version %d", v)
	}
	count := binary.LittleEndian.Uint32(raw[12:])
	mult = binary.LittleEndian.Uint32(raw[16:])
	for i := range count + 1 {
		p := 64 + 8*i
		idx = append(idx, bifEntry{binary.LittleEndian.Uint32(raw[p:]), binary.LittleEndian.Uint32(raw[p+4:])})
	}
	if last := idx[len(idx)-1]; last.stamp != 0xffffffff || int(last.off) != len(raw) {
		t.Fatalf("last entry %+v, file %d bytes", last, len(raw))
	}
	return mult, idx
}

func shadeAt(t *testing.T, raw []byte, e, next bifEntry) uint8 {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(raw[e.off:next.off]))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	g := color.GrayModel.Convert(img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)).(color.Gray)
	return g.Y
}

func near(a, b uint8) bool { return a+3 >= b && b+3 >= a }

func TestABIFIsTheSheetsCutApart(t *testing.T) {
	m := &Manifest{IntervalMS: 10000, Columns: 4, Rows: 3, Width: 32, Height: 18, Sheets: 2}
	var buf bytes.Buffer
	// 205 s: frames at 0, 10 … 200 s, so 21 frames over two sheets of 12;
	// the last sheet's padding past the end is left out.
	if err := WriteBIF(&buf, m, 205_000, 0, func(n int) (image.Image, error) { return sheetOf(m, n), nil }); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	mult, idx := readBIF(t, raw)
	if mult != 1000 || len(idx) != 22 {
		t.Fatalf("multiplier %d, %d entries", mult, len(idx))
	}
	for i := range 21 {
		if idx[i].stamp != uint32(i*10) {
			t.Errorf("frame %d at %d s", i, idx[i].stamp)
		}
		if got, want := shadeAt(t, raw, idx[i], idx[i+1]), uint8(i*10%250); !near(got, want) {
			t.Errorf("frame %d shade %d, want %d", i, got, want)
		}
	}
}

// An HLS conversion started at a resume point counts its clock from there,
// so the BIF must too: frame 0 is the first one at or after the start.
func TestABIFFromAResumePointCountsFromThere(t *testing.T) {
	m := &Manifest{IntervalMS: 10000, Columns: 4, Rows: 3, Width: 32, Height: 18, Sheets: 2}
	var buf bytes.Buffer
	if err := WriteBIF(&buf, m, 205_000, 95_000, func(n int) (image.Image, error) { return sheetOf(m, n), nil }); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	_, idx := readBIF(t, raw)
	// Frames 10 … 20 (100 s … 200 s), stamped 5 s … 105 s from the start.
	if len(idx) != 12 {
		t.Fatalf("%d entries", len(idx))
	}
	if idx[0].stamp != 5 || idx[10].stamp != 105 {
		t.Fatalf("stamps %d … %d", idx[0].stamp, idx[10].stamp)
	}
	if got := shadeAt(t, raw, idx[0], idx[1]); !near(got, 100) {
		t.Fatalf("first frame shade %d, want frame 10's", got)
	}
}

func TestABIFPastTheEndIsEmpty(t *testing.T) {
	m := &Manifest{IntervalMS: 10000, Columns: 4, Rows: 3, Width: 32, Height: 18, Sheets: 1}
	var buf bytes.Buffer
	called := false
	if err := WriteBIF(&buf, m, 60_000, 90_000, func(int) (image.Image, error) { called = true; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, idx := readBIF(t, buf.Bytes()); len(idx) != 1 || called {
		t.Fatalf("%d entries, sheet read %v", len(idx), called)
	}
}

// A two-hour film at the real sheet shape: eight sheets, 720 frames, each
// sheet decoded from JPEG the way the route reads it.
func BenchmarkBIFTwoHours(b *testing.B) {
	m := &Manifest{IntervalMS: IntervalMS, Columns: Columns, Rows: Rows, Width: Width, Height: 136, Sheets: 8}
	var enc bytes.Buffer
	jpeg.Encode(&enc, sheetOf(m, 0), &jpeg.Options{Quality: 80})
	raw := enc.Bytes()
	for b.Loop() {
		var out bytes.Buffer
		if err := WriteBIF(&out, m, 7_200_000, 0, func(int) (image.Image, error) {
			return jpeg.Decode(bytes.NewReader(raw))
		}); err != nil {
			b.Fatal(err)
		}
	}
}
