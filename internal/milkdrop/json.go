package milkdrop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

/*
 * Reading butterchurn 3's JSON.
 *
 * This is the format butterchurn's own preset packs ship in, with shaders
 * already converted to GLSL by the native converter (ADR 0091 §4). It is
 * accepted as-is **after key validation**, which is the whole of the trust it
 * gets:
 *
 *   - every key must be one butterchurn 3 reads for its WebAssembly path; an
 *     unknown key is refused, not ignored, so nothing rides along unexamined;
 *   - butterchurn 2's `*_eqs_str` is refused by name. It is JavaScript, it is
 *     what butterchurn would run with `new Function`, and LANcast never
 *     stores it even though `onlyUseWASM` would never reach it;
 *   - base values must be named like base values and be numbers;
 *   - every EEL block butterchurn would compile is parsed, as for a .milk.
 *
 * The GLSL itself is not checked: there is no GLSL parser on the server, and
 * a shader reaches only the GPU, behind the browser's validator (ADR 0091,
 * "What a shader can do once it reaches WebGL"). It is bounded in size.
 */

type wireShape struct {
	BaseVals map[string]float64 `json:"baseVals"`
	InitEqs  string             `json:"init_eqs_eel"`
	FrameEqs string             `json:"frame_eqs_eel"`
}

type wireWave struct {
	BaseVals map[string]float64 `json:"baseVals"`
	InitEqs  string             `json:"init_eqs_eel"`
	FrameEqs string             `json:"frame_eqs_eel"`
	PointEqs string             `json:"point_eqs_eel"`
}

type wirePreset struct {
	Version  *float64           `json:"version"`
	BaseVals map[string]float64 `json:"baseVals"`
	InitEqs  string             `json:"init_eqs_eel"`
	FrameEqs string             `json:"frame_eqs_eel"`
	PixelEqs string             `json:"pixel_eqs_eel"`
	Warp     string             `json:"warp"`
	Comp     string             `json:"comp"`
	Shapes   []wireShape        `json:"shapes"`
	Waves    []wireWave         `json:"waves"`
}

func parseJSON(body []byte) (Upload, error) {
	if err := refuseScript(body); err != nil {
		return Upload{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(body, []byte("\ufeff"))))
	dec.DisallowUnknownFields()
	var w wirePreset
	if err := dec.Decode(&w); err != nil {
		return Upload{}, invalid("this is not a butterchurn preset: %s", jsonReason(err))
	}
	if dec.More() {
		return Upload{}, invalid("this is not a butterchurn preset: more than one JSON value")
	}
	if len(w.Shapes) > NumShapes || len(w.Waves) > NumWaves {
		return Upload{}, invalid("a preset has at most %d shapes and %d waves", NumShapes, NumWaves)
	}
	if len(w.Warp) > MaxShaderBytes || len(w.Comp) > MaxShaderBytes {
		return Upload{}, invalid("a shader is at most %d KB", MaxShaderBytes>>10)
	}

	p := Preset{Version: 2, BaseVals: w.BaseVals,
		InitEqs: w.InitEqs, FrameEqs: w.FrameEqs, PixelEqs: w.PixelEqs,
		Warp: w.Warp, Comp: w.Comp}
	if w.Version != nil {
		switch *w.Version {
		case 1, 2:
			p.Version = int(*w.Version)
		default:
			return Upload{}, invalid("version is 1 or 2 (MilkDrop 1 or 2), not %g", *w.Version)
		}
	}
	for i, s := range w.Shapes {
		p.Shapes[i] = Shape(s)
	}
	for i, wv := range w.Waves {
		p.Waves[i] = Wave(wv)
	}
	fill(&p)
	if err := check(&p, jsonBlocks); err != nil {
		return Upload{}, err
	}
	sh := ShadersNone
	if strings.TrimSpace(p.Warp) != "" || strings.TrimSpace(p.Comp) != "" {
		sh = ShadersConverted
	}
	return Upload{Source: SourceJSON, Shaders: sh, Preset: p}, nil
}

// refuseScript finds butterchurn 2's JavaScript anywhere in the document and
// names it. The strict decode below would refuse it too, as an unknown key,
// but "this preset carries JavaScript" is the reason worth reading.
func refuseScript(body []byte) error {
	var doc any
	if err := json.Unmarshal(bytes.TrimPrefix(body, []byte("\ufeff")), &doc); err != nil {
		return invalid("this is not a butterchurn preset: %s", jsonReason(err))
	}
	var walk func(v any) string
	walk = func(v any) string {
		switch t := v.(type) {
		case map[string]any:
			for k, c := range t {
				if strings.HasSuffix(strings.ToLower(k), "_eqs_str") {
					return k
				}
				if f := walk(c); f != "" {
					return f
				}
			}
		case []any:
			for _, c := range t {
				if f := walk(c); f != "" {
					return f
				}
			}
		}
		return ""
	}
	if k := walk(doc); k != "" {
		return invalid("this preset carries butterchurn 2's JavaScript (%s); only the *_eqs_eel form is accepted", k)
	}
	return nil
}

// jsonReason says why a document did not decode, without Go's prefix.
func jsonReason(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		if te.Field != "" {
			return fmt.Sprintf("%s should be %s, not %s", te.Field, kindOf(te.Type.Kind().String()), te.Value)
		}
		return fmt.Sprintf("expected %s, found %s", kindOf(te.Type.Kind().String()), te.Value)
	}
	return strings.TrimPrefix(err.Error(), "json: ")
}

func kindOf(k string) string {
	switch k {
	case "float64", "int":
		return "a number"
	case "string":
		return "text"
	case "map", "struct":
		return "an object"
	case "slice":
		return "a list"
	}
	return k
}
