package games

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

/*
 * A minimal reader for Valve's KeyValues text format, which is what both files
 * this package cares about are written in: libraryfolders.vdf and every
 * appmanifest_<appid>.acf.
 *
 * Hand-written rather than a dependency, and small on purpose. The format that
 * matters here is a nesting of "key" "value" pairs and "key" { ... } blocks,
 * and the parts of KeyValues this does not implement — #include, conditional
 * [$WIN32] suffixes, binary VDF — do not appear in either file.
 *
 * It is pure: a reader in, a tree out, no filesystem. That is what lets the
 * Steam rules be tested against fixtures with no Steam installed, the same
 * split probe.ParseJSON keeps for ffprobe.
 */

// errVDF is the class of every parse failure here. A caller cannot do anything
// different for "unterminated block" than for "value where a key should be" —
// both mean this file is not KeyValues — so the detail goes in the message and
// not in the type.
var errVDF = errors.New("not a valid KeyValues file")

// maxDepth bounds nesting. Neither real file nests more than three deep; the
// limit exists so a malformed or hostile file cannot recurse this process to
// death, which matters because these files are read by the desktop client on
// somebody's own machine and are not ours to trust.
const maxDepth = 32

// node is one KeyValues block: its scalar values and its sub-blocks.
//
// Keys are lower-cased on the way in. Steam's own casing is not stable across
// versions of the client — appmanifest has been seen with both "AppState" and
// "appstate", and "SizeOnDisk" and "sizeondisk" — and a reader that matched
// case would fail by finding nothing, which is the failure mode that looks like
// an empty library rather than like a bug.
type node struct {
	vals  map[string]string
	subs  map[string]*node
	order []string // sub-block keys, in file order
}

func newNode() *node {
	return &node{vals: map[string]string{}, subs: map[string]*node{}}
}

// val returns a scalar value, or "" when the key is absent.
func (n *node) val(key string) string { return n.vals[key] }

// sub returns a sub-block, or nil when the key is absent.
func (n *node) sub(key string) *node { return n.subs[key] }

// eachSub visits sub-blocks in the order the file listed them. Order is worth
// keeping for libraryfolders.vdf, where "0" is the folder Steam itself is
// installed in and the rest follow.
func (n *node) eachSub(f func(key string, sub *node)) {
	for _, k := range n.order {
		if s := n.subs[k]; s != nil {
			f(k, s)
		}
	}
}

func (n *node) setVal(key, v string) { n.vals[key] = v }

func (n *node) setSub(key string, s *node) {
	if _, seen := n.subs[key]; !seen {
		n.order = append(n.order, key)
	}
	n.subs[key] = s
}

// parseVDF reads a whole KeyValues document. The top level is treated as an
// implicit block, because an .acf file is a single "AppState" { ... } pair at
// the top and libraryfolders.vdf is a single "libraryfolders" { ... } pair.
func parseVDF(r io.Reader) (*node, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src}
	n, err := p.block(0)
	if err != nil {
		return nil, err
	}
	return n, nil
}

type parser struct {
	src []byte
	i   int
}

// token kinds. A bare token is unquoted text: Steam quotes everything it
// writes, but hand-edited files in the wild do not always, and accepting one
// costs a branch.
const (
	kindString = '"'
	kindBare   = 'b'
	kindOpen   = '{'
	kindClose  = '}'
)

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func (p *parser) skipSpace() {
	for p.i < len(p.src) {
		if isSpace(p.src[p.i]) {
			p.i++
			continue
		}
		// // to end of line. Steam writes these into some .vdf files.
		if p.src[p.i] == '/' && p.i+1 < len(p.src) && p.src[p.i+1] == '/' {
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
			continue
		}
		return
	}
}

func (p *parser) next() (tok string, kind byte, ok bool) {
	p.skipSpace()
	if p.i >= len(p.src) {
		return "", 0, false
	}
	switch c := p.src[p.i]; c {
	case '{', '}':
		p.i++
		return "", c, true
	case '"':
		p.i++
		var b strings.Builder
		for p.i < len(p.src) {
			ch := p.src[p.i]
			// An escape. The one that matters constantly is \\ inside a Windows
			// path — "D:\\Games\\Steam" is how every install directory in these
			// files is written — so an unescaping reader is not optional here.
			if ch == '\\' && p.i+1 < len(p.src) {
				p.i++
				switch p.src[p.i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(p.src[p.i])
				}
				p.i++
				continue
			}
			if ch == '"' {
				p.i++
				return b.String(), kindString, true
			}
			b.WriteByte(ch)
			p.i++
		}
		// Unterminated. Return what there was: the caller will fail on the
		// missing close brace, which is a better message than "bad string".
		return b.String(), kindString, true
	default:
		start := p.i
		for p.i < len(p.src) && !isSpace(p.src[p.i]) && p.src[p.i] != '{' && p.src[p.i] != '}' && p.src[p.i] != '"' {
			p.i++
		}
		return string(p.src[start:p.i]), kindBare, true
	}
}

// block reads pairs until the matching close brace, or until the input ends
// when depth is zero.
func (p *parser) block(depth int) (*node, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: nested deeper than %d", errVDF, maxDepth)
	}
	n := newNode()
	for {
		key, kind, ok := p.next()
		if !ok {
			if depth == 0 {
				return n, nil
			}
			return nil, fmt.Errorf("%w: a block was never closed", errVDF)
		}
		switch kind {
		case kindClose:
			if depth == 0 {
				return nil, fmt.Errorf("%w: a close brace with nothing open", errVDF)
			}
			return n, nil
		case kindOpen:
			return nil, fmt.Errorf("%w: a block where a key was expected", errVDF)
		}

		val, vkind, ok := p.next()
		if !ok {
			return nil, fmt.Errorf("%w: %q has no value", errVDF, key)
		}
		lower := strings.ToLower(key)
		switch vkind {
		case kindOpen:
			sub, err := p.block(depth + 1)
			if err != nil {
				return nil, err
			}
			n.setSub(lower, sub)
		case kindString, kindBare:
			n.setVal(lower, val)
		default: // a close brace
			return nil, fmt.Errorf("%w: %q has no value", errVDF, key)
		}
	}
}
