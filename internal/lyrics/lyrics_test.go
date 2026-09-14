package lyrics

import (
	"strings"
	"testing"
)

/*
 * The LRC parser, one case per claim.
 *
 * Every one of these is a file somebody actually has. The format has no
 * authority behind it and three editors that disagree, so the tests are the
 * specification: what is read, what is dropped, and what a malformed line does
 * instead of failing.
 */

func parse(t *testing.T, body string) Lyrics {
	t.Helper()
	return Parse(strings.NewReader(body))
}

func TestASyncedLineKnowsWhenItIsSung(t *testing.T) {
	got := parse(t, "[00:12.34]The first line\n")
	if !got.Synced {
		t.Fatal("a file with timestamps was not reported as synced")
	}
	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(got.Lines))
	}
	if got.Lines[0].AtMS != 12_340 {
		t.Errorf("at = %d, want 12340", got.Lines[0].AtMS)
	}
	if got.Lines[0].Text != "The first line" {
		t.Errorf("text = %q", got.Lines[0].Text)
	}
}

func TestHundredthsAreNotMilliseconds(t *testing.T) {
	/*
	 * The format specifies two fraction digits, meaning hundredths, and
	 * several editors write three anyway. Reading `.50` as fifty milliseconds
	 * would put every line most of a second early — which on a lyric sheet is
	 * the difference between following the song and leading it.
	 */
	two := parse(t, "[00:10.50]x\n").Lines[0].AtMS
	three := parse(t, "[00:10.500]x\n").Lines[0].AtMS
	if two != 10_500 {
		t.Errorf("two digits = %d, want 10500 (hundredths)", two)
	}
	if three != 10_500 {
		t.Errorf("three digits = %d, want 10500 (milliseconds)", three)
	}
}

func TestAStampWithNoFractionIsWholeSeconds(t *testing.T) {
	if got := parse(t, "[01:05]x\n").Lines[0].AtMS; got != 65_000 {
		t.Errorf("at = %d, want 65000", got)
	}
}

func TestARepeatedChorusIsEveryTimeItIsSung(t *testing.T) {
	/*
	 * Several stamps sharing one line is how the format writes a repeat, and
	 * each is a separate occurrence of the same words. Keeping only the first
	 * would leave the chorus un-highlighted every time after the first, which
	 * is precisely when somebody looks at a lyric sheet.
	 */
	got := parse(t, "[00:30.00][01:30.00][02:30.00]Here we go again\n")
	if len(got.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(got.Lines))
	}
	for i, want := range []int64{30_000, 90_000, 150_000} {
		if got.Lines[i].AtMS != want {
			t.Errorf("line %d at %d, want %d", i, got.Lines[i].AtMS, want)
		}
		if got.Lines[i].Text != "Here we go again" {
			t.Errorf("line %d text = %q", i, got.Lines[i].Text)
		}
	}
}

func TestLinesComeBackInOrderEvenWhenTheFileIsNot(t *testing.T) {
	// A player scanning forward for the current line stops at the first stamp
	// it has passed; out of order, it stops early and never advances again.
	got := parse(t, "[02:00.00]last\n[00:30.00]first\n[01:00.00]middle\n")
	want := []string{"first", "middle", "last"}
	for i, w := range want {
		if got.Lines[i].Text != w {
			t.Errorf("line %d = %q, want %q", i, got.Lines[i].Text, w)
		}
	}
}

func TestTwoLinesAtTheSameMomentKeepTheirOrder(t *testing.T) {
	// A couplet written as two lines on one stamp is a couplet, and swapping
	// them is a visible defect in something somebody is reading along with.
	got := parse(t, "[00:10.00]one\n[00:10.00]two\n")
	if got.Lines[0].Text != "one" || got.Lines[1].Text != "two" {
		t.Errorf("order = %q then %q", got.Lines[0].Text, got.Lines[1].Text)
	}
}

