package media

import "testing"

func TestClassifyExtra(t *testing.T) {
	for _, tc := range []struct{ dir, path, kind, title string }{
		{"Trailers", "Alien (1979) Teaser.mkv", ExtraTrailer, "Alien (1979) Teaser"},
		{"Behind The Scenes", "Making_of.mp4", ExtraBehindTheScene, "Making of"},
		{"Behind.The.Scenes", "x.mp4", ExtraBehindTheScene, "x"},
		{"Featurettes", "Visual Effects-featurette.mkv", ExtraFeaturette, "Visual Effects"},
		{"", "Alien-trailer.mkv", ExtraTrailer, "Alien"},
		{"", "Alien-deleted.mkv", ExtraDeletedScene, "Alien"},
		{"", "trailer.mp4", ExtraTrailer, "Trailer"},
		{"Extras", "Gag reel.mkv", ExtraOther, "Gag reel"},
		{"Deleted Scenes", "Alternate ending.mkv", ExtraDeletedScene, "Alternate ending"},
	} {
		kind, title := ClassifyExtra(tc.dir, tc.path)
		if kind != tc.kind || title != tc.title {
			t.Errorf("%s/%s: got %s %q, want %s %q", tc.dir, tc.path, kind, title, tc.kind, tc.title)
		}
	}
}

func TestSamplesAreNotExtras(t *testing.T) {
	for _, p := range []string{"sample.mkv", "Film-sample.mkv", "Film.2011.sample.mkv"} {
		if !IsSample(p) {
			t.Errorf("%s is a sample", p)
		}
	}
	for _, p := range []string{"Free Samples (2012).mkv", "Alien-trailer.mkv"} {
		if IsSample(p) {
			t.Errorf("%s is not a sample", p)
		}
	}
}

func TestExtraStemMatches(t *testing.T) {
	if !ExtraStemMatches("Alien.mkv", "Alien-trailer.mp4") {
		t.Error("Alien's trailer")
	}
	if ExtraStemMatches("Aliens.mkv", "Alien-trailer.mp4") || ExtraStemMatches("Alien.mkv", "Aliens-trailer.mp4") {
		t.Error("a near name claimed another film's trailer")
	}
	if ExtraStemMatches("Alien.mkv", "Alien Teaser.mkv") {
		t.Error("no marker, no claim")
	}
}
