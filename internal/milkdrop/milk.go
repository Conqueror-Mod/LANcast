package milkdrop

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

/*
 * Reading a .milk file.
 *
 * A .milk is an INI file with one section, `[preset00]`. Every line is
 * `key=value`, and the keys say what the value is:
 *
 *   per_frame_init_N, per_frame_N, per_pixel_N    the preset's own equations
 *   shape_S_initN, shape_S_per_frameN             custom shape S's equations
 *   wave_W_initN, wave_W_per_frameN, …per_pointN  custom wave W's equations
 *   shapecode_S_X, wavecode_W_X                   their base values
 *   warp_N, comp_N                                HLSL shaders (MilkDrop 2)
 *   anything else                                 a base value
 *
 * MilkDrop wraps a long equation across numbered lines. The lines of one block
 * are put back in number order and joined with "\n". That is not the newline
 * the spike warned about: eel-wasm removes newlines with nothing in their
 * place, so `k1 = is_` + `beat*…` still reads `is_beat`, exactly as MilkDrop
 * reads it, and a `//` comment still ends at its own line. Joining with "\n"
 * rather than "" is what lets a refusal name the line it is on.
 *
 * The shaders are not read here as the equations are. Their text is taken
 * out of the file as butterchurn's own converter takes it (splitShaders, in
 * shader.go) and kept on the upload, unconverted and marked not_converted:
 * converting is ConvertShaders' job, after Parse, so that Parse stays pure.
 */

// eqLine matches the keys that carry equations or shaders.
var eqLine = regexp.MustCompile(`^(?:` +
	`per_frame_init_(\d+)|per_frame_(\d+)|per_pixel_(\d+)|` +
	`shape_([0-3])_init(\d+)|shape_([0-3])_per_frame(\d+)|` +
	`wave_([0-3])_init(\d+)|wave_([0-3])_per_frame(\d+)|wave_([0-3])_per_point(\d+)|` +
	`(warp|comp)_(\d+))$`)

var codeLine = regexp.MustCompile(`^(shapecode|wavecode)_([0-3])_(.+)$`)

// milkNames maps a .milk base-value name to butterchurn's, as butterchurn's
// own converter (milkdrop-preset-utils 0.2.1) maps it. A name not here is
// kept as it is, lowercased, which is also what that converter does.
var milkNames = map[string]string{
	"frating": "rating", "fgammaadj": "gammaadj", "fdecay": "decay",
	"fvideoechozoom": "echo_zoom", "fvideoechoalpha": "echo_alpha",
	"nvideoechoorientation": "echo_orient", "nwavemode": "wave_mode",
	"badditivewaves": "additivewave", "bwavedots": "wave_dots", "bwavethick": "wave_thick",
	"bmodwavealphabyvolume": "modwavealphabyvolume", "bmaximizewavecolor": "wave_brighten",
	"btexwrap": "wrap", "bdarkencenter": "darken_center", "bredbluestereo": "red_blue",
	"bbrighten": "brighten", "bdarken": "darken", "bsolarize": "solarize", "binvert": "invert",
	"fwavealpha": "wave_a", "fwavescale": "wave_scale", "fwavesmoothing": "wave_smoothing",
	"fwaveparam": "wave_mystery", "fmodwavealphastart": "modwavealphastart",
	"fmodwavealphaend": "modwavealphaend", "fwarpanimspeed": "warpanimspeed",
	"fwarpscale": "warpscale", "fzoomexponent": "zoomexp",
	"nmotionvectorsx": "mv_x", "nmotionvectorsy": "mv_y",
	"thick": "thickoutline", "instances": "num_inst", "num_instances": "num_inst",
	"badditive": "additive", "busedots": "usedots", "bspectrum": "spectrum", "bdrawthick": "thick",
}

// notBaseVals are keys that describe the file rather than the picture.
var notBaseVals = map[string]bool{
	"milkdrop_preset_version": true, "psversion": true, "psversion_warp": true, "psversion_comp": true,
}

