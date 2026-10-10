// Package geo names the town nearest a point, from a gazetteer built into the
// binary (ADR 0078).
//
// The places view needs a coordinate turned into a name somebody recognises,
// and every service that does that is a request to somebody else's server
// carrying where a family's photographs were taken. That is the one thing
// no-phone-home exists to refuse, so the lookup is local: GeoNames' populated
// places of 1,000 people or more, trimmed to an id, a name, a region and a
// position, and embedded.
//
// Loaded on demand and meant to be dropped after use. The location pass holds
// it while it runs, and the names it assigns are written to the database, so
// nothing that lists places ever needs these tables in memory.
package geo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"
)

//go:embed places.tsv.gz
var placesGz []byte

//go:embed regions.tsv.gz
var regionsGz []byte

//go:embed countries.tsv.gz
var countriesGz []byte

// MaxDistanceKm is how far a photograph may be from the nearest town and
// still be filed under it.
//
// Past this, "near" stops being true: a photograph from a boat or a mountain
// pass named after a town two hundred kilometres off is a wrong answer
// presented as a right one. Such photographs keep their position and have no
// place.
const MaxDistanceKm = 50

// Place is one populated place, named the way a person would say it.
type Place struct {
	// ID is the GeoNames id, which is stable across their exports.
	ID          int64
	Name        string
	Region      string
	CountryCode string
	Country     string
}

type point struct {
	lat, lon float64
	reach    float64
	id       int64
	name     string
	cc       string
	admin1   string
}

// Gazetteer answers nearest-town questions.
type Gazetteer struct {
	pts       []point
	cells     map[cell][]int32
	regions   map[string]string
	countries map[string]string
}

// cell is a one-degree square, the bucket a nearest-town search starts in.
type cell struct{ lat, lon int }

func cellOf(lat, lon float64) cell {
	return cell{int(math.Floor(lat)), int(math.Floor(lon))}
}

// Load decompresses the embedded tables.
func Load() (*Gazetteer, error) {
	g := &Gazetteer{
		cells:     map[cell][]int32{},
		regions:   map[string]string{},
		countries: map[string]string{},
	}
	if err := eachRow(regionsGz, func(f []string) error {
		if len(f) != 2 {
			return fmt.Errorf("region row has %d fields", len(f))
		}
		g.regions[f[0]] = f[1]
		return nil
	}); err != nil {
		return nil, err
	}
	if err := eachRow(countriesGz, func(f []string) error {
		if len(f) != 2 {
			return fmt.Errorf("country row has %d fields", len(f))
		}
		g.countries[f[0]] = f[1]
		return nil
	}); err != nil {
		return nil, err
	}
	if err := eachRow(placesGz, func(f []string) error {
		if len(f) != 7 {
			return fmt.Errorf("place row has %d fields", len(f))
		}
		id, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			return err
		}
		lat, err := strconv.ParseFloat(f[4], 64)
		if err != nil {
			return err
		}
		lon, err := strconv.ParseFloat(f[5], 64)
		if err != nil {
			return err
		}
		pop, err := strconv.ParseInt(f[6], 10, 64)
		if err != nil {
			return err
		}
		g.add(point{lat: lat, lon: lon, reach: reachKm(pop), id: id, name: f[1], cc: f[2], admin1: f[3]})
		return nil
	}); err != nil {
		return nil, err
	}
	return g, nil
}

func eachRow(gz []byte, fn func([]string) error) error {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return fmt.Errorf("gazetteer: %w", err)
	}
	defer zr.Close()
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		if err := fn(strings.Split(sc.Text(), "\t")); err != nil {
			return fmt.Errorf("gazetteer: %w", err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("gazetteer: %w", err)
	}
	return nil
}

// Len is the number of places loaded.
func (g *Gazetteer) Len() int { return len(g.pts) }

func (g *Gazetteer) add(p point) {
	c := cellOf(p.lat, p.lon)
	g.cells[c] = append(g.cells[c], int32(len(g.pts)))
	g.pts = append(g.pts, p)
}

/*
 * reachKm is how far a place's name carries: the radius of a disc holding its
 * population at 3,000 people per square kilometre, and never less than half a
 * kilometre.
 *
 * Nearest-point alone gives the wrong answer exactly where people take the
 * most photographs. A town is a point, and inside a city the nearest point is
 * a district or the suburb across the river: on the real gazetteer Times
 * Square came back as Weehawken, the Eiffel Tower as "Paris 16 Passy" and the
 * Opera House as The Rocks. Dividing the distance by a reach that grows with
 * population lets a city claim its own streets while a small town still owns
 * the ground around its centre. The density is a round figure between a
 * suburb's and a downtown's; what matters is that it is one rule for every
 * place, not that it is any city's true shape.
 */
func reachKm(population int64) float64 {
	r := math.Sqrt(float64(population) / (math.Pi * 3000))
	return math.Max(r, 0.5)
}

// Nearest returns the place a point belongs to, and false when no town lies
// within MaxDistanceKm. The distance returned is to that place's centre.
//
// Every town within MaxDistanceKm is a candidate, and the one with the
// smallest distance divided by its reach wins (see reachKm). The candidates
// are found by walking outward from the point's own one-degree cell a ring at
// a time until a ring lies wholly beyond MaxDistanceKm. A degree of latitude
// is always about 111 km, so that is the cell and its eight neighbours except
// near the poles, where a degree of longitude shrinks and more rings cover
// the same ground.
func (g *Gazetteer) Nearest(lat, lon float64) (Place, float64, bool) {
	origin := cellOf(lat, lon)
	best, bestScore, bestKm := -1, math.Inf(1), 0.0
	// Kilometres in one degree of longitude here, floored so a ring is never
	// assumed wider than it is.
	lonKm := math.Max(111.32*math.Cos(lat*math.Pi/180), 1)
	for r := 0; r <= 180; r++ {
		// Everything in ring r is at least r-1 whole cells away.
		if r > 1 && float64(r-1)*math.Min(111.0, lonKm) > MaxDistanceKm {
			break
		}
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dx), abs(dy)) != r {
					continue
				}
				c := cell{origin.lat + dy, wrapLon(origin.lon + dx)}
				for _, i := range g.cells[c] {
					p := g.pts[i]
					d := haversineKm(lat, lon, p.lat, p.lon)
					if d > MaxDistanceKm {
						continue
					}
					if score := d / p.reach; score < bestScore {
						best, bestScore, bestKm = int(i), score, d
					}
				}
			}
		}
	}
	if best < 0 {
		return Place{}, 0, false
	}
	p := g.pts[best]
	return Place{
		ID:          p.id,
		Name:        p.name,
		Region:      g.regions[p.cc+"."+p.admin1],
		CountryCode: p.cc,
		Country:     g.countries[p.cc],
	}, bestKm, true
}

// wrapLon keeps a cell index on the globe: the cell east of 179 is -180.
func wrapLon(c int) int {
	for c >= 180 {
		c -= 360
	}
	for c < -180 {
		c += 360
	}
	return c
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthKm = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthKm * math.Asin(math.Min(1, math.Sqrt(a)))
}

// Synthetic is a gazetteer of one town, for tests elsewhere that need a place
// to file photographs under without decompressing the real tables.
func Synthetic(name string, lat, lon float64, population int64) *Gazetteer {
	g := &Gazetteer{cells: map[cell][]int32{}, regions: map[string]string{}, countries: map[string]string{}}
	g.add(point{lat: lat, lon: lon, reach: reachKm(population), id: 1, name: name, cc: "ZZ"})
	return g
}
