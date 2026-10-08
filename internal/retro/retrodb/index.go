package retrodb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"lancast/internal/media"
	"lancast/internal/retro/romhash"
)

// Game is what a match says about a ROM.
type Game struct {
	Platform string
	Name     string // the DAT's canonical name, e.g. "Super Mario 64 (USA)"
	Region   string
	Serial   string
	Year     int
	Genre    string
	// ESRB is the bare ESRB label ("E", "T", "M"), or "". Coverage is uneven:
	// thousands of 8- and 16-bit games, and almost no N64 or PS1 ones.
	ESRB string
}

// Index answers hash and serial lookups for every installed platform.
type Index struct {
	tables map[string]*table
}

type table struct {
	bySHA1   map[string]*Game
	byCRC    map[string]*Game
	bySerial map[string]*Game
	// byName and games are for a person correcting a match by hand (Fix
	// match): every game the DAT lists, findable by its name.
	byName map[string]*Game
	games  []*Game
}

func newTable() *table {
	return &table{
		bySHA1:   map[string]*Game{},
		byCRC:    map[string]*Game{},
		bySerial: map[string]*Game{},
		byName:   map[string]*Game{},
	}
}

// ByName is the game a DAT lists under exactly this name on a console, or nil.
func (ix *Index) ByName(platform, name string) *Game {
	if ix == nil || ix.tables[platform] == nil {
		return nil
	}
	return ix.tables[platform].byName[name]
}

/*
 * Search finds a console's games by the words of their names, for Fix match.
 *
 * Every word of the query must be in the name — "fire emblem" finds every
 * Fire Emblem, "fire emblem japan" the Japanese ones — and nothing looser,
 * because a person reading the list is the matcher here and a long list of
 * near misses hides the right line. A title that is the query exactly comes
 * first, then titles with the fewest words beyond it, then by name.
 *
 * A fan translation is not in a DAT under its English name: The Binding
 * Blade is listed as "Fire Emblem - Fuuin no Tsurugi (Japan)", which the
 * words "fire emblem" reach and "binding blade" do not.
 */
