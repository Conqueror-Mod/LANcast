package eel

import (
	"errors"
	"strings"
	"testing"
)

// Each rule has its own named test, asserting the verdict and the reason, in
// the manner of internal/probe/decide_test.go. Every accepted and refused
// string here was also run through eel-wasm 0.0.17 itself on 2026-10-18 and
// gets the same verdict there; spikes/milkdrop/godiff.mjs repeats that check
// over the whole corpus.

func accepts(t *testing.T, src string) {
	t.Helper()
	if err := Check(src, DefaultLimits); err != nil {
		t.Errorf("Check(%q) = %v, want accepted", src, err)
	}
}

func refuses(t *testing.T, src, reason string) *Error {
	t.Helper()
	err := Check(src, DefaultLimits)
	if err == nil {
		t.Errorf("Check(%q) accepted it, want refused for %q", src, reason)
		return nil
	}
	if !strings.Contains(err.Error(), reason) {
		t.Errorf("Check(%q) = %q, want a reason containing %q", src, err, reason)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("Check(%q) returned %T, want *Error", src, err)
	}
	return e
}

func TestAcceptsAnEmptyBlock(t *testing.T) {
	accepts(t, "")
	accepts(t, "   ")
	accepts(t, "// only a comment")
}

func TestAcceptsOrdinaryEquations(t *testing.T) {
	accepts(t, "zoom = zoom + 0.01*sin(time*1.3); rot = rot + 0.02*bass_att;")
	accepts(t, "wave_r = 0.5 + 0.5*sin(time); wave_g = above(bass, 1.2)")
	accepts(t, "q1 = if(above(bass,1), q1 + 1, q1 * .98);")
}

// MilkDrop wraps a long equation across numbered lines and rejoins them with
// nothing between. eel-wasm removes newlines the same way, which is why the
// .milk reader can join lines with "\n" and keep line numbers.
func TestNewlinesJoinWithNothingBetween(t *testing.T) {
	accepts(t, "k1 = is_\nbeat * 2")
	// So two statements on two lines without a ';' run together and fail.
	refuses(t, "a = 1\nb = 2", "unexpected")
	accepts(t, "a = 1;\r\nb = 2;")
}

func TestCommentsEndAtTheirLine(t *testing.T) {
	accepts(t, "a = 1; // c = sin(\nb = 2")
	accepts(t, "a = 1; \\\\ also a comment\nb = 2")
	accepts(t, "a = /* block */ 1")
	accepts(t, "a = 1 /* unterminated")
}

// eel-wasm's preprocessor quirk, kept because acceptance follows the engine:
// a `//` inside a block comment still starts a line comment.
func TestLineCommentInsideBlockCommentStillCounts(t *testing.T) {
	accepts(t, "a = 1; /* x // y */ this is not code\nb = 2")
}

func TestASingleBackslashIsNotAComment(t *testing.T) {
	refuses(t, `a = 1 \ c`, `unexpected "\\"`)
}

func TestLoneCarriageReturnIsRefused(t *testing.T) {
	refuses(t, "a = 1;\rb = 2", "carriage return")
}

func TestNumbers(t *testing.T) {
	accepts(t, "a = 1; b = 1.; c = .5; d = 1.25; e = .")
	refuses(t, "a = 1e3", "unexpected \"e3\"")
	refuses(t, "a = 1.2.3", "unexpected \".\"")
}

// The spike's interpreter accepted these; eel-wasm does not.
func TestRefusesSyntaxEelWasmDoesNotHave(t *testing.T) {
	for src, reason := range map[string]string{
		"a = x ? 1 : 2": `unexpected "?"`,
		"a = m[1]":      `unexpected "["`,
		"a = $pi":       `unexpected "$"`,
		"a = $x10":      `unexpected "$"`,
		"a ^= 2":        `unexpected "="`,
		"a |= 1":        `unexpected "="`,
		"a = 1 ~ 2":     `unexpected "~"`,
		"a = 1 << 2":    `unexpected "<"`,
		"a = 'x'":       `unexpected "'"`,
	} {
		refuses(t, src, reason)
	}
}

func TestStatementSeparators(t *testing.T) {
	accepts(t, "a = 1;;; b = 2;;")
	refuses(t, ";a = 1", `unexpected ";"`)
	refuses(t, ";", `unexpected ";"`)
	refuses(t, "a = 1 b = 2", `unexpected "b"`)
}

// Assignment may follow any identifier, anywhere, as eel-wasm's grammar
// allows: `a + b = 3` is `a + (b = 3)`. A parenthesised expression is not
// a target.
func TestAssignmentTargets(t *testing.T) {
	accepts(t, "a = b = 3")
	accepts(t, "a + b = 3")
	accepts(t, "a += 1; a -= 1; a *= 2; a /= 2; a %= 2")
	refuses(t, "(a) = 1", `unexpected "="`)
}

func TestBuffersCanBeAssigned(t *testing.T) {
	accepts(t, "megabuf(1) = 2; gmegabuf(i) += 1")
	refuses(t, "sin(1) = 2", "only megabuf() and gmegabuf() can be assigned to")
	refuses(t, "megabuf(1, 2) = 3", "megabuf() takes 1 argument, given 2")
}

func TestParenthesesAndBlocks(t *testing.T) {
	accepts(t, "a = (b; c)")
	accepts(t, "a = (b; c;)")
	refuses(t, "a = ()", `unexpected ")"`)
	refuses(t, "a = (1)(2)", `unexpected "("`)
}

