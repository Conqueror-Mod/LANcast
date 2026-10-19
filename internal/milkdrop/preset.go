// Package milkdrop accepts MilkDrop presets for the Now Playing visualizer
// (ADR 0091, step b): it reads an uploaded file, checks it, and keeps the
// result under the data directory.
//
// The rule it exists to keep is the ADR's title: **a preset is data, never
// code.** What it stores is numbers, EEL text that internal/milkdrop/eel has
// parsed and that only ever runs as eel-wasm's WebAssembly in a browser, and,
// GLSL that runs only on a GPU. Nothing in a
// preset is executed here, and nothing it stores is JavaScript: butterchurn
// 2's `*_eqs_str` is refused by name.
//
// Two formats come in and one goes out:
//   - a `.milk` file, read as MilkDrop writes it. Its HLSL shaders are
//     converted to GLSL by ConvertShaders, after Parse, in a WebAssembly
//     sandbox (shader.go, msc/). When they do not all convert they are
//     dropped, and the preset is marked so the client can say "shaders not
//     converted". The HLSL is kept beside the preset either way, and never
//     served;
//   - butterchurn 3's JSON, whose shaders are already GLSL. It is validated
//     key by key, never trusted.
//
// Either way what is stored is butterchurn 3's preset shape, so the client
// hands it to butterchurn as it is.
package milkdrop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"lancast/internal/milkdrop/eel"
)

// MaxUploadBytes bounds one uploaded preset. The largest of the 9,487 in the
// corpus is 171,610 bytes and the 99th percentile 25 KB; this leaves room
// without letting one request hold much memory.
const MaxUploadBytes = 512 << 10

// MaxShaderBytes bounds each GLSL shader in a JSON upload. butterchurn's own
// largest converted preset is 23 KB in all.
const MaxShaderBytes = 64 << 10

// MaxBaseVals bounds how many base values one preset, shape or wave may set.
// MilkDrop defines about 75; the rest are names a converter passed through.
const MaxBaseVals = 256

// The shapes and waves a preset has. MilkDrop and butterchurn both have
// exactly four of each, and butterchurn indexes all four.
const (
	NumShapes = 4
	NumWaves  = 4
)

// Preset is butterchurn 3's preset, the shape the client loads with
// `onlyUseWASM`. Field names are butterchurn's, which is why they are not
// LANcast's usual snake case.
type Preset struct {
	// Version is the EEL dialect, 1 or 2 (MilkDrop 1 or 2). eel-wasm 0.0.17
	// accepts it and does not use it.
	Version  int                `json:"version"`
	BaseVals map[string]float64 `json:"baseVals"`
	InitEqs  string             `json:"init_eqs_eel"`
	FrameEqs string             `json:"frame_eqs_eel"`
	PixelEqs string             `json:"pixel_eqs_eel"`
	// Warp and Comp are GLSL, or empty for butterchurn's defaults.
	Warp   string           `json:"warp"`
	Comp   string           `json:"comp"`
	Shapes [NumShapes]Shape `json:"shapes"`
	Waves  [NumWaves]Wave   `json:"waves"`
}

// Shape is one custom shape.
type Shape struct {
	BaseVals map[string]float64 `json:"baseVals"`
	InitEqs  string             `json:"init_eqs_eel"`
	FrameEqs string             `json:"frame_eqs_eel"`
}

// Wave is one custom wave.
type Wave struct {
	BaseVals map[string]float64 `json:"baseVals"`
	InitEqs  string             `json:"init_eqs_eel"`
	FrameEqs string             `json:"frame_eqs_eel"`
	PointEqs string             `json:"point_eqs_eel"`
}

// The formats a preset arrives in.
const (
	SourceMilk = "milk"
	SourceJSON = "json"
)

// What happened to a preset's shaders.
const (
	// ShadersNone: the preset has none, and butterchurn's defaults are right.
	ShadersNone = "none"
	// ShadersNotConverted: a .milk had HLSL shaders that did not all convert,
	// or that were uploaded before the server could convert any. It plays
	// with butterchurn's defaults and the client says so.
	ShadersNotConverted = "not_converted"
	// ShadersConverted: GLSL, either a .milk's HLSL converted at upload or
	// butterchurn JSON carrying it, stored as it came.
	ShadersConverted = "converted"
)

