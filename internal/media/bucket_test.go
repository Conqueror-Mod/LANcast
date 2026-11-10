package media

import "testing"

// A real library filed its music as `G's/GIMS/track.mp3`. The letter folder is
// an index, not an artist, and must never be read as one.
func TestIsLetterBucket(t *testing.T) {
	for _, name := range []string{
		"G's", "g's", "G’s", "B's", "#", "0-9", "A-Z", "A - Z", "A–C", "0–9",
	} {
		if !IsLetterBucket(name) {
			t.Errorf("IsLetterBucket(%q) = false, want true", name)
		}
	}
	// Real names that are short, or carry an apostrophe, stay names. A bare
	// letter is one of them: X is a band, and an untagged X/Los Angeles/ is
	// its record, so only the unmistakable shapes count.
	for _, name := range []string{
		"", "GIMS", "Go_A", "Guns N' Roses", "Bob's Burgers", "Mr. A", "AC-DC", "A-ha",
		"U2", "Prof", "K's Choice", "Dark", "Soundtrack", "X", "G",
	} {
		if IsLetterBucket(name) {
			t.Errorf("IsLetterBucket(%q) = true, want false", name)
		}
	}
}