// leadingNumber is what JavaScript's parseFloat reads, which is how
// butterchurn's converter reads a value: `0.5000` and `1` and `.5`, ignoring
// anything after.
var leadingNumber = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?`)

func milkNumber(s string) (float64, bool) {
	m := leadingNumber.FindString(strings.TrimSpace(s))
	if m == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

type eqText struct {
	n    int
	key  string
	text string
}

// readMilk reads a .milk into a preset and its blocks, unchecked.
func readMilk(body []byte) (Upload, func(*Preset) []block, error) {
	text := decodeText(body)
	sawSection := false
	blocks := map[string][]eqText{}
	main := map[string]float64{}
	parts := [2][4]map[string]float64{} // [0] shapes, [1] waves
	for i := range parts {
		for j := range parts[i] {
			parts[i][j] = map[string]float64{}
		}
	}
	shaders := false
	version := 0.0

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if strings.EqualFold(trimmed, "[preset00]") {
				sawSection = true
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := line[eq+1:]

		if m := eqLine.FindStringSubmatch(key); m != nil {
			name, n := eqBlock(m)
			if name == "warp" || name == "comp" {
				if strings.TrimSpace(strings.TrimPrefix(val, "`")) != "" {
					shaders = true
				}
				continue
			}
			blocks[name] = append(blocks[name], eqText{n: n, key: key, text: val})
			continue
		}
		if m := codeLine.FindStringSubmatch(key); m != nil {
			which := 0
			if m[1] == "wavecode" {
				which = 1
			}
			idx, _ := strconv.Atoi(m[2])
			if v, ok := milkNumber(val); ok {
				parts[which][idx][baseName(m[3])] = v
			}
			continue
		}
		v, ok := milkNumber(val)
		if !ok {
			continue
		}
		if key == "milkdrop_preset_version" {
			version = v
		}
		if notBaseVals[key] {
			continue
		}
		main[baseName(key)] = v
	}
	if !sawSection {
		return Upload{}, nil, invalid("this is not a MilkDrop preset: it has no [preset00] section")
	}

	p := Preset{Version: 1, BaseVals: dropUnnamed(main)}
	if version >= 200 {
		p.Version = 2
	}
	lines := map[string][]eqText{}
	join := func(name string) string {
		ls := blocks[name]
		sort.SliceStable(ls, func(a, b int) bool { return ls[a].n < ls[b].n })
		lines[name] = ls
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = l.text
		}
		return strings.Join(out, "\n")
	}
	p.InitEqs, p.FrameEqs, p.PixelEqs = join("init"), join("frame"), join("pixel")
	for i := 0; i < NumShapes; i++ {
		bv := dropUnnamed(parts[0][i])
		if _, ok := bv["enabled"]; !ok {
			bv["enabled"] = 0 // MilkDrop's default; butterchurn's would be on
		}
		p.Shapes[i] = Shape{BaseVals: bv,
			InitEqs:  join(fmt.Sprintf("shape%d.init", i)),
			FrameEqs: join(fmt.Sprintf("shape%d.frame", i))}
	}
	for i := 0; i < NumWaves; i++ {
		bv := dropUnnamed(parts[1][i])
		if _, ok := bv["enabled"]; !ok {
			bv["enabled"] = 0
		}
		p.Waves[i] = Wave{BaseVals: bv,
			InitEqs:  join(fmt.Sprintf("wave%d.init", i)),
			FrameEqs: join(fmt.Sprintf("wave%d.frame", i)),
			PointEqs: join(fmt.Sprintf("wave%d.point", i))}
	}

	// A refusal names the .milk key the faulty line came from, which is what
	// a person can find in the file.
	named := func(name string, src *string) block {
		return block{name: name, src: src, line: func(n int) string {
			if ls := lines[name]; n >= 1 && n <= len(ls) {
				return ls[n-1].key
			}
			return name
		}}
	}
	blocksOf := func(p *Preset) []block {
		bs := []block{named("init", &p.InitEqs), named("frame", &p.FrameEqs), named("pixel", &p.PixelEqs)}
		for i := range p.Shapes {
			bs = append(bs, named(fmt.Sprintf("shape%d.init", i), &p.Shapes[i].InitEqs),
				named(fmt.Sprintf("shape%d.frame", i), &p.Shapes[i].FrameEqs))
		}
		for i := range p.Waves {
			bs = append(bs, named(fmt.Sprintf("wave%d.init", i), &p.Waves[i].InitEqs),
				named(fmt.Sprintf("wave%d.frame", i), &p.Waves[i].FrameEqs),
				named(fmt.Sprintf("wave%d.point", i), &p.Waves[i].PointEqs))
		}
		return bs
	}
	u := Upload{Source: SourceMilk, Shaders: ShadersNone, Preset: p}
	if shaders {
		src := splitShaders(text)
		u.Shaders, u.HLSL = ShadersNotConverted, &src
	}
	return u, blocksOf, nil
}

func parseMilk(body []byte) (Upload, error) {
	u, blocks, err := readMilk(body)
	if err != nil {
		return Upload{}, err
	}
	if err := check(&u.Preset, blocks); err != nil {
		return Upload{}, err
	}
	return u, nil
}

// eqBlock names the block an equation line belongs to, and its line number.
func eqBlock(m []string) (string, int) {
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch {
	case m[1] != "":
		return "init", num(m[1])
	case m[2] != "":
		return "frame", num(m[2])
	case m[3] != "":
		return "pixel", num(m[3])
	case m[4] != "":
		return "shape" + m[4] + ".init", num(m[5])
	case m[6] != "":
		return "shape" + m[6] + ".frame", num(m[7])
	case m[8] != "":
		return "wave" + m[8] + ".init", num(m[9])
	case m[10] != "":
		return "wave" + m[10] + ".frame", num(m[11])
	case m[12] != "":
		return "wave" + m[12] + ".point", num(m[13])
	}
	return m[14], num(m[15])
}

func baseName(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	if n, ok := milkNames[k]; ok {
		return n
	}
	return k
}

// dropUnnamed removes base values whose names butterchurn could not mean. A
// .milk is read leniently, as MilkDrop reads it, so an odd line is skipped
// rather than refusing the file; a JSON upload is held to the same names
// strictly.
func dropUnnamed(bv map[string]float64) map[string]float64 {
	for k := range bv {
		if len(k) > 64 || !baseValName.MatchString(k) || reservedNames[k] {
			delete(bv, k)
		}
	}
	return bv
}

// decodeText reads a .milk as UTF-8 when it is valid UTF-8 and as Latin-1
// otherwise, which is what a file from 2005 is. Equations are ASCII; this
// only decides what a comment or a stray byte becomes, so that the stored
// text says what the file said.
func decodeText(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	if utf8.Valid(b) {
		return string(b)
	}
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}
