package subtitle

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

/*
 * FuzzSRTToVTT feeds the converter what a sidecar next to a film might hold,
 * which is anything: the file is the library's, and a friend's server sends
 * its own (docs/hardening-plan.md, item 3).
 *
 * The converter must not panic, and what it returns must still be a WebVTT
 * file, which is the one promise the player relies on. Shifting that output
 * is the second step every converted seek takes, so it is fuzzed in the same
 * run.
 */
func FuzzSRTToVTT(f *testing.F) {
	for _, s := range []string{
		"1\n00:00:01,000 --> 00:00:04,000\nHello there\n",
		"\ufeff1\r\n00:00:01,000 --> 00:00:04,000\r\nCRLF and a BOM\r\n\r\n2\r\n00:01:00,500 --> 00:01:02,000\r\n<i>two</i>\r\n",
		"00:00:01,000 --> 00:00:04,000\nno number\n",
		"1\n99:99:99,999 --> 00:00:00,000\nbackwards\n",
		"1\n00:00:01 --> 00:00:02\nno millis\n",
		"WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nalready vtt\n",
		"",
		"-->",
		"1\n\n\n\n",
	} {
		f.Add([]byte(s), 2.5)
	}
	f.Fuzz(func(t *testing.T, in []byte, offset float64) {
		out, err := SRTToVTT(bytes.NewReader(in))
		if err != nil {
			return
		}
		if !bytes.HasPrefix(out, []byte("WEBVTT")) {
			t.Fatalf("output does not begin WEBVTT: %q", firstLine(out))
		}
		shifted := ShiftVTT(out, offset)
		if utf8.Valid(out) && !utf8.Valid(shifted) {
			t.Fatalf("shifting valid text made it invalid")
		}
		_ = EnsureVTTHeader(in)
		_ = LooksLikeText(in)
	})
}

func firstLine(b []byte) []byte {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i]
	}
	return b
}