// Invalid is an upload refused for what it contains: a client's error, with
// a reason worth showing the person who uploaded it.
type Invalid struct{ Msg string }

func (e *Invalid) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &Invalid{Msg: fmt.Sprintf(format, a...)} }

// IsInvalid reports whether err is a refusal rather than a failure.
func IsInvalid(err error) bool {
	var iv *Invalid
	return errors.As(err, &iv)
}

// Upload is a preset that has passed every check, ready to store.
type Upload struct {
	Name    string
	Source  string
	Shaders string
	// ShadersReason is why a .milk's shaders were not converted: the
	// converter's first error line, named by shader.
	ShadersReason string
	Preset        Preset
	// HLSL is a .milk's shader text, when it has any. Parse leaves it
	// unconverted; ConvertShaders converts it.
	HLSL *ShaderSource
	// Replaces is the id this preset had before its shaders were converted:
	// what an upload before step g3 stored for the same file.
	Replaces string
}

// Parse reads an uploaded file. filename is only a display name: it never
// becomes a path. The format is decided by the content, not the extension,
// because an extension is a claim.
func Parse(body []byte, filename string) (Upload, error) {
	if len(body) > MaxUploadBytes {
		return Upload{}, invalid("a preset is at most %d KB", MaxUploadBytes>>10)
	}
	head := strings.TrimLeftFunc(strings.TrimPrefix(string(body), "\ufeff"), unicode.IsSpace)
	var (
		u   Upload
		err error
	)
	switch {
	case head == "":
		return Upload{}, invalid("the file is empty")
	case strings.HasPrefix(head, "{"):
		u, err = parseJSON(body)
	case strings.HasPrefix(head, "<"):
		return Upload{}, invalid("this is markup, not a MilkDrop preset")
	default:
		u, err = parseMilk(body)
	}
	if err != nil {
		return Upload{}, err
	}
	u.Name = CleanName(filename)
	return u, nil
}

// ID is a preset's identity: the SHA-256 of its canonical JSON. The same
// preset uploaded twice, or under two names, is one preset.
func ID(p Preset) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ---- names ---------------------------------------------------------------------

const maxNameRunes = 120

/*
 * CleanName turns an uploaded filename into a display name.
 *
 * It is only ever shown, never joined to a path — the stored file is named by
 * the preset's hash — but it is still cleaned, because it is shown in every
 * signed-in person's picker: the directory part goes (either slash, since a
 * browser on Windows may send `C:\…`), the extension goes, control and
 * formatting characters go, runs of space collapse, and it is cut to a length
 * a list can show.
 */
