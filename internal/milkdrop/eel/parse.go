package eel

import (
	"strconv"
	"strings"
)

// script := ε | sequence, and nothing after it.
//
// A rule broken inside a block that also fails to parse is reported as the
// parse failure, because eel-wasm parses the whole block before it looks at
// a single call, and the reason a person is shown should be the one the
// engine would have given.
func (p *parser) script() error {
	if p.peek().k != tEOF {
		if _, _, err := p.sequence(func() bool { return p.peek().k == tEOF }); err != nil {
			return err
		}
		if p.peek().k != tEOF {
			return p.unexpected()
		}
	}
	return p.rule
}

// sequence := expr (';'+ expr)* ';'*, stopping before whatever done accepts.
//
// It reports whether a ';' was seen and the last expression's node, because
// `(a)` is that expression and `(a;)` is a block, and assign() can tell them
// apart: eel-wasm accepts `assign((a), 1)` and refuses `assign((a;), 1)`.
// There is no leading ';': eel-wasm refuses `;a = 1`.
func (p *parser) sequence(done func() bool) (bool, node, error) {
	semi := false
	for {
		n, err := p.expr()
		if err != nil {
			return false, node{}, err
		}
		if !p.isChar(";") {
			return semi, n, nil
		}
		semi = true
		for p.isChar(";") {
			p.k++
		}
		if done() {
			return semi, n, nil
		}
	}
}

// node is just enough of the tree for the rules: whether an expression is a
// bare identifier (assign()'s target) or a bare number (loop()'s count).
type node struct {
	ident string
	num   float64
	isNum bool
}

// The single-character binary operators. Comparisons and && || are tokens
// of their own.
var binaryOps = map[string]bool{"*": true, "/": true, "%": true, "&": true, "|": true, "^": true, "+": true, "-": true}

// expr := operand (binop operand)*
func (p *parser) expr() (node, error) {
	if err := p.enter(); err != nil {
		return node{}, err
	}
	defer p.leave()
	n, err := p.operand()
	if err != nil {
		return node{}, err
	}
	for {
		t := p.peek()
		if t.k != tCompare && t.k != tLogical && !(t.k == tChar && binaryOps[t.v]) {
			return n, nil
		}
		p.k++
		if _, err := p.operand(); err != nil {
			return node{}, err
		}
		n = node{}
	}
}

// operand := ('-' | '+' | '!') operand | primary
func (p *parser) operand() (node, error) {
	if p.isChar("-") || p.isChar("+") || p.isChar("!") {
		p.k++
		if err := p.enter(); err != nil {
			return node{}, err
		}
		defer p.leave()
		if _, err := p.operand(); err != nil {
			return node{}, err
		}
		return node{}, nil
	}
	return p.primary()
}

/*
 * primary := number
 *          | '(' sequence ')'
 *          | ident [ '(' arguments ')' ] [ assignop expr ]
 *
 * The assignment sits here, not at the top of an expression, because that is
 * where eel-wasm's grammar lets it be: after any identifier or call, in any
 * position. So `a + b = 3` is `a + (b = 3)` and accepted, and `(a) = 3` is
 * refused.
 */
func (p *parser) primary() (node, error) {
	t := p.peek()
	switch {
	case t.k == tDigits || p.isChar("."):
		return p.number(), nil
	case p.isChar("("):
		p.k++
		if p.isChar(")") {
			return node{}, p.unexpected()
		}
		semi, n, err := p.sequence(func() bool { return p.isChar(")") })
		if err != nil {
			return node{}, err
		}
		if !p.isChar(")") {
			return node{}, p.unexpected()
		}
		p.k++
		if semi {
			return node{}, nil
		}
		return n, nil
	case t.k == tIdent:
		p.k++
		if p.isChar("(") {
			p.k++
			if err := p.call(t); err != nil {
				return node{}, err
			}
		} else if p.peek().k != tAssign {
			return node{ident: t.v}, nil
		}
		if p.peek().k == tAssign {
			p.k++
			if _, err := p.expr(); err != nil {
				return node{}, err
			}
		}
		return node{}, nil
	}
	return node{}, p.unexpected()
}

