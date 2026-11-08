package artwork

import (
	"fmt"
	"image"
	_ "image/jpeg" // the cache stores every size as JPEG
	_ "image/png"
	"math"
)

/*
 * Tone is how much of each of the field's wisps a picture leans toward
 * (ADR 0118 §5): the hero raises those wisps and lowers the others, so the
 * room takes its light from the film without any colour entering the palette
 * that the field did not already have.
 *
 * The four weights share 1 between them, or are all zero for a picture with no
 * colour to speak of (a black-and-white still), which the client reads as
 * "leave the field as it is". Light is the picture's mean luminance, 0 to 1.
 *
 * A pure function of the image, and the image behind a hash never changes, so
 * the answer for a hash is the same for ever and is served as immutable.
 */
type Tone struct {
	Violet float64 `json:"violet"`
	Blue   float64 `json:"blue"`
	Teal   float64 `json:"teal"`
	Ember  float64 `json:"ember"`
	Light  float64 `json:"light"`
}

// toneSamples is about how many pixels a side the picture is read at. A wash of
// colour does not need more, and a large still read whole costs time for no
// difference in the answer.
const toneSamples = 48

// ToneOf summarises a picture.
func ToneOf(img image.Image) Tone {
	b := img.Bounds()
	if b.Empty() {
		return Tone{}
	}
	step := max(1, max(b.Dx(), b.Dy())/toneSamples)
	var t Tone
	var weight, light float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			rf, gf, bf := float64(r)/0xffff, float64(g)/0xffff, float64(bl)/0xffff
			light += 0.2126*rf + 0.7152*gf + 0.0722*bf
			n++
			h, chroma := hueChroma(rf, gf, bf)
			// Greys and near-blacks say nothing about colour; counting them would
			// hand every dark still to whichever hue its noise leans toward.
			if chroma < 0.08 {
				continue
			}
			weight += chroma
			switch {
			case h < 70 || h >= 330: // reds, oranges, yellows: the warm ember
				t.Ember += chroma
			case h < 195: // greens and cyans
				t.Teal += chroma
			case h < 255: // blues
				t.Blue += chroma
			default: // purples and magentas
				t.Violet += chroma
			}
		}
	}
	if n > 0 {
		t.Light = round2(light / float64(n))
	}
	if weight == 0 {
		return Tone{Light: t.Light}
	}
	t.Violet = round2(t.Violet / weight)
	t.Blue = round2(t.Blue / weight)
	t.Teal = round2(t.Teal / weight)
	t.Ember = round2(t.Ember / weight)
	return t
}

// hueChroma is the hue in degrees and the chroma (max minus min channel).
func hueChroma(r, g, b float64) (float64, float64) {
	hi := math.Max(r, math.Max(g, b))
	lo := math.Min(r, math.Min(g, b))
	c := hi - lo
	if c == 0 {
		return 0, 0
	}
	var h float64
	switch hi {
	case r:
		h = math.Mod((g-b)/c, 6)
	case g:
		h = (b-r)/c + 2
	default:
		h = (r-g)/c + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, c
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// Tone reads a cached picture's tone from its thumbnail, which is plenty for a
// wash of colour and quick to decode.
func (c *Cache) Tone(hash string) (Tone, error) {
	f, err := c.Open(hash, SizeThumb)
	if err != nil {
		return Tone{}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return Tone{}, fmt.Errorf("artwork tone: %w", err)
	}
	return ToneOf(img), nil
}
