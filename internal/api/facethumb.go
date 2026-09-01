package api

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"strconv"

	"golang.org/x/image/draw"
)

/*
 * A face, cropped out of the photograph it was found in (ADR 0052).
 *
 * Cut on demand rather than stored. A crop is derivable from a photograph and a
 * box, and writing tens of thousands of small JPEGs into the artwork cache
 * would double a picture library's disk cost to save an operation that takes
 * milliseconds — and would then need invalidating every time a re-cluster
 * changed which face represents a group.
 *
 * This is the one endpoint in the feature that opens a media file, so it is the
 * one that has to be careful: the database is trusted, and this is the boundary
 * where a bad row would become arbitrary file access.
 */

// faceThumbSize is the long edge of the crop. Big enough to recognise somebody
// at a glance in a grid, small enough that a page of sixty is not a download.
const faceThumbSize = 160

func (s *Server) faceThumb(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid face id")
		return
	}

	// A face in a marked folder reports as absent rather than forbidden: a 403
	// would confirm that a face exists at that id, inside a folder somebody
	// marked precisely so its contents could not be enumerated.
	face, item, err := s.st.GetFace(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get face", "no such face") {
		return
	}

	/*
	 * Containment, which is not optional here.
	 *
	 * itemFilePath resolves the row's path against the location it was scanned
	 * under and refuses anything that escapes it. Every handler that turns a
	 * database row into a filesystem path goes through it, because the row is
	 * trusted and the filesystem is not.
	 */
	path, err := s.itemFilePath(r, item)
	if err != nil {
		s.log.Error("face thumb containment check failed",
			"face", id, "item", item.ID, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such face")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"the photograph is missing from disk")
		return
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"the photograph could not be read")
		return
	}

	out := cropFace(src, face.X, face.Y, face.W, face.H)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 82}); err != nil {
		s.writeInternal(w, err, "encode face thumb")
		return
	}

	/*
	 * Cached hard, and safely.
	 *
	 * A face id identifies one box in one photograph. Re-running detection
	 * *replaces* the rows rather than editing them, so an id whose crop could
	 * change does not survive to be asked about again — which is what makes a
	 * long max-age honest here rather than a bet.
	 */
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", `"face-`+strconv.FormatInt(face.ID, 10)+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

/*
 * cropFace cuts a square around the box and scales it.
 *
 * Square, and wider than the detector's box, because a detection is tight to
 * the face and a portrait cropped at the hairline reads as a mugshot. The extra
 * margin is what makes a grid of these look like people rather than evidence.
 *
 * Clamped to the image rather than assumed to fit: a face at the very edge of a
 * photograph has a box that runs off it, and cropping outside the bounds is
 * either a panic or a black band depending on which library gets there first.
 */
func cropFace(src image.Image, x, y, wide, high int) image.Image {
	b := src.Bounds()
	side := wide
	if high > side {
		side = high
	}
	side = side * 13 / 10
	if side <= 0 {
		side = 1
	}

	cx := x + wide/2
	cy := y + high/2
	x0 := cx - side/2
	y0 := cy - side/2

	// Slide the window back inside the photograph before clamping its size, so
	// a face near an edge stays centred on the face rather than on the corner.
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x0+side > b.Max.X {
		x0 = b.Max.X - side
	}
	if y0+side > b.Max.Y {
		y0 = b.Max.Y - side
	}
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x0+side > b.Max.X {
		side = b.Max.X - x0
	}
	if y0+side > b.Max.Y {
		side = b.Max.Y - y0
	}
	if side <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	out := image.NewRGBA(image.Rect(0, 0, faceThumbSize, faceThumbSize))
	draw.CatmullRom.Scale(out, out.Bounds(), src,
		image.Rect(x0, y0, x0+side, y0+side), draw.Src, nil)
	return out
}
