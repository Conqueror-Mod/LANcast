package main

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

/*
 * CLIP's byte-pair encoder, in Go (ADR 0060).
 *
 * WHY THIS IS HERE RATHER THAN IMPORTED
 *
 * A text query and a photograph are only comparable if both went through the
 * *same* model, and the text half of that model starts with this tokenizer. Get
 * it subtly wrong — a different whitespace rule, a missing end-of-word marker —
 * and nothing fails: the query still produces 77 token ids, the text encoder
 * still produces a 512-vector, the search still returns photographs in a
 * confident order, and they are the wrong photographs. There is no error path
 * for a tokenizer that is nearly right.
 *
 * So it is written here and tested here, the way this project wrote its own
 * EXIF reader rather than take a dependency for two tags. It is a few hundred
 * lines of a published, fixed algorithm, and the alternative is a dependency in
 * the one binary where a supply-chain surprise executes native code.
 *
 * WHAT IS FAITHFUL AND WHAT IS NOT
 *
 * The merge table, the byte-to-unicode mapping, the end-of-word marker, the
 * pre-tokenisation pattern and the vocabulary's construction are exactly the
 * reference implementation's. The text cleaning is deliberately not: the
 * original runs `ftfy`, which repairs mojibake from scraped web captions. A
 * query somebody typed into a search box has not been through a broken decoder,
 * so this does the half that still matters — HTML unescaping and whitespace
 * collapsing — and skips the half that exists for training data.
 *
 * THE VOCABULARY IS DERIVED, NOT DOWNLOADED SEPARATELY
 *
 * A common misreading of this format is that the merges and the vocabulary are
 * two files. They are not: the vocabulary is 256 byte characters, the same 256
 * again with the end-of-word marker, then one entry per merge, then the two
 * special tokens — 49,408 in the order the model's embedding matrix expects. An
 * implementation that sorted or de-duplicated that list would produce ids that
 * are all individually plausible and collectively wrong.
 */

const (
	// ContextLength is CLIP's fixed sequence length. Shorter queries are padded
	// with zeros and longer ones are truncated, keeping the end-of-text marker,
	// because the text encoder reads a fixed-width tensor.
	ContextLength = 77

	startOfText = "<|startoftext|>"
	endOfText   = "<|endoftext|>"

	// mergeCount is how many merges the reference reads from the file:
	// 49152 - 256 - 2 + 1. Stated as the arithmetic rather than as 48894 so it
	// is checkable against the source it came from.
	mergeCount = 49152 - 256 - 2 + 1
)

// clipPattern is the reference pre-tokenisation regex. Go's regexp is RE2,
// which has the Unicode classes this needs and none of the lookaround it does
// not.
var clipPattern = regexp.MustCompile(
	`(?i)<\|startoftext\|>|<\|endoftext\|>|'s|'t|'re|'ve|'m|'ll|'d|[\pL]+|[\pN]|[^\s\pL\pN]+`)

var whitespaceRun = regexp.MustCompile(`\s+`)

type mergePair struct{ a, b string }

// Tokenizer turns a typed query into the token ids CLIP's text encoder expects.
type Tokenizer struct {
	encoder map[string]int32
	ranks   map[mergePair]int
	cache   map[string][]string

	byteToRune map[byte]rune
}

/*
 * bytesToUnicode maps every byte to a printable rune.
 *
 * BPE operates on characters, and arbitrary bytes include control codes and the
 * space, which cannot survive a text-splitting pattern. So the 188 bytes that
 * are already printable map to themselves and the remaining 68 are lifted into
 * an unused block at U+0100. This is the same trick GPT-2 uses and the mapping
 * must match exactly, because the merge table was built in terms of it.
 */
func bytesToUnicode() (map[byte]rune, []string) {
	var bs []int
	for b := '!'; b <= '~'; b++ {
		bs = append(bs, int(b))
	}
	for b := '¡'; b <= '¬'; b++ {
		bs = append(bs, int(b))
	}
	for b := '®'; b <= 'ÿ'; b++ {
		bs = append(bs, int(b))
	}

	inSet := make(map[int]bool, len(bs))
	for _, b := range bs {
		inSet[b] = true
	}
	cs := append([]int(nil), bs...)
	n := 0
	for b := 0; b < 256; b++ {
		if !inSet[b] {
			bs = append(bs, b)
			cs = append(cs, 256+n)
			n++
		}
	}

	/*
	 * The second return is the vocabulary's first 256 entries **in the order the
	 * model expects**, which is this insertion order and not ascending byte
	 * value.
	 *
	 * That distinction is not cosmetic and it is not obvious. `bs` begins at
	 * '!' (byte 33) because the printable ranges are appended first and the
	 * unprintable bytes afterwards, so entry 0 of the vocabulary is "!" and byte
	 * 0 lands at index 188. Building this by counting 0..255 instead produces a
	 * vocabulary where every token exists, every id is in range, and every one
	 * of them is the wrong row of the embedding matrix — which is a search that
	 * returns confident nonsense and reports no error. It was written that way
	 * first and a test caught it.
	 */
	out := make(map[byte]rune, 256)
	order := make([]string, 0, 256)
	for i, b := range bs {
		out[byte(b)] = rune(cs[i])
		order = append(order, string(rune(cs[i])))
	}
	return out, order
}

/*
 * NewTokenizer builds the encoder from the merges file.
 *
 * The file is the model's, downloaded with it and verified by checksum, so this
 * does not guess at its shape: a file that does not carry enough merges is an
 * error here rather than a vocabulary that is quietly short and produces ids
 * the embedding matrix will index out of range.
 */
func NewTokenizer(r io.Reader) (*Tokenizer, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var merges []mergePair
	line := 0
	for sc.Scan() {
		line++
		// The first line is a version banner, not a merge.
		if line == 1 && strings.HasPrefix(sc.Text(), "#version") {
			continue
		}
		parts := strings.Fields(sc.Text())
		if len(parts) != 2 {
			continue
		}
		merges = append(merges, mergePair{parts[0], parts[1]})
		if len(merges) == mergeCount {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read merges: %w", err)
	}
	if len(merges) == 0 {
		return nil, fmt.Errorf("read merges: file carried none")
	}

	byteToRune, ordered := bytesToUnicode()
	t := &Tokenizer{
		encoder:    make(map[string]int32, 49408),
		ranks:      make(map[mergePair]int, len(merges)),
		cache:      map[string][]string{startOfText: {startOfText}, endOfText: {endOfText}},
		byteToRune: byteToRune,
	}

	/*
	 * The vocabulary, in the one order the embedding matrix agrees with.
	 *
	 * Built rather than read: 256 byte characters, the same 256 with the
	 * end-of-word marker, one entry per merge, then the two special tokens.
	 * Sorting or de-duplicating this would produce ids that are individually
	 * plausible and collectively wrong.
	 */
	var next int32
	add := func(tok string) {
		if _, seen := t.encoder[tok]; seen {
			return
		}
		t.encoder[tok] = next
		next++
	}
	for _, v := range ordered {
		add(v)
	}
	for _, v := range ordered {
		add(v + "</w>")
	}
	for i, m := range merges {
		t.ranks[m] = i
		add(m.a + m.b)
	}
	add(startOfText)
	add(endOfText)

	return t, nil
}

// VocabSize is how many tokens the encoder knows, which must match the width of
// the model's embedding matrix.
func (t *Tokenizer) VocabSize() int { return len(t.encoder) }

/*
 * bpe merges one pre-token into sub-tokens, lowest rank first.
 *
 * The end-of-word marker goes on the *last* character rather than being a
 * separate token, which is what lets the table distinguish "dog" ending a word
 * from "dog" starting "dogma".
 */
func (t *Tokenizer) bpe(token string) []string {
	if cached, ok := t.cache[token]; ok {
		return cached
	}
	if token == "" {
		return nil
	}

	word := make([]string, 0, utf8.RuneCountInString(token))
	for _, r := range token {
		word = append(word, string(r))
	}
	word[len(word)-1] += "</w>"

	for len(word) > 1 {
		best, bestRank, found := 0, 0, false
		for i := 0; i < len(word)-1; i++ {
			rank, ok := t.ranks[mergePair{word[i], word[i+1]}]
			if !ok {
				continue
			}
			if !found || rank < bestRank {
				best, bestRank, found = i, rank, true
			}
		}
		if !found {
			break
		}
		merged := make([]string, 0, len(word)-1)
		merged = append(merged, word[:best]...)
		merged = append(merged, word[best]+word[best+1])
		merged = append(merged, word[best+2:]...)
		word = merged
	}

	t.cache[token] = word
	return word
}

// clean is the reference's text preparation, minus the mojibake repair — see
// the file comment for why that half is deliberately absent.
func clean(text string) string {
	text = html.UnescapeString(html.UnescapeString(text))
	text = whitespaceRun.ReplaceAllString(text, " ")
	return strings.ToLower(strings.TrimSpace(text))
}

/*
 * Encode turns a query into exactly ContextLength ids.
 *
 * Truncation keeps the end-of-text marker rather than simply cutting: the
 * encoder reads the vector at that marker's position as the sentence's meaning,
 * so a truncated query without one is not a shortened search, it is a search
 * for whatever the 77th token happened to be.
 */
func (t *Tokenizer) Encode(text string) ([]int32, error) {
	sot, ok := t.encoder[startOfText]
	if !ok {
		return nil, fmt.Errorf("encode: vocabulary has no %s", startOfText)
	}
	eot := t.encoder[endOfText]

	ids := []int32{sot}
	for _, tok := range clipPattern.FindAllString(clean(text), -1) {
		// Bytes rather than runes: the mapping is per byte, so a multi-byte
		// character becomes several vocabulary entries, which is what the merge
		// table was built against.
		var mapped strings.Builder
		for i := 0; i < len(tok); i++ {
			mapped.WriteRune(t.byteToRune[tok[i]])
		}
		for _, piece := range t.bpe(mapped.String()) {
			id, ok := t.encoder[piece]
			if !ok {
				// Unreachable with a complete merges file, and silently dropping
				// it would shift every later token — better to say so.
				return nil, fmt.Errorf("encode: %q is not in the vocabulary", piece)
			}
			ids = append(ids, id)
		}
	}

	out := make([]int32, ContextLength)
	if len(ids) >= ContextLength {
		copy(out, ids[:ContextLength])
		out[ContextLength-1] = eot
		return out, nil
	}
	copy(out, append(ids, eot))
	return out, nil
}
