package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
)

/*
 * The object syntax of a PDF, and only as much of it as finding a page's
 * image needs: numbers, names, strings, arrays, dictionaries, references and
 * the keywords true, false and null. Content streams are never parsed — a
 * page's drawing operators are not read at all, which is the point.
 */

// Name is a PDF name, without its slash: /Type is Name("Type").
type Name string

// Ref is an indirect reference, "12 0 R".
type Ref struct{ Num, Gen int }

// Dict is a dictionary.
type Dict map[Name]any

// Array is an array.
type Array []any

// String is a string's bytes, literal or hex.
type String []byte

// Stream is a stream object: its dictionary, and where its bytes are in the
// file. The bytes are read only when asked for.
type Stream struct {
	Dict Dict
	Off  int64
	Len  int64
}

// errShort means the buffer ended inside an object: read more and try again.
var errShort = errors.New("pdf: object runs past the buffer")

// maxDepth bounds nesting of arrays and dictionaries. Real files nest a
// handful deep; a hostile one could nest until the stack ran out.
const maxDepth = 64

type lexer struct {
	b   []byte
	pos int
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		if isSpace(c) {
			l.pos++
			continue
		}
		if c == '%' {
			for l.pos < len(l.b) && l.b[l.pos] != '\n' && l.b[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		return
	}
}

// word reads a run of regular characters: a number or a keyword.
func (l *lexer) word() []byte {
	start := l.pos
	for l.pos < len(l.b) && !isSpace(l.b[l.pos]) && !isDelim(l.b[l.pos]) {
		l.pos++
	}
	return l.b[start:l.pos]
}

// keyword reports whether the next token is kw, consuming it if so.
func (l *lexer) keyword(kw string) bool {
	l.skipSpace()
	save := l.pos
	if string(l.word()) == kw {
		return true
	}
	l.pos = save
	return false
}

// atEnd reports whether a word ending at the buffer's end might continue past
// it, which makes the word unreliable.
func (l *lexer) atEnd() bool { return l.pos >= len(l.b) }

func (l *lexer) object(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("pdf: objects nested too deeply")
	}
	l.skipSpace()
	if l.pos >= len(l.b) {
		return nil, errShort
	}
	switch c := l.b[l.pos]; {
	case c == '/':
		return l.name()
	case c == '(':
		return l.literal()
	case c == '<':
		if l.pos+1 >= len(l.b) {
			return nil, errShort
		}
		if l.b[l.pos+1] == '<' {
			return l.dict(depth)
		}
		return l.hex()
	case c == '[':
		l.pos++
		var a Array
		for {
			l.skipSpace()
			if l.pos >= len(l.b) {
				return nil, errShort
			}
			if l.b[l.pos] == ']' {
				l.pos++
				return a, nil
			}
			v, err := l.object(depth + 1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
	case c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9'):
		return l.number()
	case isDelim(c):
		return nil, fmt.Errorf("pdf: unexpected %q", c)
	default:
		w := l.word()
		if l.atEnd() {
			return nil, errShort
		}
		switch string(w) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		return nil, fmt.Errorf("pdf: unexpected keyword %q", w)
	}
}

func (l *lexer) name() (Name, error) {
	l.pos++ // the slash
	w := l.word()
	if l.atEnd() {
		return "", errShort
	}
	if bytes.IndexByte(w, '#') < 0 {
		return Name(w), nil
	}
	var out []byte
	for i := 0; i < len(w); i++ {
		if w[i] == '#' && i+2 < len(w) {
			if v, err := strconv.ParseUint(string(w[i+1:i+3]), 16, 8); err == nil {
				out = append(out, byte(v))
				i += 2
				continue
			}
		}
		out = append(out, w[i])
	}
	return Name(out), nil
}

// number reads an integer or a real, and an integer followed by "gen R" as a
// reference.
func (l *lexer) number() (any, error) {
	w := l.word()
	if l.atEnd() {
		return nil, errShort
	}
	if bytes.ContainsAny(w, ".") {
		f, err := strconv.ParseFloat(string(w), 64)
		if err != nil {
			return nil, fmt.Errorf("pdf: bad number %q", w)
		}
		return f, nil
	}
	n, err := strconv.ParseInt(string(w), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("pdf: bad number %q", w)
	}
	// Look ahead for "gen R".
	save := l.pos
	l.skipSpace()
	g := l.word()
	if len(g) > 0 && isDigits(g) {
		l.skipSpace()
		if l.pos >= len(l.b) {
			return nil, errShort
		}
		if l.b[l.pos] == 'R' && (l.pos+1 >= len(l.b) || isSpace(l.b[l.pos+1]) || isDelim(l.b[l.pos+1])) {
			if l.pos+1 >= len(l.b) {
				return nil, errShort
			}
			l.pos++
			gen, _ := strconv.Atoi(string(g))
			if n < 0 || n > 1<<31 {
				return nil, fmt.Errorf("pdf: bad reference %d", n)
			}
			return Ref{Num: int(n), Gen: gen}, nil
		}
	} else if l.atEnd() {
		return nil, errShort
	}
	l.pos = save
	return n, nil
}

func isDigits(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(b) > 0
}

func (l *lexer) literal() (String, error) {
	l.pos++ // (
	var out []byte
	depth := 1
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return out, nil
			}
		case '\\':
			if l.pos >= len(l.b) {
				return nil, errShort
			}
			e := l.b[l.pos]
			l.pos++
			switch e {
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case '\r', '\n':
				continue // a line continuation
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && l.pos < len(l.b) && l.b[l.pos] >= '0' && l.b[l.pos] <= '7'; k++ {
						v = v*8 + int(l.b[l.pos]-'0')
						l.pos++
					}
					c = byte(v)
				} else {
					c = e
				}
			}
		}
		out = append(out, c)
	}
	return nil, errShort
}

