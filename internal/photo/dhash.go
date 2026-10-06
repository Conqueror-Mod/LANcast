package photo

import "image"

/*
 * DHash is the 64-bit difference hash of img: a 9x8 greyscale reduction, one
 * bit per neighbouring pair, set where the left cell is brighter.
 *
 * For near copies (ADR 0075, 2026-10-06 amendment). Computed on the upright
 * display copy, the picture the measurement hashed, so its distances mean what
 * the measurement found: a resized or re-saved copy within 8, a crop of the
 * same picture 14 or more. The algorithm is the measurement's, unchanged, for
 * the same reason: a threshold is only valid for the hash it was measured on.
 *
 * Each cell is a box average, sampled on a grid of at most 8x8 points per cell
 * so a 1600-pixel picture costs a few thousand reads rather than millions.
 */
func DHash(img image.Image) uint64 {
	b := img.Bounds()
	if b.Dx() < 9 || b.Dy() < 8 {
		return 0
	}
	var g [8][9]float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 9; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/9, b.Min.X+(x+1)*b.Dx()/9
			y0, y1 := b.Min.Y+y*b.Dy()/8, b.Min.Y+(y+1)*b.Dy()/8
			step := max(1, (x1-x0)/8)
			var s float64
			var n int
			for yy := y0; yy < y1; yy += step {
				for xx := x0; xx < x1; xx += step {
					r, gg, bb, _ := img.At(xx, yy).RGBA()
					s += 0.299*float64(r) + 0.587*float64(gg) + 0.114*float64(bb)
					n++
				}
			}
			if n > 0 {
				g[y][x] = s / float64(n)
			}
		}
	}
	var h uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			h <<= 1
			if g[y][x] > g[y][x+1] {
				h |= 1
			}
		}
	}
	return h
}
