package webview2

import "image"

/*
 * The pure half of thumbnail.go: fitting, scaling and layering captures.
 * Nothing here touches a window, so it is tested on its own.
 */

// fitWithin is the largest size of w×h's shape that fits inside maxW×maxH,
// never larger than w×h and never zero.
func fitWithin(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 || maxW <= 0 || maxH <= 0 {
		return 1, 1
	}
	if w <= maxW && h <= maxH {
		return w, h
	}
	// Compare w/h against maxW/maxH without floating point.
	if w*maxH > h*maxW {
		th := h * maxW / w
		return maxW, max(th, 1)
	}
	tw := w * maxH / h
	return max(tw, 1), maxH
}

// scaleBox shrinks img to w×h by averaging each destination pixel's box of
// source pixels: a thumbnail of a page of small text, scaled by picking
// single pixels, is a pattern of noise.
func scaleBox(img *image.RGBA, w, h int) *image.RGBA {
	sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 || sw <= 0 || sh <= 0 {
		return out
	}
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var r, g, b, n int
			for sy := y0; sy < y1 && sy < sh; sy++ {
				row := sy * img.Stride
				for sx := x0; sx < x1 && sx < sw; sx++ {
					i := row + sx*4
					r += int(img.Pix[i])
					g += int(img.Pix[i+1])
					b += int(img.Pix[i+2])
					n++
				}
			}
			o := y*out.Stride + x*4
			if n > 0 {
				out.Pix[o], out.Pix[o+1], out.Pix[o+2] = byte(r/n), byte(g/n), byte(b/n)
			}
			out.Pix[o+3] = 255
		}
	}
	return out
}

// drawAt copies src onto dst with its top-left at (x, y), clipped to dst.
func drawAt(dst, src *image.RGBA, x, y int) {
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for sy := 0; sy < sh; sy++ {
		dy := y + sy
		if dy < 0 || dy >= dh {
			continue
		}
		for sx := 0; sx < sw; sx++ {
			dx := x + sx
			if dx < 0 || dx >= dw {
				continue
			}
			copy(dst.Pix[dy*dst.Stride+dx*4:dy*dst.Stride+dx*4+4], src.Pix[sy*src.Stride+sx*4:sy*src.Stride+sx*4+4])
		}
	}
}

// toBGRA writes img into dst as opaque BGRA, the layout a 32-bit DIB holds.
func toBGRA(dst []byte, img *image.RGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := y*img.Stride + x*4
			d := (y*w + x) * 4
			dst[d], dst[d+1], dst[d+2], dst[d+3] = img.Pix[s+2], img.Pix[s+1], img.Pix[s], 255
		}
	}
}
