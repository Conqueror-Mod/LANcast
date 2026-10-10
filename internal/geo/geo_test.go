package geo

import (
	"sync"
	"testing"
)

// The embedded tables are real data, so these are tests against the world
// rather than fixtures: each one is a place whose answer anybody can check.

var (
	loadOnce sync.Once
	loaded   *Gazetteer
	loadErr  error
)

func gazetteer(t *testing.T) *Gazetteer {
	t.Helper()
	loadOnce.Do(func() { loaded, loadErr = Load() })
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	return loaded
}

func TestTheEmbeddedTablesLoad(t *testing.T) {
	g := gazetteer(t)
	// cities1000 less neighbourhoods and places that no longer exist. A
	// number far below this means the build embedded a truncated table.
	if g.Len() < 150_000 {
		t.Fatalf("loaded %d places, want the whole gazetteer", g.Len())
	}
}

func TestACityCentreIsThatCity(t *testing.T) {
	g := gazetteer(t)
	cases := []struct {
		name         string
		lat, lon     float64
		want, region string
		country      string
	}{
		{"the Eiffel Tower", 48.8584, 2.2945, "Paris", "Île-de-France", "France"},
		{"Texas Tech", 33.5843, -101.8783, "Lubbock", "Texas", "United States"},
		{"Sydney Opera House", -33.8568, 151.2153, "Sydney", "New South Wales", "Australia"},
	}
	for _, c := range cases {
		p, km, ok := g.Nearest(c.lat, c.lon)
		if !ok {
			t.Errorf("%s: no place", c.name)
			continue
		}
		if p.Name != c.want || p.Region != c.region || p.Country != c.country {
			t.Errorf("%s: got %q, %q, %q (%.1f km), want %q, %q, %q",
				c.name, p.Name, p.Region, p.Country, km, c.want, c.region, c.country)
		}
	}
}

// A photograph from the middle of an ocean is not "near" the closest island
// town thousands of kilometres away, and saying so would be a confident lie.
func TestTheOpenSeaIsNoPlace(t *testing.T) {
	g := gazetteer(t)
	if p, km, ok := g.Nearest(-30, -130); ok {
		t.Errorf("mid-Pacific filed under %q, %.0f km away", p.Name, km)
	}
}

// The search walks cells, and the cell east of longitude 179 is -180. A
// point just west of the line must still find the town just east of it.
func TestTheSearchCrossesTheAntimeridian(t *testing.T) {
	g := &Gazetteer{cells: map[cell][]int32{}, regions: map[string]string{}, countries: map[string]string{}}
	g.add(point{lat: -16.80, lon: -179.95, reach: reachKm(5000), id: 1, name: "East", cc: "FJ"})
	g.add(point{lat: -16.80, lon: 179.40, reach: reachKm(5000), id: 2, name: "West", cc: "FJ"})
	p, km, ok := g.Nearest(-16.80, 179.99)
	if !ok || p.Name != "East" {
		t.Fatalf("got %q (%.1f km, ok=%v), want the town 6 km east across the line", p.Name, km, ok)
	}
}

// A small town keeps the ground around its own centre even with a city in
// reach: Wolfforth sits 16 km from Lubbock's centre and is its own place.
func TestASmallTownBesideACityIsItself(t *testing.T) {
	g := gazetteer(t)
	p, _, ok := g.Nearest(33.5059, -102.0091)
	if !ok || p.Name != "Wolfforth" {
		t.Errorf("Wolfforth's centre filed under %q", p.Name)
	}
}

// Inside a city the nearest point is a district or the suburb across the
// river; before population counted, Times Square was Weehawken.
func TestInsideACityIsTheCity(t *testing.T) {
	g := gazetteer(t)
	p, _, ok := g.Nearest(40.7580, -73.9855) // Times Square
	if !ok {
		t.Fatal("no place in Manhattan")
	}
	if p.Name != "New York City" {
		t.Errorf("Times Square filed under %q", p.Name)
	}
}

func BenchmarkNearest(b *testing.B) {
	g, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		g.Nearest(33.5843, -101.8783)
	}
}

func BenchmarkLoad(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Load(); err != nil {
			b.Fatal(err)
		}
	}
}
