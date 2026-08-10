package photo

import "image"

// Orient applies an EXIF orientation to a decoded image.
//
// Applied here, once, when derivatives are generated — never sent to a client.
// Handing the raw value out would make every consumer responsible for rotating
// correctly, and the first one that forgot would show a phone photo on its side
// in a way that reads as LANcast's bug rather than the camera's convention.
//
// The eight values are a rotation and an optional mirror. Values 2, 4, 5 and 7
// are mirrored, which real cameras do produce (front-facing phone cameras
// especially), so they are handled rather than approximated by the nearest
// rotation.
func Orient(img image.Image, orientation int) image.Image {
	switch orientation {
	case 0, 1:
		return img
	case 2:
		return remap(img, false, func(x, y, w, _ int) (int, int) { return w - 1 - x, y })
	case 3:
		return remap(img, false, func(x, y, w, h int) (int, int) { return w - 1 - x, h - 1 - y })
	case 4:
		return remap(img, false, func(x, y, _, h int) (int, int) { return x, h - 1 - y })
	case 5:
		return remap(img, true, func(x, y, _, _ int) (int, int) { return y, x })
	case 6:
		return remap(img, true, func(x, y, _, h int) (int, int) { return h - 1 - y, x })
	case 7:
		return remap(img, true, func(x, y, w, h int) (int, int) { return h - 1 - y, w - 1 - x })
	case 8:
		return remap(img, true, func(x, y, w, _ int) (int, int) { return y, w - 1 - x })
	default:
		// An orientation outside 1..8 is a malformed file, not an instruction.
		// Returning the image unchanged is the one option that cannot make it
		// worse.
		return img
	}
}

// remap builds the transformed image pixel by pixel. swap says the output's
// dimensions are the input's, transposed.
func remap(src image.Image, swap bool, at func(x, y, w, h int) (int, int)) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if swap {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := at(x, y, w, h)
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
