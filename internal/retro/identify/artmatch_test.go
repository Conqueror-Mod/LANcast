package identify

import "testing"

// Names from the real thumbnail listings, read 2026-10-08, for the two cases
// found on a real library.
var genesisBoxes = []string{
	"Road Rash (Japan)",
	"Road Rash (USA)",
	"Road Rash (USA, Europe)",
	"Road Rash 3 (Europe)",
	"Road Rash 3 (USA) (Alpha)",
	"Road Rash 3 (USA, Europe)",
	"Road Rash II (Japan) (Mega Drive Mini)",
	"Road Rash II (Japan)",
	"Road Rash II (USA) (Genesis Mini)",
	"Road Rash II (USA, Europe) (RR205)",
	"Road Rash II (USA, Europe) (RR206)",
}

var gbaBoxes = []string{
	"Fire Emblem (Europe) (En,Es,It) (Virtual Console)",
	"Fire Emblem (Europe) (En,Es,It)",
	"Fire Emblem (USA) (Virtual Console)",
	"Fire Emblem (USA, Australia)",
	"Fire Emblem - Fuuin no Tsurugi (Japan) (Virtual Console)",
	"Fire Emblem - Fuuin no Tsurugi (Japan)",
	"Fire Emblem - Fuuin no Tsurugi (Japan) [T-En by Gringe]",
	"Fire Emblem - The Binding Blade (USA)",
	"Fire Emblem - Requiem [Hack by Sacred Blaze]",
	"Void's Blitzarre Adventure [Hack by Fire Emblem Universe]",
}

func TestBestThumbnail(t *testing.T) {
	cases := []struct {
		name, why string
		listing   []string
		want      string
	}{
		{"Road Rash II (USA, Europe) (Rev 1)", "the DAT's revision is the set's build code: same regions win, Minis are passed over",
			genesisBoxes, "Road Rash II (USA, Europe) (RR205)"},
		{"Road Rash II (Japan)", "an exact region is still the best", genesisBoxes, "Road Rash II (Japan)"},
		{"Road Rash 3 (USA)", "an alpha is not the game", genesisBoxes, "Road Rash 3 (USA, Europe)"},
		{"Fire Emblem - The Binding Blade (T)", "a translation's own English name",
			gbaBoxes, "Fire Emblem - The Binding Blade (USA)"},
		{"Fire Emblem (Europe)", "a Virtual Console build is passed over",
			gbaBoxes, "Fire Emblem (Europe) (En,Es,It)"},
		{"Fire Emblem (USA) (Virtual Console)", "unless the name asks for one",
			gbaBoxes, "Fire Emblem (USA) (Virtual Console)"},
		{"Fire Emblem - Requiem", "a hack's box is never given to a name that is not one",
			gbaBoxes, ""},
		{"Fire Emblem (Brazil)", "the same game in another region beats nothing",
			gbaBoxes, "Fire Emblem (USA, Australia)"},
		{"Fire", "a shared prefix is not the same title", gbaBoxes, ""},
		{"Advance Wars (USA)", "nothing by that title", gbaBoxes, ""},
	}
	for _, c := range cases {
		if got := bestThumbnail(c.name, c.listing); got != c.want {
			t.Errorf("%s (%s):\n got %q\nwant %q", c.name, c.why, got, c.want)
		}
	}
}

func TestBestThumbnailIsTheSameEveryTime(t *testing.T) {
	rev := make([]string, len(genesisBoxes))
	for i, n := range genesisBoxes {
		rev[len(rev)-1-i] = n
	}
	if a, b := bestThumbnail("Road Rash II (USA, Europe) (Rev 1)", genesisBoxes),
		bestThumbnail("Road Rash II (USA, Europe) (Rev 1)", rev); a != b {
		t.Fatalf("the listing's order changed the answer: %q vs %q", a, b)
	}
}