func (ix *Index) Search(platform, query string, limit int) []*Game {
	if ix == nil || ix.tables[platform] == nil {
		return nil
	}
	want := nameWords(query)
	if len(want) == 0 {
		return nil
	}
	type hit struct {
		g     *Game
		exact bool
		extra int
	}
	var hits []hit
	for _, g := range ix.tables[platform].games {
		have := map[string]bool{}
		for _, w := range nameWords(g.Name) {
			have[w] = true
		}
		all := true
		for _, w := range want {
			if !have[w] {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		title := nameWords(media.ROMTitle(g.Name))
		hits = append(hits, hit{g, strings.Join(title, " ") == strings.Join(want, " "), len(title) - len(want)})
	}
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.exact != b.exact {
			return a.exact
		}
		if a.extra != b.extra {
			return a.extra < b.extra
		}
		return a.g.Name < b.g.Name
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*Game, len(hits))
	for i, h := range hits {
		out[i] = h.g
	}
	return out
}

// nameWords is a name's words, lower-cased, letters and digits only.
func nameWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Platforms reports which platforms the index can identify.
func (ix *Index) Platforms() []string {
	if ix == nil {
		return nil
	}
	out := make([]string, 0, len(ix.tables))
	for p := range ix.tables {
		out = append(out, p)
	}
	return out
}

/*
 * Lookup finds the game a ROM is.
 *
 * SHA-1 first, then CRC32, across each layout romhash proposed in order, so a
 * clean dump matches on its first layout and a headered one on its second.
 * A disc is looked up by serial.
 *
 * With no platform, only SHA-1 is tried, and across every table: a SHA-1 is
 * unique across consoles in practice, where a CRC32 is unique only within
 * one, so this is what lets a .bin or .zip that no folder placed be placed by
 * what it is. The match carries its platform for that reason.
 */
func (ix *Index) Lookup(platform string, sums []romhash.Sums, serial string) *Game {
	if ix == nil {
		return nil
	}
	if platform == "" {
		for _, s := range sums {
			for _, t := range ix.tables {
				if g := t.bySHA1[s.SHA1]; g != nil && s.SHA1 != "" {
					return g
				}
			}
		}
		return nil
	}
	t := ix.tables[platform]
	if t == nil {
		return nil
	}
	for _, s := range sums {
		if g := t.bySHA1[s.SHA1]; g != nil && s.SHA1 != "" {
			return g
		}
		if g := t.byCRC[s.CRC32]; g != nil && s.CRC32 != "" {
			return g
		}
	}
	if serial != "" {
		if g := t.bySerial[strings.ToUpper(serial)]; g != nil {
			return g
		}
	}
	return nil
}

// Build indexes one platform from its parsed DATs: the game list and any
// metadata DATs, which are joined to it by CRC and then by name.
func (ix *Index) Build(platform string, games []Entry, metadata ...[]Entry) {
	if ix.tables == nil {
		ix.tables = map[string]*table{}
	}
	t := newTable()
	byName := map[string]*Game{}
	for _, e := range games {
		g := &Game{
			Platform: platform, Name: e.Name, Region: e.Region,
			Serial: e.Serial, Year: atoi(e.Year), Genre: e.Genre,
		}
		byName[e.Name] = g
		if _, dup := t.byName[e.Name]; !dup && e.Name != "" {
			t.byName[e.Name] = g
			t.games = append(t.games, g)
		}
		for _, s := range splitSerials(e.Serial) {
			addOnce(t.bySerial, s, g)
		}
		for _, r := range e.ROMs {
			addOnce(t.bySHA1, r.SHA1, g)
			addOnce(t.byCRC, r.CRC, g)
			for _, s := range splitSerials(r.Serial) {
				addOnce(t.bySerial, s, g)
			}
		}
	}
	for _, meta := range metadata {
		for _, e := range meta {
			var g *Game
			for _, r := range e.ROMs {
				if g = t.byCRC[r.CRC]; g != nil {
					break
				}
			}
			if g == nil {
				g = byName[e.Comment]
			}
			if g == nil {
				continue
			}
			if g.Year == 0 {
				g.Year = atoi(e.Year)
			}
			if g.Genre == "" {
				g.Genre = e.Genre
			}
			if g.ESRB == "" && e.ESRB != "" && !strings.EqualFold(e.ESRB, "NOT RATED") {
				g.ESRB = strings.ToUpper(strings.TrimSpace(e.ESRB))
			}
		}
	}
	ix.tables[platform] = t
}

// addOnce keeps the first game listed under a key. DATs list a parent before
// its clones, so the first is the one worth naming.
func addOnce(m map[string]*Game, key string, g *Game) {
	if key == "" {
		return
	}
	if _, ok := m[key]; !ok {
		m[key] = g
	}
}

// splitSerials splits "SLUS-00594, SLUS-00595" and upper-cases each.
func splitSerials(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToUpper(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	if n < 1950 || n > 2100 {
		return 0
	}
	return n
}

// Load builds an index from an installed DAT directory. A platform whose
// game DAT is absent is left out rather than failing the whole index; a
// metadata DAT that is absent costs that platform its years and genres.
func Load(dir string) (*Index, error) {
	ix := &Index{}
	for _, p := range platformFiles {
		games, err := parseFile(filepath.Join(dir, p.Games.Name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var meta [][]Entry
		for _, m := range p.Metadata {
			if e, err := parseFile(filepath.Join(dir, m.Name)); err == nil {
				meta = append(meta, e)
			}
		}
		ix.Build(p.Platform, games, meta...)
	}
	if len(ix.tables) == 0 {
		return nil, fmt.Errorf("no DAT files in %s", dir)
	}
	return ix, nil
}

func parseFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	e, err := Parse(io.Reader(f))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return e, nil
}
