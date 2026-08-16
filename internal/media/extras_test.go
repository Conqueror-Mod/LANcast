package media

import (
	"path/filepath"
	"testing"
)

func TestIsExtraFolders(t *testing.T) {
	root := filepath.Join("W:", "Movies")
	j := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }

	extras := []string{
		j("The Film (2011)", "Trailers", "teaser.mkv"),
		j("The Film (2011)", "Featurettes", "making of.mkv"),
		j("The Film (2011)", "Behind The Scenes", "bts.mkv"),
		j("The Film (2011)", "Behind.The.Scenes", "bts.mkv"),
		j("The Film (2011)", "Deleted Scenes", "cut.mkv"),
		j("The Film (2011)", "Extras", "thing.mkv"),
		j("Collections", "The Film (2011)", "Interviews", "cast.mkv"),
	}
	for _, p := range extras {
		if !IsExtra(root, p) {
			t.Errorf("IsExtra(%q) = false, want true", p)
		}
	}

	films := []string{
		j("The Film (2011)", "The Film (2011).mkv"),
		j("The Film (2011)", "CD1", "part.mkv"),
		j("Trailer Park Boys (1999)", "Trailer Park Boys.mkv"),
	}
	for _, p := range films {
		if IsExtra(root, p) {
			t.Errorf("IsExtra(%q) = true, want false", p)
		}
	}
}

/*
 * The rule that protects a real collection.
 *
 * `Movies/Shorts/…` is somebody's short-film collection, and `Movies/Trailers/…`
 * is a folder of trailers they deliberately keep. Discarding either would be a
 * far worse bug than importing a featurette, so an extras folder must have a
 * film folder above it.
 */
func TestTopLevelCategoryFolderIsNotExtras(t *testing.T) {
	root := filepath.Join("W:", "Movies")
	for _, p := range []string{
		filepath.Join(root, "Shorts", "Paperman (2012).mkv"),
		filepath.Join(root, "Trailers", "coming soon.mkv"),
		filepath.Join(root, "Other", "home video.mkv"),
	} {
		if IsExtra(root, p) {
			t.Errorf("IsExtra(%q) = true — a category directly under the root is not an extras folder", p)
		}
	}

	// One level deeper, the same name *is* an extras folder.
	nested := filepath.Join(root, "The Film (2011)", "Shorts", "short.mkv")
	if !IsExtra(root, nested) {
		t.Errorf("IsExtra(%q) = false, want true", nested)
	}
}

func TestIsExtraFilenames(t *testing.T) {
	root := filepath.Join("W:", "Movies")
	j := func(name string) string { return filepath.Join(root, "The Film (2011)", name) }

	extras := []string{
		j("sample.mkv"),
		j("Sample.MKV"),
		j("The Film.2011.sample.mkv"),
		j("The Film-trailer.mp4"),
		j("The Film-featurette.mkv"),
		j("The Film-deleted.mkv"),
	}
	for _, p := range extras {
		if !IsExtra(root, p) {
			t.Errorf("IsExtra(%q) = false, want true", p)
		}
	}

	films := []string{
		j("The Film (2011).mkv"),
		j("Free Samples (2012).mkv"),
		j("Trailer Park Boys.mkv"),
		j("The Sample Room.mkv"),
	}
	for _, p := range films {
		if IsExtra(root, p) {
			t.Errorf("IsExtra(%q) = true, want false — that is a title, not a marker", p)
		}
	}
}

// A path that is not under the root at all is not an extra. The caller has
// handed over a mismatched pair, and discarding a file on the strength of that
// would be the wrong direction to be wrong in.
func TestIsExtraOutsideRoot(t *testing.T) {
	if IsExtra(filepath.Join("W:", "Movies"), filepath.Join("X:", "Other", "Trailers", "x.mkv")) {
		t.Error("a path outside the root was called an extra")
	}
}
