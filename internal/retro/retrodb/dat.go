// Package retrodb identifies ROMs offline against libretro-database's DAT
// files (ADR 0073).
//
// The DATs are data, fetched once on request from a pinned commit (see
// install.go), so identifying a library makes no network call and no
// phone-home holds. The parser is pure and is tested against fixtures.
package retrodb

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

/*
 * A DAT is the clrmamepro text format:
 *
 *	game (
 *		name "Super Mario 64 (USA)"
 *		region "USA"
 *		rom ( name "Super Mario 64 (USA).z64" size 8388608 crc 3CE60709 sha1 9BEF… )
 *	)
 *
 * — nested parenthesised blocks of key/value pairs, values bare or quoted.
 * libretro's metadata DATs (releaseyear, genre) are the same shape with a
 * `comment` naming the game instead of a `name`, and a rom carrying only a
 * crc.
 */

// Entry is one `game ( … )` block.
type Entry struct {
	Name    string // the No-Intro or Redump name, region tags included
	Comment string // a metadata DAT's name for the game it describes
	Region  string
	Serial  string
	Year    string
	Genre   string
	ROMs    []ROM
}

// ROM is one `rom ( … )` inside an entry. Hashes are upper-case hex.
type ROM struct {
	Name   string
	CRC    string
	SHA1   string
	Serial string
}

// Parse reads every game block in a DAT. The header block and any block that
// is not a game are skipped.
func Parse(r io.Reader) ([]Entry, error) {
	t := newTokenizer(r)
	var out []Entry
	for {
		tok, err := t.next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if tok.paren {
			return out, fmt.Errorf("line %d: unexpected %q at top level", t.line, tok.text)
		}
		open, err := t.next()
		if err != nil || !open.paren || open.text != "(" {
			return out, fmt.Errorf("line %d: %q is not followed by a block", t.line, tok.text)
		}
		if tok.text != "game" {
			if err := t.skipBlock(); err != nil {
				return out, err
			}
			continue
		}
		e, err := t.game()
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
}

type token struct {
	text  string
	paren bool // a ( or ), as opposed to a word or quoted string
}

type tokenizer struct {
	r    *bufio.Reader
	line int
}

func newTokenizer(r io.Reader) *tokenizer {
	return &tokenizer{r: bufio.NewReaderSize(r, 64<<10), line: 1}
}

func (t *tokenizer) next() (token, error) {
	for {
		c, err := t.r.ReadByte()
		if err != nil {
			return token{}, err
		}
		switch {
		case c == '\n':
			t.line++
		case c == ' ' || c == '\t' || c == '\r':
		case c == '(' || c == ')':
			return token{text: string(c), paren: true}, nil
		case c == '"':
			var b strings.Builder
			for {
				c, err := t.r.ReadByte()
				if err != nil {
					return token{}, fmt.Errorf("line %d: unterminated string", t.line)
				}
				if c == '"' {
					return token{text: b.String()}, nil
				}
				if c == '\n' {
					t.line++
				}
				b.WriteByte(c)
			}
		default:
			var b strings.Builder
			b.WriteByte(c)
			for {
				c, err := t.r.ReadByte()
				if err != nil {
					return token{text: b.String()}, nil
				}
				if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '(' || c == ')' || c == '"' {
					_ = t.r.UnreadByte()
					return token{text: b.String()}, nil
				}
				b.WriteByte(c)
			}
		}
	}
}

// skipBlock consumes up to and including the ) that closes the block whose (
// was just read.
func (t *tokenizer) skipBlock() error {
	depth := 1
	for depth > 0 {
		tok, err := t.next()
		if err != nil {
			return fmt.Errorf("line %d: unclosed block", t.line)
		}
		if tok.paren {
			if tok.text == "(" {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
}

// pairs reads key/value pairs until the closing ), handing nested blocks to
// onBlock. The ( has already been read.
func (t *tokenizer) pairs(onPair func(k, v string), onBlock func(k string) error) error {
	for {
		k, err := t.next()
		if err != nil {
			return fmt.Errorf("line %d: unclosed block", t.line)
		}
		if k.paren {
			if k.text == ")" {
				return nil
			}
			return fmt.Errorf("line %d: unexpected (", t.line)
		}
		v, err := t.next()
		if err != nil {
			return fmt.Errorf("line %d: %q has no value", t.line, k.text)
		}
		if v.paren {
			if v.text != "(" {
				// A key with no value at the end of a block: tolerated, as
				// clrmamepro does.
				return nil
			}
			if err := onBlock(k.text); err != nil {
				return err
			}
			continue
		}
		onPair(k.text, v.text)
	}
}

func (t *tokenizer) game() (Entry, error) {
	var e Entry
	err := t.pairs(func(k, v string) {
		switch k {
		case "name":
			e.Name = v
		case "comment":
			e.Comment = v
		case "region":
			e.Region = v
		case "serial":
			e.Serial = v
		case "releaseyear":
			e.Year = v
		case "genre":
			e.Genre = v
		}
	}, func(k string) error {
		if k != "rom" {
			return t.skipBlock()
		}
		var r ROM
		err := t.pairs(func(k, v string) {
			switch k {
			case "name":
				r.Name = v
			case "crc":
				r.CRC = strings.ToUpper(v)
			case "sha1":
				r.SHA1 = strings.ToUpper(v)
			case "serial":
				r.Serial = v
			}
		}, func(string) error { return t.skipBlock() })
		e.ROMs = append(e.ROMs, r)
		return err
	})
	return e, err
}
