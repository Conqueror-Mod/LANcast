package retrodb

import (
	"strings"
	"testing"
)

// Names from the real GBA DAT, read 2026-10-08.
const gbaFireEmblems = `
game ( name "Fire Emblem (USA, Australia)" region "USA" rom ( crc 2A524221 sha1 C735FDBB9E8ABE19E0C6A44708DF19ACC962E204 ) )
game ( name "Fire Emblem (Europe) (En,Fr,De)" region "Europe" rom ( crc 00000001 ) )
game ( name "Fire Emblem - Fuuin no Tsurugi (Japan)" region "Japan" rom ( crc 00000002 ) )
game ( name "Fire Emblem - The Sacred Stones (USA, Australia)" region "USA" rom ( crc 00000003 ) )
game ( name "Golden Sun (USA, Europe)" region "USA" rom ( crc 00000004 ) )
`

func gbaIndex(t *testing.T) *Index {
	t.Helper()
	g, err := Parse(strings.NewReader(gbaFireEmblems))
	if err != nil {
		t.Fatal(err)
	}
	ix := &Index{}
	ix.Build("gba", g)
	return ix
}

func names(gs []*Game) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

func TestSearchNeedsEveryWordAndPutsTheExactTitleFirst(t *testing.T) {
	ix := gbaIndex(t)
	got := names(ix.Search("gba", "fire emblem", 10))
	if len(got) != 4 {
		t.Fatalf("got %q", got)
	}
	// "Fire Emblem" itself before the subtitled ones, each group by name.
	if !strings.HasPrefix(got[0], "Fire Emblem (") || !strings.HasPrefix(got[1], "Fire Emblem (") {
		t.Errorf("the exact title did not come first: %q", got)
	}
	if got := names(ix.Search("gba", "Fire Emblem japan", 10)); len(got) != 1 || got[0] != "Fire Emblem - Fuuin no Tsurugi (Japan)" {
		t.Errorf("region word: %q", got)
	}
}

// A fan translation's English name is not in a DAT. Searching it finds
// nothing, rather than every Fire Emblem by its first two words.
func TestSearchIsNotLooserThanItsWords(t *testing.T) {
	ix := gbaIndex(t)
	if got := ix.Search("gba", "Fire Emblem - The Binding Blade", 10); len(got) != 0 {
		t.Errorf("got %q", names(got))
	}
	if got := ix.Search("snes", "fire emblem", 10); len(got) != 0 {
		t.Errorf("another console's DAT was searched: %q", names(got))
	}
	if got := ix.Search("gba", "  ", 10); len(got) != 0 {
		t.Errorf("an empty query listed %d games", len(got))
	}
	if got := ix.Search("gba", "fire emblem", 2); len(got) != 2 {
		t.Errorf("limit ignored: %d", len(got))
	}
}

func TestByNameIsExactAndPerConsole(t *testing.T) {
	ix := gbaIndex(t)
	if g := ix.ByName("gba", "Fire Emblem - Fuuin no Tsurugi (Japan)"); g == nil || g.Region != "Japan" {
		t.Errorf("got %+v", g)
	}
	if ix.ByName("gba", "fire emblem - fuuin no tsurugi (japan)") != nil {
		t.Error("a name in another case was accepted")
	}
	if ix.ByName("nes", "Fire Emblem (USA, Australia)") != nil {
		t.Error("another console's name was accepted")
	}
	var none *Index
	if none.ByName("gba", "x") != nil || none.Search("gba", "x", 1) != nil {
		t.Error("a nil index answered")
	}
}
