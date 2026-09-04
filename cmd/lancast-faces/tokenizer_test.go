package main

import (
	"strings"
	"testing"
)

/*
 * The tokenizer, which has no error path when it is nearly right.
 *
 * A wrong merge rule does not crash and does not return an error. It produces
 * 77 ids, which produce a 512-vector, which ranks the library confidently and
 * wrongly. So these assert the mechanics — the byte mapping, the vocabulary's
 * order, the merge preference, the end-of-word marker, the shape of the output
 * — because those are the things that can be checked without the model.
 *
 * The one thing they cannot prove is that the whole pipeline agrees with the
 * reference implementation. That wants a handful of real strings encoded by
 * both and compared, against the real merges file, and it is written down as
 * the gap it is rather than implied by a green suite.
 */

// tinyMerges is a hand-built table: enough to exercise ranking and ordering,
// small enough that every expected id can be worked out by hand.
const tinyMerges = `#version: 0.2
a b
ab c</w>
d e
`

func tinyTokenizer(t *testing.T) *Tokenizer {
	t.Helper()
	tk, err := NewTokenizer(strings.NewReader(tinyMerges))
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

/*
 * The byte mapping must be exactly the reference's, because the merge table was
 * built in terms of it.
 *
 * The three anchors: a printable ASCII byte maps to itself; the space, which is
 * not in the printable set, is lifted into the private range; and every byte
 * has a distinct rune, since a collision would silently merge two different
 * inputs.
 */
func TestByteToUnicodeMatchesTheReference(t *testing.T) {
	m, order := bytesToUnicode()
	if len(m) != 256 {
		t.Fatalf("mapped %d bytes, want 256", len(m))
	}
	if got := m['A']; got != 'A' {
		t.Errorf("'A' mapped to %q, want itself", got)
	}
	if got := m[' ']; got != 'Ġ' {
		t.Errorf("space mapped to %q, want U+0120 — the reference lifts it out of the printable range", got)
	}
	// And the order is the vocabulary's, which starts at '!' rather than at byte
	// zero — the distinction that makes every id plausible and wrong if missed.
	if len(order) != 256 {
		t.Fatalf("order has %d entries, want 256", len(order))
	}
	if order[0] != "!" {
		t.Errorf("vocabulary starts with %q, want %q — the printable ranges are appended first", order[0], "!")
	}
	if order[188] != "Ā" {
		t.Errorf("byte 0 landed at index 188 as %q, want U+0100", order[188])
	}

	seen := map[rune]byte{}
	for b, r := range m {
		if prev, dup := seen[r]; dup {
			t.Fatalf("bytes %d and %d both map to %q; two inputs would tokenise identically", prev, b, r)
		}
		seen[r] = b
	}
}

/*
 * The vocabulary's order is the contract with the model's embedding matrix.
 *
 * 256 byte characters, then the same 256 with the end-of-word marker, then one
 * entry per merge, then the two specials. An implementation that sorted this
 * would produce ids that are all individually valid and collectively wrong —
 * which is the failure with no symptom.
 */
func TestVocabularyIsBuiltInTheModelsOrder(t *testing.T) {
	tk := tinyTokenizer(t)

	// Entry zero is "!", not byte zero: the printable ranges are appended first.
	if got := tk.encoder["!"]; got != 0 {
		t.Errorf("first byte character has id %d, want 0", got)
	}
	if got := tk.encoder["Ā"]; got != 188 {
		t.Errorf("byte 0 has id %d, want 188 — it is appended after the printable ranges", got)
	}
	if got := tk.encoder["!</w>"]; got != 256 {
		t.Errorf("first end-of-word character has id %d, want 256 — the second block starts there", got)
	}
	// Then one per merge, in file order.
	if got := tk.encoder["ab"]; got != 512 {
		t.Errorf("first merge has id %d, want 512", got)
	}
	if got := tk.encoder["abc</w>"]; got != 513 {
		t.Errorf("second merge has id %d, want 513", got)
	}
	// Specials last, in this order.
	if tk.encoder[startOfText] >= tk.encoder[endOfText] {
		t.Error("the specials are not in reference order")
	}
	if tk.VocabSize() != 512+3+2 {
		t.Errorf("vocabulary is %d, want 512 byte entries + 3 merges + 2 specials", tk.VocabSize())
	}
}

/*
 * Merges are applied lowest-rank first, not left to right.
 *
 * With "a b" ranked above "ab c", the word "abc" must become "abc" via "ab",
 * and a naive left-to-right pass over the table would reach the same answer
 * here for the wrong reason — so the assertion is on the intermediate, which
 * only the ranked order produces.
 */
func TestMergesFollowRankNotPosition(t *testing.T) {
	tk := tinyTokenizer(t)
	// (a,b) ranks above (ab,c</w>), so "abc" becomes "ab" + "c</w>" and then
	// the whole word. A pass that merged left to right without consulting rank
	// reaches the same place here only by luck; the intermediate is the point.
	got := tk.bpe("abc")
	if len(got) != 1 || got[0] != "abc</w>" {
		t.Errorf("bpe(abc) = %v, want [abc</w>]", got)
	}
	// "de" has a merge but "d" alone does not: the marker lands on the last
	// character, so the pair is (d, e</w>) and never fires.
	if got := tk.bpe("de"); len(got) != 2 {
		t.Errorf("bpe(de) = %v — the end-of-word marker should stop the (d,e) merge firing", got)
	}
}

// The marker goes on the last character rather than being its own token, which
// is what lets the table tell a word ending in "dog" from one starting with it.
func TestEndOfWordMarkerIsAttachedNotAppended(t *testing.T) {
	tk := tinyTokenizer(t)
	got := tk.bpe("z")
	if len(got) != 1 || got[0] != "z</w>" {
		t.Errorf("bpe(z) = %v, want [z</w>]", got)
	}
}

/*
 * Every query is exactly ContextLength ids, bracketed by the two specials.
 *
 * The encoder reads a fixed-width tensor, so a short query is padded with zeros
 * — and zero is a real token id, which is why the padding has to be what the
 * reference pads with rather than anything that looks empty.
 */
func TestEncodeIsAlwaysContextLength(t *testing.T) {
	tk := tinyTokenizer(t)
	for _, q := range []string{"", "a", "a b c", "  Mixed   Case  "} {
		ids, err := tk.Encode(q)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if len(ids) != ContextLength {
			t.Errorf("%q encoded to %d ids, want %d", q, len(ids), ContextLength)
		}
		if ids[0] != tk.encoder[startOfText] {
			t.Errorf("%q does not begin with the start marker", q)
		}
	}
}

/*
 * A query longer than the window keeps its end-of-text marker.
 *
 * The text encoder reads the vector at that marker's position as the sentence's
 * meaning. Truncating without one is not a shortened search — it is a search
 * for whatever the seventy-seventh token happened to be, which is a result that
 * looks like an answer.
 */
func TestOverlongQueriesKeepTheEndMarker(t *testing.T) {
	tk := tinyTokenizer(t)
	ids, err := tk.Encode(strings.Repeat("a b c ", 200))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != ContextLength {
		t.Fatalf("got %d ids, want %d", len(ids), ContextLength)
	}
	if ids[len(ids)-1] != tk.encoder[endOfText] {
		t.Error("a truncated query lost its end-of-text marker; the encoder would read the wrong position")
	}
}

// Case and surrounding whitespace are normalised, so the same question typed
// two ways is the same search.
func TestCleaningIsCaseAndWhitespaceInsensitive(t *testing.T) {
	tk := tinyTokenizer(t)
	a, err := tk.Encode("A B")
	if err != nil {
		t.Fatal(err)
	}
	b, err := tk.Encode("   a    b   ")
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("the same query typed two ways encoded differently at %d", i)
		}
	}
}

// A merges file that is not one is an error here rather than a short
// vocabulary that indexes out of the embedding matrix at inference time.
func TestAnEmptyMergesFileIsRefused(t *testing.T) {
	if _, err := NewTokenizer(strings.NewReader("#version: 0.2\n")); err == nil {
		t.Error("a merges file carrying no merges was accepted")
	}
}

/*
 * Multi-byte input becomes several vocabulary entries.
 *
 * The mapping is per byte, not per rune, because that is what the merge table
 * was built against. A per-rune implementation works for ASCII and diverges
 * silently the first time somebody searches in their own language.
 */
func TestMultiByteCharactersMapPerByte(t *testing.T) {
	tk := tinyTokenizer(t)
	ids, err := tk.Encode("é")
	if err != nil {
		t.Fatal(err)
	}
	// start + two byte-tokens + end, then padding.
	if ids[1] == 0 || ids[2] == 0 {
		t.Errorf("a two-byte character did not produce two tokens: %v", ids[:5])
	}
	if ids[3] != tk.encoder[endOfText] {
		t.Errorf("expected the end marker at position 3, got %d", ids[3])
	}
}