func (l *lexer) hex() (String, error) {
	l.pos++ // <
	var out []byte
	var hi int = -1
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		if c == '>' {
			if hi >= 0 {
				out = append(out, byte(hi<<4))
			}
			return out, nil
		}
		if isSpace(c) {
			continue
		}
		v, ok := hexVal(c)
		if !ok {
			return nil, fmt.Errorf("pdf: bad hex digit %q", c)
		}
		if hi < 0 {
			hi = v
		} else {
			out = append(out, byte(hi<<4|v))
			hi = -1
		}
	}
	return nil, errShort
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func (l *lexer) dict(depth int) (Dict, error) {
	l.pos += 2 // <<
	d := Dict{}
	for {
		l.skipSpace()
		if l.pos+1 >= len(l.b) {
			return nil, errShort
		}
		if l.b[l.pos] == '>' && l.b[l.pos+1] == '>' {
			l.pos += 2
			return d, nil
		}
		if l.b[l.pos] != '/' {
			return nil, fmt.Errorf("pdf: dictionary key is not a name")
		}
		k, err := l.name()
		if err != nil {
			return nil, err
		}
		v, err := l.object(depth + 1)
		if err != nil {
			return nil, err
		}
		d[k] = v
	}
}

// header reads "num gen obj" and returns num and gen.
func (l *lexer) header() (int, int, error) {
	l.skipSpace()
	n := l.word()
	l.skipSpace()
	g := l.word()
	if l.atEnd() {
		return 0, 0, errShort
	}
	if !isDigits(n) || !isDigits(g) || !l.keyword("obj") {
		return 0, 0, errors.New("pdf: no object header here")
	}
	num, err1 := strconv.Atoi(string(n))
	gen, err2 := strconv.Atoi(string(g))
	if err1 != nil || err2 != nil {
		return 0, 0, errors.New("pdf: bad object header")
	}
	return num, gen, nil
}

// streamStart reports whether "stream" follows, and if so moves past it and
// its end of line to the first byte of the data.
func (l *lexer) streamStart() (bool, error) {
	l.skipSpace()
	if l.pos+6 >= len(l.b) {
		return false, errShort
	}
	if !bytes.HasPrefix(l.b[l.pos:], []byte("stream")) {
		return false, nil
	}
	l.pos += 6
	if l.pos < len(l.b) && l.b[l.pos] == '\r' {
		l.pos++
	}
	if l.pos < len(l.b) && l.b[l.pos] == '\n' {
		l.pos++
	}
	return true, nil
}
