package retrodb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
}

// Index answers hash and serial lookups for every installed platform.
type Index struct {
	tables map[string]*table
}

type table struct {
	bySHA1   map[string]*Game
	byCRC    map[string]*Game
	bySerial map[string]*Game
}

func newTable() *table {
	return &table{
		bySHA1:   map[string]*Game{},
		byCRC:    map[string]*Game{},
		bySerial: map[string]*Game{},
	}
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
