package probe

import (
	"math"
	"testing"
)

// ReplayGain as taggers write it (ADR 0108): foobar2000 and Picard write
// "-7.89 dB", some write a bare number or a plus sign, and Opus files carry
// R128 gains in 256ths of a dB against a different reference.
func TestParseTagsReadsLoudness(t *testing.T) {
	near := func(t *testing.T, name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s: not read, want %v", name, want)
		}
		if math.Abs(*got-want) > 1e-9 {
			t.Fatalf("%s = %v, want %v", name, *got, want)
		}
	}

	got := parseTags(map[string]string{
		"title":                 "Mysterons",
		"REPLAYGAIN_TRACK_GAIN": "-7.89 dB",
		"REPLAYGAIN_TRACK_PEAK": "0.988525",
		"replaygain_album_gain": "+1.20 dB",
		"replaygain_album_peak": "1.000000",
	}).Loudness
	near(t, "track gain", got.TrackGain, -7.89)
	near(t, "track peak", got.TrackPeak, 0.988525)
	near(t, "album gain", got.AlbumGain, 1.2)
	near(t, "album peak", got.AlbumPeak, 1)

	// A bare number is a gain in dB too.
	near(t, "bare", parseTags(map[string]string{"REPLAYGAIN_TRACK_GAIN": "-3.5"}).Loudness.TrackGain, -3.5)

	// Opus: -1280/256 = -5 dB against -23 LUFS is 0 dB against -18.
	opus := parseTags(map[string]string{"R128_TRACK_GAIN": "-1280", "R128_ALBUM_GAIN": "-2560"}).Loudness
	near(t, "r128 track", opus.TrackGain, 0)
	near(t, "r128 album", opus.AlbumGain, -5)

	// ReplayGain wins when a file somehow has both.
	both := parseTags(map[string]string{"REPLAYGAIN_TRACK_GAIN": "-6 dB", "R128_TRACK_GAIN": "0"}).Loudness
	near(t, "both", both.TrackGain, -6)
}

// Absent and broken are both "not tagged": nil, never 0 dB, which would be a
// claim that the track is already at the reference.
func TestParseTagsRefusesBrokenLoudness(t *testing.T) {
	for _, raw := range []map[string]string{
		{"title": "x"},
		{"REPLAYGAIN_TRACK_GAIN": "loud"},
		{"REPLAYGAIN_TRACK_GAIN": "-400 dB"},
		{"REPLAYGAIN_TRACK_PEAK": "0"},
		{"REPLAYGAIN_TRACK_PEAK": "-0.5"},
		{"REPLAYGAIN_TRACK_PEAK": "NaN"},
		{"R128_TRACK_GAIN": "-1.5"},
	} {
		l := parseTags(raw).Loudness
		if l.TrackGain != nil || l.TrackPeak != nil {
			t.Errorf("%v read as gain %v peak %v", raw, l.TrackGain, l.TrackPeak)
		}
	}
	// And gain tags alone do not make a file tagged.
	if !parseTags(map[string]string{"REPLAYGAIN_TRACK_GAIN": "-6 dB"}).Empty() {
		t.Error("a file with only a gain counts as tagged")
	}
}