func TestAnOffsetMovesTheWordsEarlier(t *testing.T) {
	/*
	 * `[offset:+500]` exists so somebody can correct a file against their own
	 * copy of a track without retyping ninety timestamps. Positive means the
	 * words should appear earlier, which is the reading every player in common
	 * use takes.
	 */
	got := parse(t, "[offset:+500]\n[00:10.00]x\n")
	if got.Lines[0].AtMS != 9_500 {
		t.Errorf("at = %d, want 9500", got.Lines[0].AtMS)
	}

	late := parse(t, "[offset:-500]\n[00:10.00]x\n")
	if late.Lines[0].AtMS != 10_500 {
		t.Errorf("negative offset at = %d, want 10500", late.Lines[0].AtMS)
	}
}

func TestAnOffsetCannotPushALineBeforeTheSongStarts(t *testing.T) {
	// A negative time is not a place in a song, and a player seeking to one
	// has no sensible behaviour to fall back on.
	got := parse(t, "[offset:+5000]\n[00:01.00]x\n")
	if got.Lines[0].AtMS != 0 {
		t.Errorf("at = %d, want 0", got.Lines[0].AtMS)
	}
}

func TestMetadataIsReadAndNotPrintedAsLyrics(t *testing.T) {
	got := parse(t, "[ti:Song]\n[ar:Somebody]\n[al:A Record]\n[by:a transcriber]\n[length:03:21]\n[00:01.00]x\n")
	if got.Title != "Song" || got.Artist != "Somebody" || got.Album != "A Record" {
		t.Errorf("metadata = %q/%q/%q", got.Title, got.Artist, got.Album)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1: tags about the file are not words in the song", len(got.Lines))
	}
}

func TestAFileWithNoTimestampsIsStillWorthShowing(t *testing.T) {
	/*
	 * Plenty of .lrc files are somebody's copy-and-paste. Showing the words
	 * and saying they are not synced is better than showing nothing, and it is
	 * the difference between "this track has no lyrics" and "this track's
	 * lyrics do not follow it".
	 */
	got := parse(t, "First line\nSecond line\n")
	if got.Synced {
		t.Error("a file with no timestamps reported itself as synced")
	}
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(got.Lines))
	}
	for _, l := range got.Lines {
		if l.AtMS != 0 {
			t.Errorf("an unsynced line carried a time (%d); a line number is not a timestamp", l.AtMS)
		}
	}
}

func TestWordLevelStampsAreStrippedRatherThanPrinted(t *testing.T) {
	// Karaoke-style highlighting is a real feature and not this one. Leaving
	// the stamps in would print angle brackets across the middle of the line.
	got := parse(t, "[00:10.00]<00:10.00>Hello <00:11.00>world\n")
	if got.Lines[0].Text != "Hello world" {
		t.Errorf("text = %q, want the words without the stamps", got.Lines[0].Text)
	}
}

func TestAnEmptyFileIsEmptyRatherThanASingleBlankLine(t *testing.T) {
	// "We found lyrics" for a blank panel is worse than "none found".
	for _, body := range []string{"", "\n\n\n", "[ti:Song]\n[ar:Nobody]\n"} {
		if !parse(t, body).Empty() {
			t.Errorf("%q was not reported as empty", body)
		}
	}
	if parse(t, "[00:01.00]a word\n").Empty() {
		t.Error("a file with a word in it was reported as empty")
	}
}

func TestALineWithNoWordsKeepsItsPlace(t *testing.T) {
	/*
	 * An instrumental break is written as a stamp with nothing after it, and
	 * it is meaningful: it is how a lyric sheet says "nothing is sung here"
	 * rather than leaving the previous line highlighted for ninety seconds.
	 */
	got := parse(t, "[00:10.00]words\n[00:20.00]\n[00:40.00]more words\n")
	if len(got.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(got.Lines))
	}
	if got.Lines[1].Text != "" || got.Lines[1].AtMS != 20_000 {
		t.Errorf("the silent line = %q at %d", got.Lines[1].Text, got.Lines[1].AtMS)
	}
}

func TestSquareBracketsInAWordAreNotATimestamp(t *testing.T) {
	// The line is text, and a reader strict enough to reject it would reject
	// files people have.
	got := parse(t, "[00:10.00]he said [something] quietly\n")
	if got.Lines[0].Text != "he said [something] quietly" {
		t.Errorf("text = %q", got.Lines[0].Text)
	}
}