func TestArguments(t *testing.T) {
	accepts(t, "a = loop(3, b += 1; c)")
	accepts(t, "a = sin (1)")
	refuses(t, "a = min(1,)", `unexpected ")"`)
	refuses(t, "a = min(1 2)", "unexpected a number")
}

func TestUnknownFunctionIsRefused(t *testing.T) {
	e := refuses(t, "zoom = 1;\nrot = wobble(time)", "unknown function wobble()")
	if e != nil && (e.Line != 2 || e.Col != 7) {
		t.Errorf("position = %d:%d, want 2:7 (the name of the call)", e.Line, e.Col)
	}
}

// MilkDrop has these; eel-wasm 0.0.17 does not, so a preset using one would
// be black in the browser.
func TestFunctionsEelWasmLacksAreRefused(t *testing.T) {
	for _, src := range []string{"a = invsqrt(1)", "a = fmod(1, 2)", "a = memcpy(1, 2, 3)", "a = memset(1, 2, 3)", "a = _aboeq(1, 2)"} {
		refuses(t, src, "unknown function")
	}
}

func TestFunctionNamesAreCaseInsensitive(t *testing.T) {
	accepts(t, "A = Sin(1) + ABOVE(x, 2)")
}

func TestArityIsExact(t *testing.T) {
	refuses(t, "a = sin()", "sin() takes 1 argument, given 0")
	refuses(t, "a = sin(1, 2)", "sin() takes 1 argument, given 2")
	refuses(t, "a = if(1, 2)", "if() takes 3 arguments, given 2")
	refuses(t, "a = min(1)", "min() takes 2 arguments, given 1")
}

func TestEveryKnownFunctionIsAccepted(t *testing.T) {
	for name, n := range Functions {
		args := make([]string, n)
		for i := range args {
			args[i] = "x"
		}
		accepts(t, "y = "+name+"("+strings.Join(args, ", ")+")")
	}
}

func TestAssignNeedsAVariable(t *testing.T) {
	accepts(t, "assign(b, 2)")
	accepts(t, "assign((b), 2)")
	refuses(t, "assign(megabuf(1), 2)", "first argument of assign() must be a variable")
	refuses(t, "assign(b;, 2)", "first argument of assign() must be a variable")
}

// eel-wasm parses a whole block before it checks a call, so a block with
// both faults reports the syntax error, as the browser would.
func TestASyntaxErrorOutranksARuleBrokenEarlier(t *testing.T) {
	refuses(t, "a = wobble(1); b = )", `unexpected ")"`)
}

func TestLoopCountAboveTheCapIsRefused(t *testing.T) {
	accepts(t, "loop(1048576, a += 1)")
	refuses(t, "loop(2000000, a += 1)", "loop() runs at most 1048576 times")
}

// The per-loop cap does not bound nesting: two loops at the cap are 10^12
// iterations a frame. Real presets nest three deep; five is refused.
func TestLoopNestingIsBounded(t *testing.T) {
	accepts(t, "loop(10, loop(10, loop(10, loop(10, a += 1))))")
	refuses(t, "loop(10, loop(10, loop(10, loop(10, loop(10, a += 1)))))", "loops are nested more than 4 deep")
	refuses(t, "while(while(while(while(while(a -= 1)))))", "loops are nested more than 4 deep")
}

// A loop's count is not inside the loop.
func TestLoopCountIsNotNesting(t *testing.T) {
	accepts(t, "loop(loop(1, loop(1, loop(1, loop(1, 1)))), a)")
}

func TestDeepNestingIsRefusedNotRecursedInto(t *testing.T) {
	src := "a = " + strings.Repeat("(", 10000) + "1" + strings.Repeat(")", 10000)
	refuses(t, src, "nested more than 128 deep")
	refuses(t, "a = "+strings.Repeat("-", 10000)+"1", "nested more than 128 deep")
}

func TestOversizedBlockIsRefused(t *testing.T) {
	refuses(t, "a = 1;"+strings.Repeat(" ", DefaultLimits.MaxBytes), "the most is 65536")
}

func TestNonASCIIIsRefusedOutsideComments(t *testing.T) {
	accepts(t, "a = 1; // café")
	refuses(t, "a = café", `unexpected "é"`)
	// JavaScript's \s, which eel-wasm skips, includes the no-break space.
	accepts(t, "a = 1")
}

func TestInvalidUTF8IsRefused(t *testing.T) {
	refuses(t, "a = \xff", "not valid text")
}

func TestErrorPositionCountsOriginalLines(t *testing.T) {
	e := refuses(t, "a = 1;\n// note\nb = ?", `unexpected "?"`)
	if e != nil && (e.Line != 3 || e.Col != 5) {
		t.Errorf("position = %d:%d, want 3:5", e.Line, e.Col)
	}
}

func TestMeasureReportsCost(t *testing.T) {
	st, err := Measure("loop(2, loop(3, a = (1 + (2))))")
	if err != nil {
		t.Fatal(err)
	}
	if st.LoopNesting != 2 {
		t.Errorf("LoopNesting = %d, want 2", st.LoopNesting)
	}
	if st.Depth < 3 {
		t.Errorf("Depth = %d, want at least 3", st.Depth)
	}
}
