// Package pdf finds the picture on each page of a PDF comic (ADR 0102).
//
// It is not a PDF renderer and does not try to be. A comic sold or scanned as
// a PDF is, page for page, one full-page image with nothing drawn over it, so
// a page can be served as that image — a JPEG byte for byte as the file holds
// it, or a lossless one made into a PNG — without reading a single drawing
// operator. A page that is anything else (text, vector art, several images
// tiled) is reported as unreadable rather than shown wrong.
package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
)

// Limits. A comic has tens of pages; a page image is a few megabytes.
const (
	MaxPages     = 5000
	MaxPageBytes = 64 << 20
	// maxPixels bounds a lossless page, which is decoded into memory to be
	// made into a PNG. A comic scanned at 600 dpi is about 24 megapixels.
	maxPixels = 36_000_000
)

// ErrNoPages is a file with no pages.
var ErrNoPages = errors.New("pdf: no pages")

// Format is how a page's image is held in the file.
type Format int

const (
	JPEG  Format = iota + 1 // served as it is
	Flate                   // raw samples, made into a PNG
)

// Page is where one page's image is in the file. Err is set, and nothing
// else, on a page that is not a single image.
type Page struct {
	Format Format
	Off    int64
	Len    int64
	// For Flate: the samples' shape, and the predictor that was applied.
	W, H, Colors int
	Predictor    int
	Err          string
}

// ContentType is what a page is served as.
func (p Page) ContentType() string {
	switch p.Format {
	case JPEG:
		return "image/jpeg"
	case Flate:
		return "image/png"
	}
	return ""
}

// Index finds every page's image. It reads the file's index and its page
// objects, and none of the image data.
func Index(r io.ReaderAt, size int64) ([]Page, error) {
	d, err := open(r, size)
	if err != nil {
		return nil, err
	}
	root := d.dict(d.trailer["Root"])
	if root == nil {
		return nil, errors.New("pdf: no catalogue")
	}
	var pages []Page
	seen := map[Ref]bool{}
	var walk func(node any, res any, depth int) error
	walk = func(node any, res any, depth int) error {
		if depth > 64 {
			return errors.New("pdf: page tree nested too deeply")
		}
		if ref, ok := node.(Ref); ok {
			if seen[ref] {
				return nil // a loop, or a page listed twice
			}
			seen[ref] = true
		}
		n := d.dict(node)
		if n == nil {
			return nil
		}
		if r := n["Resources"]; r != nil {
			res = r // inherited by every page beneath
		}
		if kids, ok := d.resolve(n["Kids"]).(Array); ok && n["Type"] != Name("Page") {
			for _, k := range kids {
				if err := walk(k, res, depth+1); err != nil {
					return err
				}
				if len(pages) > MaxPages {
					return fmt.Errorf("pdf: more than %d pages", MaxPages)
				}
			}
			return nil
		}
		pages = append(pages, d.page(res))
		return nil
	}
	if err := walk(root["Pages"], nil, 0); err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, ErrNoPages
	}
	return pages, nil
}

type candidate struct {
	s    *Stream
	area int64
}

// page finds the one image a page shows. A small mark beside it — a
// publisher's logo, a scanner's watermark — is ignored when the main image
// is nearly all of the page's picture area; anything less is refused.
func (d *doc) page(res any) Page {
	var imgs []candidate
	var total int64
	var collect func(res any, depth int)
	collect = func(res any, depth int) {
		xo := d.dict(d.dict(res)["XObject"])
		for _, v := range xo {
			s, ok := d.resolve(v).(*Stream)
			if !ok {
				continue
			}
			switch s.Dict["Subtype"] {
			case Name("Image"):
				if m, _ := d.resolve(s.Dict["ImageMask"]).(bool); m {
					continue
				}
				w, _ := d.resolve(s.Dict["Width"]).(int64)
				h, _ := d.resolve(s.Dict["Height"]).(int64)
				if w <= 0 || h <= 0 {
					continue
				}
				imgs = append(imgs, candidate{s, w * h})
				total += w * h
			case Name("Form"):
				// Some tools wrap the page's image in a form; look one level in.
				if depth < 2 {
					collect(s.Dict["Resources"], depth+1)
				}
			}
		}
	}
	collect(res, 0)
	if len(imgs) == 0 {
		return Page{Err: "this page has no picture: it is text or drawing, which is not read"}
	}
	best := imgs[0]
	for _, c := range imgs[1:] {
		if c.area > best.area {
			best = c
		}
	}
	if best.area*100 < total*95 {
		return Page{Err: "this page is made of several pictures, which is not read"}
	}
	return d.imagePage(best.s)
}

func (d *doc) imagePage(s *Stream) Page {
	fs := d.filters(s)
	w, _ := d.resolve(s.Dict["Width"]).(int64)
	h, _ := d.resolve(s.Dict["Height"]).(int64)
	if s.Len > MaxPageBytes {
		return Page{Err: "this page's picture is too large"}
	}
	switch {
	case len(fs) == 1 && fs[0] == "DCTDecode":
		return Page{Format: JPEG, Off: s.Off, Len: s.Len, W: int(w), H: int(h)}
	case len(fs) == 1 && fs[0] == "JPXDecode":
		return Page{Err: "this page is JPEG 2000, which browsers cannot show"}
	case len(fs) == 1 && fs[0] == "FlateDecode":
	default:
		return Page{Err: fmt.Sprintf("this page's picture is stored as %v, which is not read", fs)}
	}
	if bpc, _ := d.resolve(s.Dict["BitsPerComponent"]).(int64); bpc != 8 {
		return Page{Err: "this page's picture is not 8 bits a sample, which is not read"}
	}
	if d.resolve(s.Dict["Decode"]) != nil || d.resolve(s.Dict["SMask"]) != nil {
		return Page{Err: "this page's picture is masked or remapped, which is not read"}
	}
	colors := d.components(s.Dict["ColorSpace"])
	if colors != 1 && colors != 3 {
		return Page{Err: "this page's picture is in a colour space that is not read"}
	}
	if w*h > maxPixels {
		return Page{Err: "this page's picture is too large"}
	}
	pred := int64(0)
	parms := d.dict(s.Dict["DecodeParms"])
	if a, ok := d.resolve(s.Dict["DecodeParms"]).(Array); ok && len(a) == 1 {
		parms = d.dict(a[0])
	}
	if p, ok := d.resolve(parms["Predictor"]).(int64); ok {
		pred = p
		cols, _ := d.resolve(parms["Columns"]).(int64)
		pc, _ := d.resolve(parms["Colors"]).(int64)
		if pc == 0 {
			pc = 1
		}
		if pred > 1 && (pred < 10 || cols != w || int(pc) != colors) {
			return Page{Err: "this page's picture uses a predictor that is not read"}
		}
	}
	return Page{Format: Flate, Off: s.Off, Len: s.Len, W: int(w), H: int(h), Colors: colors, Predictor: int(pred)}
}

// components is how many samples a pixel has in a colour space this reads:
// grey or RGB, named or as an ICC profile. Anything else is 0.
func (d *doc) components(cs any) int {
	switch v := d.resolve(cs).(type) {
	case Name:
		switch v {
		case "DeviceGray", "CalGray":
			return 1
		case "DeviceRGB", "CalRGB":
			return 3
		}
	case Array:
		if len(v) == 0 {
			return 0
		}
		switch d.resolve(v[0]) {
		case Name("ICCBased"):
			if len(v) > 1 {
				if s, ok := d.resolve(v[1]).(*Stream); ok {
					n, _ := d.resolve(s.Dict["N"]).(int64)
					return int(n)
				}
			}
		case Name("CalGray"):
			return 1
		case Name("CalRGB"):
			return 3
		}
	}
	return 0
}

// ReadPage returns a page's image, ready to serve as p.ContentType().
func ReadPage(r io.ReaderAt, p Page) ([]byte, error) {
	if p.Err != "" {
		return nil, errors.New(p.Err)
	}
	if p.Len <= 0 || p.Len > MaxPageBytes {
		return nil, errors.New("pdf: page is empty or too large")
	}
	raw := make([]byte, p.Len)
	if _, err := r.ReadAt(raw, p.Off); err != nil && !(errors.Is(err, io.EOF)) {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	if p.Format == JPEG {
		return raw, nil
	}
	rowLen := p.W * p.Colors
	want := int64(rowLen) * int64(p.H)
	limit := want
	if p.Predictor >= 10 {
		limit += int64(p.H)
	}
	data, err := inflate(raw, limit)
	if err != nil {
		return nil, err
	}
	var parms Dict
	if p.Predictor >= 10 {
		parms = Dict{"Predictor": int64(p.Predictor), "Colors": int64(p.Colors), "Columns": int64(p.W)}
	}
	if data, err = unpredict(data, parms); err != nil {
		return nil, err
	}
	// A stream that holds fewer samples than it claims is refused rather than
	// padded: padding is how a file of a few bytes would have this allocate
	// for a page of 36 megapixels.
	if int64(len(data)) < want {
		return nil, errors.New("pdf: a page's picture is shorter than its size says")
	}
	var img image.Image
	rect := image.Rect(0, 0, p.W, p.H)
	if p.Colors == 1 {
		img = &image.Gray{Pix: data[:want], Stride: rowLen, Rect: rect}
	} else {
		rgba := image.NewNRGBA(rect)
		for i, j := 0, 0; i < int(want); i, j = i+3, j+4 {
			rgba.Pix[j], rgba.Pix[j+1], rgba.Pix[j+2], rgba.Pix[j+3] = data[i], data[i+1], data[i+2], 0xff
		}
		img = rgba
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
