package host

import "fmt"

/*
 * How the picture is drawn (the game menu's Display, 2026-10-18).
 *
 * The zero value is how it always looked: crisp pixels, the largest whole
 * multiple of the game's height that fits, in the shape the core asked for,
 * no scanlines. Every field is a departure from that, so a person who never
 * opens the menu sees no change.
 */
type Display struct {
	// Smooth blends pixels when scaling instead of keeping them square-edged.
	Smooth bool `json:"smooth"`
	// Fill scales to fill the screen rather than to a whole multiple, which
	// leaves wider black bars but every pixel the same size.
	Fill bool `json:"fill"`
	// Aspect is "" (what the core says), "4:3", "square" (each pixel square,
	// the frame's own width over height) or "stretch" (the whole window).
	Aspect string `json:"aspect"`
	// Scanlines darkens every other line of a doubled picture, the look of a
	// CRT. Only on the ordinary drawing path; a game drawn by OpenGL (N64)
	// shows without them.
	Scanlines bool `json:"scanlines"`
}

// Valid reports whether d holds only values the menu offers.
func (d Display) Valid() error {
	switch d.Aspect {
	case "", "4:3", "square", "stretch":
		return nil
	}
	return fmt.Errorf("no aspect %q", d.Aspect)
}

// aspectFor is the shape to draw a frameW×frameH picture in, given the
// shape the core asked for.
func (d Display) aspectFor(core float64, frameW, frameH int) float64 {
	switch d.Aspect {
	case "4:3":
		return 4.0 / 3
	case "square":
		if frameH > 0 {
			return float64(frameW) / float64(frameH)
		}
	}
	return core
}

// LayoutFor places a frame in a window under these settings.
func LayoutFor(winW, winH, frameW, frameH int, aspect float64, d Display) Rect {
	if winW <= 0 || winH <= 0 || frameW <= 0 || frameH <= 0 {
		return Rect{}
	}
	if d.Aspect == "stretch" {
		return Rect{W: winW, H: winH}
	}
	if !d.Fill {
		return Layout(winW, winH, frameW, frameH, aspect)
	}
	if aspect <= 0 {
		aspect = float64(frameW) / float64(frameH)
	}
	w, h := winW, int(float64(winW)/aspect+0.5)
	if h > winH {
		h = winH
		w = int(float64(winH)*aspect + 0.5)
	}
	return Rect{X: (winW - w) / 2, Y: (winH - h) / 2, W: w, H: h}
}

// scanlineLevel is how bright the in-between lines are: dark enough to read
// as lines, light enough that the picture does not halve in brightness.
const scanlineLevel = 140 // of 256

/*
 * Scanlines doubles a BGRA frame's height, each source row followed by a
 * darkened copy of itself, into dst (grown if needed). Drawn at the doubled
 * height, the lines line up with the picture's own rows at every scale.
 */
func Scanlines(src []byte, width, height int, dst []byte) []byte {
	row := width * 4
	need := row * height * 2
	if cap(dst) < need {
		dst = make([]byte, need)
	}
	dst = dst[:need]
	for y := 0; y < height; y++ {
		in := src[y*row : (y+1)*row]
		lit := dst[2*y*row : (2*y+1)*row]
		dim := dst[(2*y+1)*row : (2*y+2)*row]
		copy(lit, in)
		for i := 0; i < row; i += 4 {
			dim[i] = byte(int(in[i]) * scanlineLevel >> 8)
			dim[i+1] = byte(int(in[i+1]) * scanlineLevel >> 8)
			dim[i+2] = byte(int(in[i+2]) * scanlineLevel >> 8)
			dim[i+3] = 0xff
		}
	}
	return dst
}
