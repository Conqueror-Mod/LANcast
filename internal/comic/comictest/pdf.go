package comictest

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// PDFOptions shapes a test PDF the ways real ones differ.
type PDFOptions struct {
	// Flate stores pages as lossless RGB samples behind a PNG predictor
	// instead of as JPEG.
	Flate bool
	// Compressed puts the non-stream objects in an object stream and the
	// cross-reference in a (predicted) stream, as PDF 1.5 tools do.
	Compressed bool
	// BreakXref writes every cross-reference offset a few bytes wrong.
	BreakXref bool
	// TextPage, from 1, is a page with no picture on it.
	TextPage int
	// Inherit moves page 1's resources onto the page tree's root.
	Inherit bool
	// Logo adds a small second image to every page, beside the page's own.
	Logo bool
	// Halves draws every page as two equal images, side by side.
	Halves bool
	// Encrypt marks the file encrypted.
	Encrypt bool
	// Loop makes the page tree list its own root as a child.
	Loop bool
}

// PDF is a test PDF of n pages, page i's picture filled with red = i, so a
// test can tell the pages apart after decoding. Objects are written in an
// order unrelated to the pages', and the page tree has two levels.
func PDF(t testing.TB, n int, o PDFOptions) []byte {
	t.Helper()
	type obj struct {
		body   []byte // a dictionary, or a stream's dictionary
		stream []byte // nil for a plain object
	}
	objs := map[int]obj{}
	next := 5 // 1 catalogue, 2 root of the tree, 3 and 4 its two halves
	half := (n + 1) / 2
	kids := [2][]int{}
	var page1Res string
	for i := 1; i <= n; i++ {
		pageNum, contentNum := next, next+1
		next += 2
		res := "<< >>"
		content := []byte("BT /F1 12 Tf (words) Tj ET")
		if i != o.TextPage {
			imgNum := next
			next++
			objs[imgNum] = image3(t, i, 40, 60, o.Flate)
			xo := fmt.Sprintf("/Im%d %d 0 R", i, imgNum)
			if o.Logo {
				logo := next
				next++
				objs[logo] = image3(t, 200, 4, 4, false)
				xo += fmt.Sprintf(" /Logo %d 0 R", logo)
			}
			if o.Halves {
				other := next
				next++
				objs[other] = image3(t, i, 40, 60, o.Flate)
				xo += fmt.Sprintf(" /Other %d 0 R", other)
			}
			res = "<< /XObject << " + xo + " >> >>"
			content = []byte("q 612 0 0 792 0 0 cm /Im Do Q")
		}
		objs[contentNum] = obj{body: []byte(fmt.Sprintf("<< /Length %d >>", len(content))), stream: content}
		parent := 3
		if i > half {
			parent = 4
		}
		kids[parent-3] = append(kids[parent-3], pageNum)
		resEntry := " /Resources " + res
		if o.Inherit && i == 1 {
			page1Res = res
			resEntry = ""
		}
		objs[pageNum] = obj{body: []byte(fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792]%s /Contents %d 0 R >>",
			parent, resEntry, contentNum))}
	}
	refs := func(nums []int) string {
		var b bytes.Buffer
		for _, k := range nums {
			fmt.Fprintf(&b, "%d 0 R ", k)
		}
		return b.String()
	}
	rootKids := "3 0 R 4 0 R"
	if o.Loop {
		rootKids += " 2 0 R"
	}
	rootRes := ""
	if o.Inherit {
		// Inherited down both levels: set on the root, read by page 1.
		rootRes = " /Resources " + page1Res
	}
	objs[1] = obj{body: []byte("<< /Type /Catalog /Pages 2 0 R >>")}
	objs[2] = obj{body: []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d%s >>", rootKids, n, rootRes))}
	objs[3] = obj{body: []byte(fmt.Sprintf("<< /Type /Pages /Parent 2 0 R /Kids [%s] /Count %d >>", refs(kids[0]), len(kids[0])))}
	objs[4] = obj{body: []byte(fmt.Sprintf("<< /Type /Pages /Parent 2 0 R /Kids [%s] /Count %d >>", refs(kids[1]), len(kids[1])))}

	var nums []int
	for k := range objs {
		nums = append(nums, k)
	}
	// Written last number first, so file order is not page order.
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))

	var f bytes.Buffer
	f.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := map[int]int{}
	inStm := map[int]int{} // object → index in the object stream
	stmNum := next
	if o.Compressed {
		next++
		var heads, bodies bytes.Buffer
		idx := 0
		for _, k := range nums {
			if objs[k].stream != nil {
				continue
			}
			fmt.Fprintf(&heads, "%d %d ", k, bodies.Len())
			bodies.Write(objs[k].body)
			bodies.WriteByte('\n')
			inStm[k] = idx
			idx++
		}
		data := zlibBytes(t, append(heads.Bytes(), bodies.Bytes()...))
		objs[stmNum] = obj{
			body:   []byte(fmt.Sprintf("<< /Type /ObjStm /N %d /First %d /Filter /FlateDecode /Length %d >>", idx, heads.Len(), len(data))),
			stream: data,
		}
		nums = append([]int{stmNum}, nums...)
	}
	for _, k := range nums {
		if _, packed := inStm[k]; packed {
			continue
		}
		offsets[k] = f.Len()
		fmt.Fprintf(&f, "%d 0 obj\n", k)
		f.Write(objs[k].body)
		if s := objs[k].stream; s != nil {
			f.WriteString("\nstream\n")
			f.Write(s)
			f.WriteString("\nendstream")
		}
		f.WriteString("\nendobj\n")
	}
	size := next + 1
	encrypt := ""
	if o.Encrypt {
		encrypt = " /Encrypt << /Filter /Standard /V 2 /R 3 >>"
	}
	skew := 0
	if o.BreakXref {
		skew = 3
	}
	start := f.Len()
	if o.Compressed {
		xrefNum := next
		// Rows of [type:1][field2:4][field3:2], PNG "Up" predicted.
		const row = 7
		var raw, prev []byte
		prev = make([]byte, row)
		for k := 0; k < size; k++ {
			cur := make([]byte, row)
			switch {
			case k == xrefNum:
				cur[0] = 1
				put(cur[1:5], start)
			case offsets[k] != 0:
				cur[0] = 1
				put(cur[1:5], offsets[k]+skew)
			default:
				if idx, ok := inStm[k]; ok {
					cur[0] = 2
					put(cur[1:5], stmNum)
					cur[5], cur[6] = byte(idx>>8), byte(idx)
				}
			}
			raw = append(raw, 2)
			for j := range cur {
				raw = append(raw, cur[j]-prev[j])
			}
			prev = cur
		}
		data := zlibBytes(t, raw)
		fmt.Fprintf(&f, "%d 0 obj\n<< /Type /XRef /Size %d /W [1 4 2] /Root 1 0 R%s /Filter /FlateDecode "+
			"/DecodeParms << /Predictor 12 /Columns %d >> /Length %d >>\nstream\n", xrefNum, size, encrypt, row, len(data))
		f.Write(data)
		f.WriteString("\nendstream\nendobj\n")
	} else {
		fmt.Fprintf(&f, "xref\n0 %d\n0000000000 65535 f \n", size)
		for k := 1; k < size; k++ {
			if off, ok := offsets[k]; ok {
				fmt.Fprintf(&f, "%010d 00000 n \n", off+skew)
			} else {
				f.WriteString("0000000000 00000 f \n")
			}
		}
		fmt.Fprintf(&f, "trailer\n<< /Size %d /Root 1 0 R%s >>\n", size, encrypt)
	}
	fmt.Fprintf(&f, "startxref\n%d\n%%%%EOF\n", start)
	return f.Bytes()
}

// WritePDF writes PDF(t, n, o) to path.
func WritePDF(t testing.TB, path string, n int, o PDFOptions) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, PDF(t, n, o), 0o644); err != nil {
		t.Fatal(err)
	}
}

func put(b []byte, v int) {
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
}

func zlibBytes(t testing.TB, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(b)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// image3 is an image XObject of w×h filled with red = r: JPEG, or RGB samples
// compressed with the PNG "Up" predictor applied.
func image3(t testing.TB, r, w, h int, flate bool) struct {
	body   []byte
	stream []byte
} {
	t.Helper()
	type out = struct {
		body   []byte
		stream []byte
	}
	if !flate {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, color.RGBA{R: uint8(r), G: 80, B: 120, A: 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		return out{
			body: []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
				"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", w, h, buf.Len())),
			stream: buf.Bytes(),
		}
	}
	var raw []byte
	prev := make([]byte, w*3)
	for y := 0; y < h; y++ {
		cur := make([]byte, w*3)
		for x := 0; x < w; x++ {
			cur[x*3], cur[x*3+1], cur[x*3+2] = byte(r), 80, byte(120+y) // a gradient, so Up has work to do
		}
		raw = append(raw, 2)
		for j := range cur {
			raw = append(raw, cur[j]-prev[j])
		}
		prev = cur
	}
	data := zlibBytes(t, raw)
	return out{
		body: []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode "+
			"/DecodeParms << /Predictor 15 /Colors 3 /Columns %d >> /Length %d >>", w, h, w, len(data))),
		stream: data,
	}
}