// number := digits ['.' [digits]] | '.' [digits]. A lone '.' is zero, and
// the parts may be separated by spaces, because eel-wasm builds a number in
// its grammar from separate tokens.
func (p *parser) number() node {
	var s strings.Builder
	if p.peek().k == tDigits {
		s.WriteString(p.peek().v)
		p.k++
		if !p.isChar(".") {
			return literal(s.String())
		}
	}
	p.k++ // the '.'
	s.WriteString(".")
	if p.peek().k == tDigits {
		s.WriteString(p.peek().v)
		p.k++
	}
	return literal(s.String())
}

func literal(s string) node {
	if s == "." {
		return node{isNum: true}
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "."), 64)
	if err != nil {
		// Only a run of digits too long for a float fails here, and eel-wasm
		// reads that as Infinity: not a count anything can be learnt from.
		return node{}
	}
	return node{num: v, isNum: true}
}

// call parses the arguments after the '(' of name, then applies the rules
// eel-wasm applies when it compiles the call.
func (p *parser) call(name token) error {
	var args []node
	if p.isChar(")") {
		p.k++
	} else {
		for i := 0; ; i++ {
			body := (name.v == "loop" && i == 1) || (name.v == "while" && i == 0)
			if body {
				p.loops++
				p.maxLoops = max(p.maxLoops, p.loops)
				if p.loops > p.lim.MaxLoopNesting {
					return p.fail(name, "loops are nested more than %d deep", p.lim.MaxLoopNesting)
				}
			}
			semi, n, err := p.sequence(func() bool { return p.isChar(",") || p.isChar(")") })
			if body {
				p.loops--
			}
			if err != nil {
				return err
			}
			if semi {
				n = node{}
			}
			args = append(args, n)
			if p.isChar(",") {
				p.k++
				continue
			}
			if p.isChar(")") {
				p.k++
				break
			}
			return p.unexpected()
		}
	}
	p.check(name, args, p.peek().k == tAssign)
	return nil
}

// check records the first rule a call breaks. It never stops the parse; see
// script.
func (p *parser) check(name token, args []node, assigned bool) {
	if p.rule != nil {
		return
	}
	fail := func(format string, a ...any) { p.rule = p.fail(name, format, a...) }
	want, known := Functions[name.v]
	switch {
	case !known:
		fail("unknown function %s()", name.v)
	case len(args) != want:
		fail("%s() takes %d %s, given %d", name.v, want, plural(want, "argument"), len(args))
	case assigned && name.v != "megabuf" && name.v != "gmegabuf":
		fail("only megabuf() and gmegabuf() can be assigned to, not %s()", name.v)
	case name.v == "assign" && args[0].ident == "":
		fail("the first argument of assign() must be a variable")
	case name.v == "loop" && args[0].isNum && args[0].num > LoopCap:
		fail("loop() runs at most %d times, and this asks for %g", LoopCap, args[0].num)
	}
}

func plural(n int, s string) string {
	if n == 1 {
		return s
	}
	return s + "s"
}

/*
 * Functions is every function eel-wasm 0.0.17 can call, with its exact
 * number of arguments: eel-wasm refuses too few and too many alike.
 *
 * Read from its bundle: the calls its compiler emits inline, the shims it
 * imports from JavaScript's Math, and the small WebAssembly helpers of its
 * own that a preset may name.
 *
 * Absent on purpose, though MilkDrop and the spike's interpreter have them:
 * invsqrt, fmod, memcpy, memset, and any name beginning with '_' (eel-wasm
 * refuses those even for its own helpers, which is why `_aboeq()`, an ns-eel2
 * internal, fails). A preset that calls one is refused here exactly as
 * eel-wasm would refuse it in the browser.
 */
var Functions = map[string]int{
	// emitted inline
	"if": 3, "loop": 2, "while": 1, "exec2": 2, "exec3": 3, "assign": 2,
	"megabuf": 1, "gmegabuf": 1,
	"abs": 1, "sqrt": 1, "int": 1, "floor": 1, "ceil": 1, "min": 2, "max": 2,
	"above": 2, "below": 2, "equal": 2, "bnot": 1,
	// imported from Math
	"sin": 1, "cos": 1, "tan": 1, "asin": 1, "acos": 1, "atan": 1, "atan2": 2,
	"rand": 1, "pow": 2, "log": 1, "log10": 1, "exp": 1, "sigmoid": 2,
	// eel-wasm's own helpers
	"sqr": 1, "bor": 2, "band": 2, "sign": 1, "mod": 2, "div": 2,
}
