package milkdrop

import (
	"testing"
)

/*
 * FuzzParse feeds the preset reader an uploaded file (docs/hardening-plan.md,
 * item 3). Presets come in downloaded packs of thousands, written by many
 * hands over twenty years, and an administrator uploads them as they come.
 *
 * Parse refuses what it cannot take with an error the person can read; it
 * must never panic, and a preset it accepts must have an identity, since that
 * is what it is stored under.
 */
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"[preset00]\nfDecay=0.98\nzoom=1.01\nper_frame_1=zoom = zoom + 0.01*sin(time);\n",
		"[preset00]\nnWaveMode=3\nwave_0_enabled=1\nwave_0_per_point1=y = 0.5 + value1;\nshapecode_1_enabled=1\nshape_1_per_frame1=x = 0.5;\n",
		"[preset00]\nper_frame_1=while(1) ( x = x + 1; );\n",
		"[preset00]\nwarp_1=`shader_body { ret = tex2D(sampler_main, uv).xyz; }\n",
		`{"baseVals":{"zoom":1.01},"shapes":[],"waves":[]}`,
		"\ufeff[preset00]\r\nfDecay=abc\r\n",
		"<html></html>",
		"",
		"=",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		u, err := Parse(body, "fuzz.milk")
		if err != nil {
			return
		}
		if _, err := ID(u.Preset); err != nil {
			t.Fatalf("an accepted preset has no identity: %v", err)
		}
	})
}
