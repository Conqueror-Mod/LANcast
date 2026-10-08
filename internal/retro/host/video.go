// Package host runs a libretro core (ADR 0073): the loop, its pacing, the
// picture, the sound, the controller and the saves.
//
// Everything here is written against libretro.Core and small sink interfaces,
// so it is tested with a fake core and fake sinks on any platform. The
// Windows pieces — a GDI blit into the video window, waveOut, XInput — live
// beside it in their own files and do nothing but move bytes.
package host

import (
	"encoding/binary"

	"lancast/internal/retro/libretro"
)

/*
 * ToBGRA converts one frame to 32-bit BGRA rows with no padding — what a
 * Windows DIB section wants — growing dst if it is too small, and returns it.
 *
 * XRGB8888 is already that layout in memory (0x00RRGGBB little-endian is
 * B, G, R, X), so it is a row copy that drops the pitch. The 16-bit formats
 * expand each channel by replicating its high bits into the low ones, so
 * full intensity maps to 255 rather than 248 — the difference between white
 * and a faintly grey white.
 */
func ToBGRA(f libretro.Frame, dst []byte) []byte {
	w, h := int(f.Width), int(f.Height)
	need := w * h * 4
	if cap(dst) < need {
		dst = make([]byte, need)
	}
	dst = dst[:need]
	if f.Data == nil || w == 0 || h == 0 {
		return dst
	}
	pitch := int(f.Pitch)
	for y := 0; y < h; y++ {
		row := f.Data[y*pitch:]
		out := dst[y*w*4 : (y+1)*w*4]
		switch f.Format {
		case libretro.FormatXRGB8888:
			copy(out, row[:w*4])
			for x := 0; x < w; x++ {
				out[x*4+3] = 0xff
			}
		case libretro.FormatRGB565:
			for x := 0; x < w; x++ {
				p := binary.LittleEndian.Uint16(row[x*2:])
				r, g, b := byte(p>>11)&0x1f, byte(p>>5)&0x3f, byte(p)&0x1f
				out[x*4+0] = b<<3 | b>>2
				out[x*4+1] = g<<2 | g>>4
				out[x*4+2] = r<<3 | r>>2
				out[x*4+3] = 0xff
			}
		default: // 0RGB1555
			for x := 0; x < w; x++ {
				p := binary.LittleEndian.Uint16(row[x*2:])
				r, g, b := byte(p>>10)&0x1f, byte(p>>5)&0x1f, byte(p)&0x1f
				out[x*4+0] = b<<3 | b>>2
				out[x*4+1] = g<<3 | g>>2
				out[x*4+2] = r<<3 | r>>2
				out[x*4+3] = 0xff
			}
		}
	}
	return dst
}

// Rect is where the picture goes in the window.
type Rect struct{ X, Y, W, H int }

/*
 * Layout places a frame in a window: as large as fits, at its display aspect,
 * centred, with black bars.
 *
 * Integer scaling when it fits. Pixel art scaled by 3.4 has columns of two
 * widths, and the eye finds them; scaled by 3 every source pixel is the same
 * size. So the height is the largest whole multiple of the source height
 * that fits, and the width follows from the aspect. If not even one whole
 * multiple fits — a window smaller than the console's own resolution — the
 * picture is fitted instead, because a cropped game is worse than a soft one.
 */
func Layout(winW, winH, frameW, frameH int, aspect float64) Rect {
	if winW <= 0 || winH <= 0 || frameW <= 0 || frameH <= 0 {
		return Rect{}
	}
	if aspect <= 0 {
		aspect = float64(frameW) / float64(frameH)
	}
	for k := winH / frameH; k >= 1; k-- {
		h := k * frameH
		w := int(float64(h)*aspect + 0.5)
		if w <= winW {
			return Rect{X: (winW - w) / 2, Y: (winH - h) / 2, W: w, H: h}
		}
	}
	// Fit: limited by whichever side runs out first.
	w, h := winW, int(float64(winW)/aspect+0.5)
	if h > winH {
		h = winH
		w = int(float64(winH)*aspect + 0.5)
	}
	return Rect{X: (winW - w) / 2, Y: (winH - h) / 2, W: w, H: h}
}
