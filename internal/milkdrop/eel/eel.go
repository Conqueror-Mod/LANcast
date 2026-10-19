// Package eel checks a MilkDrop equation block before LANcast stores it
// (ADR 0091, step c).
//
// It parses and never evaluates. There is no interpreter here: nothing in a
// preset is ever run on the server, and the browser runs it only as the
// WebAssembly module eel-wasm compiles. This package exists so that a preset
// eel-wasm would refuse is refused once, at upload, with the line and the
// reason, instead of failing silently on every screen that picks it.
//
// So the dialect is **eel-wasm 0.0.17's**, not ns-eel2's, and not the spike
// interpreter's. The spike's interpreter (spikes/milkdrop/eel.mjs) is the
// shape of this code — tokens, then a recursive-descent parser, then a table
// of built-ins — but it accepted more than eel-wasm does: `?:`, `x[i]`,
// `$pi`, `1e3`, `^=`, `~`, `<<`, invsqrt(), fmod(), memcpy(). A validator
// that is looser than the engine is the failure this package exists to
// prevent, so each of those is refused here. Every rule below was read from
// eel-wasm's bundle and then checked against it, string by string and over
// the 9,487-preset corpus (see corpus_test.go).
//
// What eel-wasm does, and therefore what this does:
//
//   - Comments are removed first: `//` and `\\` to the end of the line, and
//     `/* … */`. **Newlines are removed too, with nothing in their place.**
//     That is MilkDrop's own rule for a long equation wrapped across numbered
//     lines — `k1 = is_` then `beat*…` is `is_beat` — and it means a block
//     can be stored with its lines joined by "\n", keeping line numbers for
//     error messages, and still mean what MilkDrop meant.
//   - Tokens are digits, a comparison (== != <= >= < >), an assignment
//     (= += -= *= /= %=), && or ||, an identifier ([A-Za-z_][A-Za-z0-9._]*,
//     case-insensitive), or any single character. Numbers are built from
//     digits and '.' by the grammar, so `1.5`, `.5`, `1.` and a lone `.`
//     (zero) are numbers and `1e3` is not.
//   - A block is statements separated by one or more ';', with no leading
//     ';' and any number of trailing ones. An empty block is fine.
//   - Precedence is eel-wasm's own (it disagrees with butterchurn 2 about
//     `&` and `^`, ADR 0091 §1), but which operator binds tighter never
//     decides whether a block is accepted, so this parser does not model it.
//     What does decide acceptance is reproduced: an identifier, or a call,
//     followed by an assignment operator is an assignment whose right-hand
//     side runs to the end of the expression, wherever it appears.
package eel

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// LoopCap is the most iterations eel-wasm runs one loop() or while() for,
// the same cap as ns-eel2. A count above it is not an error in eel-wasm —
// it is silently cut — so a literal count above it is refused here as a
// preset that asks for something it will not get.
const LoopCap = 1048576

// Limits bounds what a block may cost. The zero value is not useful;
// DefaultLimits is what uploads use.
type Limits struct {
	// MaxBytes is the longest block, before comments are removed.
	MaxBytes int
	// MaxDepth is how deeply expressions may nest: parentheses, calls,
	// operators. It is what keeps this parser's recursion, and eel-wasm's,
	// bounded by something smaller than the input.
	MaxDepth int
	// MaxLoopNesting is how many loop()/while() may enclose one another.
	// The per-loop cap does not bound nesting: two nested loops at the cap
	// are 10^12 iterations, every frame.
	MaxLoopNesting int
}

/*
 * DefaultLimits were read off the corpus, not chosen in the abstract. Over
 * the 9,487 presets (corpus_test.go, run 2026-10-18):
 *
 *   - the longest block is 31,999 bytes, so 64 KiB refuses nothing real;
 *   - the deepest nesting is 23, so 128 leaves room;
 *   - the deepest loop nesting is 3, so 4 is allowed and 5 is not.
 *
 * None of these is a promise about cost. Three nested loops at the cap are
 * already more work than any frame has time for. What bounds the harm is the
 * ADR's argument: a slow preset slows only the tab playing it, and only an
 * administrator can upload one. These limits refuse what no real preset does,
 * and keep this parser's recursion bounded.
 */
var DefaultLimits = Limits{MaxBytes: 64 << 10, MaxDepth: 128, MaxLoopNesting: 4}

// Error is a block eel-wasm would refuse, or one that breaks a limit. Line
// and Col are 1-based positions in the block as given, before comments and
// newlines are removed.
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Col, e.Msg)
}

// Check parses one equation block and reports the first reason eel-wasm
// would refuse it, or a limit it breaks. nil means it compiles.
func Check(src string, lim Limits) error {
	if len(src) > lim.MaxBytes {
		return &Error{Msg: fmt.Sprintf("the block is %d bytes; the most is %d", len(src), lim.MaxBytes)}
	}
	if !utf8.ValidString(src) {
		return &Error{Msg: "the block is not valid text"}
	}
	text, pos, err := strip(src)
	if err != nil {
		return err
	}
	p := &parser{toks: lex(text, pos), lim: lim}
	return p.script()
}

// Stats is what a block costs, measured against no limits.
type Stats struct {
	Bytes, Depth, LoopNesting int
}

// Measure reports a block's size, deepest nesting and deepest loop nesting,
// along with whether eel-wasm would accept it. It exists so the limits in
// DefaultLimits can be read off a corpus rather than guessed.
func Measure(src string) (Stats, error) {
	st := Stats{Bytes: len(src)}
	text, pos, err := strip(src)
	if err != nil {
		return st, err
	}
	p := &parser{toks: lex(text, pos), lim: Limits{MaxBytes: len(src), MaxDepth: 1 << 20, MaxLoopNesting: 1 << 20}}
	err = p.script()
	st.Depth, st.LoopNesting = p.maxDepth, p.maxLoops
	return st, err
}