func CleanName(filename string) string {
	name := filename
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	lower := strings.ToLower(name)
	for _, ext := range []string{".milk", ".json"} {
		if strings.HasSuffix(lower, ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	var b strings.Builder
	space := false
	for _, r := range name {
		// Space first: a tab or a newline is also a control character, and
		// should separate words rather than join them.
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := b.String()
	if rs := []rune(out); len(rs) > maxNameRunes {
		out = strings.TrimSpace(string(rs[:maxNameRunes]))
	}
	if out == "" {
		return "Untitled preset"
	}
	return out
}

// ---- checks shared by both formats ------------------------------------------------

// baseValName is what a base value may be called: an EEL-shaped name, which
// is all butterchurn ever reads one as.
var baseValName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

/*
 * reservedNames are the names of Object.prototype's own properties.
 *
 * butterchurn builds its variable pools as plain objects keyed by base-value
 * name, so a base value called `__proto__` or `constructor` would be a key on
 * an object that already has one with that name. Nothing in MilkDrop is called
 * any of these; refusing them costs nothing, and the spike's interpreter
 * renamed them for the same reason.
 */
var reservedNames = map[string]bool{
	"__proto__": true, "constructor": true, "hasownproperty": true, "isprototypeof": true,
	"propertyisenumerable": true, "tostring": true, "tolocalestring": true, "valueof": true,
	"__definegetter__": true, "__definesetter__": true, "__lookupgetter__": true, "__lookupsetter__": true,
}

func checkBaseVals(where string, bv map[string]float64) error {
	if len(bv) > MaxBaseVals {
		return invalid("%s: %d base values, and the most is %d", where, len(bv), MaxBaseVals)
	}
	for k, v := range bv {
		if len(k) > 64 || !baseValName.MatchString(k) || reservedNames[k] {
			return invalid("%s: %q is not a base value name", where, k)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return invalid("%s: %s is not a finite number", where, k)
		}
	}
	return nil
}

// enabled is butterchurn's own test: a shape or wave runs unless its
// `enabled` is exactly zero, so one that does not say is on.
func enabled(bv map[string]float64) bool {
	v, ok := bv["enabled"]
	return !ok || v != 0
}

// block is one equation block and the name a person would know it by.
type block struct {
	name string
	src  *string
	// line names line n (1-based) of the block, for a .milk whose lines each
	// came from their own numbered key. Nil for JSON.
	line func(n int) string
}

/*
 * check validates every block butterchurn would compile, and empties the
 * ones it would not.
 *
 * butterchurn compiles a shape's or a wave's equations only when it is
 * enabled. A disabled shape's code is never run, so it is not a reason to
 * refuse a preset — and so it is not stored either: what is kept is exactly
 * what was checked.
 */
func check(p *Preset, blocks func(p *Preset) []block) error {
	if err := checkBaseVals("baseVals", p.BaseVals); err != nil {
		return err
	}
	for i := range p.Shapes {
		s := &p.Shapes[i]
		if err := checkBaseVals(fmt.Sprintf("shape %d", i), s.BaseVals); err != nil {
			return err
		}
		if !enabled(s.BaseVals) {
			s.InitEqs, s.FrameEqs = "", ""
		}
	}
	for i := range p.Waves {
		w := &p.Waves[i]
		if err := checkBaseVals(fmt.Sprintf("wave %d", i), w.BaseVals); err != nil {
			return err
		}
		if !enabled(w.BaseVals) {
			w.InitEqs, w.FrameEqs, w.PointEqs = "", "", ""
		}
	}
	for _, b := range blocks(p) {
		err := eel.Check(*b.src, eel.DefaultLimits)
		if err == nil {
			continue
		}
		var e *eel.Error
		if errors.As(err, &e) && e.Line > 0 && b.line != nil {
			return invalid("%s, column %d: %s", b.line(e.Line), e.Col, e.Msg)
		}
		return invalid("%s: %v", b.name, err)
	}
	return nil
}

// jsonBlocks names each block by its butterchurn key.
func jsonBlocks(p *Preset) []block {
	bs := []block{
		{name: "init_eqs_eel", src: &p.InitEqs},
		{name: "frame_eqs_eel", src: &p.FrameEqs},
		{name: "pixel_eqs_eel", src: &p.PixelEqs},
	}
	for i := range p.Shapes {
		bs = append(bs,
			block{name: fmt.Sprintf("shapes[%d].init_eqs_eel", i), src: &p.Shapes[i].InitEqs},
			block{name: fmt.Sprintf("shapes[%d].frame_eqs_eel", i), src: &p.Shapes[i].FrameEqs})
	}
	for i := range p.Waves {
		bs = append(bs,
			block{name: fmt.Sprintf("waves[%d].init_eqs_eel", i), src: &p.Waves[i].InitEqs},
			block{name: fmt.Sprintf("waves[%d].frame_eqs_eel", i), src: &p.Waves[i].FrameEqs},
			block{name: fmt.Sprintf("waves[%d].point_eqs_eel", i), src: &p.Waves[i].PointEqs})
	}
	return bs
}

// fill makes every map present, so the stored JSON never says null where
// butterchurn reads an object.
func fill(p *Preset) {
	if p.BaseVals == nil {
		p.BaseVals = map[string]float64{}
	}
	for i := range p.Shapes {
		if p.Shapes[i].BaseVals == nil {
			p.Shapes[i].BaseVals = map[string]float64{"enabled": 0}
		}
	}
	for i := range p.Waves {
		if p.Waves[i].BaseVals == nil {
			p.Waves[i].BaseVals = map[string]float64{"enabled": 0}
		}
	}
}