// ---- comments and newlines ---------------------------------------------------

// where is a position in the original block.
type where struct{ line, col int }

/*
 * strip removes comments and newlines exactly as eel-wasm's preprocessor
 * does, and remembers where each surviving character came from.
 *
 * Its quirks are kept on purpose, because a block is accepted or refused by
 * what eel-wasm does, not by what it should do:
 *   - a `//` inside a block comment still starts a line comment, so after
 *     `/* a // b *\/` the rest of that line is a comment;
 *   - a `/*` inside a line comment still opens a block comment, which the
 *     newline does not close.
 *
 * A carriage return without a line feed is refused. eel-wasm keeps it, and
 * its lexer then counts a new line that the preprocessor did not, which ends
 * in "Unexpected multiline" for any expression that spans it. The .milk
 * reader never produces one; a JSON upload carrying one is refused with a
 * reason rather than in the browser with none.
 */
func strip(src string) (string, []where, error) {
	var b strings.Builder
	pos := make([]where, 0, len(src))
	line, col := 1, 1
	inLine, inBlock := false, false
	rs := []rune(src)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		next := rune(0)
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		switch {
		case c == '\n':
			inLine = false
			line, col = line+1, 1
			continue
		case c == '\r' && next == '\n':
			i++
			inLine = false
			line, col = line+1, 1
			continue
		case inBlock && c == '*' && next == '/':
			inBlock = false
			i++
			col += 2
			continue
		case (c == '\\' && next == '\\') || (c == '/' && next == '/'):
			inLine = true
			i++
			col += 2
			continue
		case c == '/' && next == '*':
			inBlock = true
			i++
			col += 2
			continue
		}
		if !inLine && !inBlock {
			if c == '\r' {
				return "", nil, &Error{Line: line, Col: col, Msg: "a carriage return without a line feed"}
			}
			b.WriteRune(c)
			pos = append(pos, where{line, col})
		}
		col++
	}
	return b.String(), pos, nil
}

// ---- tokens ------------------------------------------------------------------

type kind int

const (
	tEOF kind = iota
	tDigits
	tCompare // == != <= >= < >
	tAssign  // = += -= *= /= %=
	tLogical // && ||
	tIdent
	tChar // any other single character
)

type token struct {
	k  kind
	v  string // identifiers lowercased, as eel-wasm does
	at where
}

// isSpace is JavaScript's \s, which is what eel-wasm's lexer skips.
func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
func isIdentPart(r rune) bool { return isIdentStart(r) || isDigit(r) || r == '.' }

// lex follows eel-wasm's lexer rules in their order: the first rule that
// matches wins, which is jison's default and why `a+=1` is an assignment
// while `a+ =1` is a plus followed by one.
func lex(text string, pos []where) []token {
	rs := []rune(text)
	var out []token
	end := where{1, 1}
	if len(pos) > 0 {
		end = where{pos[len(pos)-1].line, pos[len(pos)-1].col + 1}
	}
	for i := 0; i < len(rs); {
		c := rs[i]
		at := pos[i]
		peek := rune(0)
		if i+1 < len(rs) {
			peek = rs[i+1]
		}
		switch {
		case isSpace(c):
			i++
		case isDigit(c):
			j := i
			for j < len(rs) && isDigit(rs[j]) {
				j++
			}
			out = append(out, token{tDigits, string(rs[i:j]), at})
			i = j
		case (c == '=' || c == '!' || c == '<' || c == '>') && peek == '=':
			out = append(out, token{tCompare, string(rs[i : i+2]), at})
			i += 2
		case c == '<' || c == '>':
			out = append(out, token{tCompare, string(c), at})
			i++
		case strings.ContainsRune("+-*/%", c) && peek == '=':
			out = append(out, token{tAssign, string(rs[i : i+2]), at})
			i += 2
		case c == '=':
			out = append(out, token{tAssign, "=", at})
			i++
		case (c == '&' && peek == '&') || (c == '|' && peek == '|'):
			out = append(out, token{tLogical, string(rs[i : i+2]), at})
			i += 2
		case isIdentStart(c):
			j := i
			for j < len(rs) && isIdentPart(rs[j]) {
				j++
			}
			out = append(out, token{tIdent, strings.ToLower(string(rs[i:j])), at})
			i = j
		default:
			out = append(out, token{tChar, string(c), at})
			i++
		}
	}
	out = append(out, token{k: tEOF, at: end})
	return out
}

// ---- parser --------------------------------------------------------------------

type parser struct {
	toks  []token
	k     int
	lim   Limits
	depth int
	loops int
	// rule is the first rule a call broke; see script.
	rule error
	// The deepest each reached, for Measure.
	maxDepth, maxLoops int
}

func (p *parser) peek() token { return p.toks[p.k] }

func (p *parser) isChar(c string) bool {
	t := p.toks[p.k]
	return t.k == tChar && t.v == c
}

func describe(t token) string {
	switch t.k {
	case tEOF:
		return "the end of the block"
	case tDigits:
		return "a number"
	}
	return fmt.Sprintf("%q", t.v)
}

func (p *parser) fail(t token, format string, args ...any) error {
	return &Error{Line: t.at.line, Col: t.at.col, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) unexpected() error {
	return p.fail(p.peek(), "unexpected %s", describe(p.peek()))
}

// enter bounds recursion, which is the parser's own safety as much as a rule.
func (p *parser) enter() error {
	p.depth++
	p.maxDepth = max(p.maxDepth, p.depth)
	if p.depth > p.lim.MaxDepth {
		return p.fail(p.peek(), "the expression is nested more than %d deep", p.lim.MaxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }
