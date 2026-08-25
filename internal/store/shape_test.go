package store

import "testing"

func TestShapeUnsettled(t *testing.T) {
	parent := int64(7)
	cases := []struct {
		name        string
		libraryKind string
		item        Item
		want        bool
	}{
		/*
		 * The trap. A file that lost its EP1 marker parses as a film and lands
		 * top-level in a shows library; confirming a match there locks a wrong
		 * identity onto a row that is in the wrong place.
		 */
		{"parentless film in a shows library", "show",
			Item{Kind: "movie", ParentID: nil}, true},

		/*
		 * The refusals, and they matter as much. A shows library legitimately
		 * holds loose files -- an extras folder, a documentary beside a series
		 * -- so this must not become "a shows library contains a film", which
		 * would refuse to lock ordinary correct rows and make Fix match feel
		 * broken on the very libraries it is for.
		 */
		{"film with a parent", "show",
			Item{Kind: "movie", ParentID: &parent}, false},
		{"episode", "show",
			Item{Kind: "episode", ParentID: &parent}, false},
		{"parentless episode", "show",
			Item{Kind: "episode", ParentID: nil}, false},
		{"show row itself", "show",
			Item{Kind: "show", ParentID: nil}, false},

		// A film in a film library is a film. Nothing to settle.
		{"parentless film in a film library", "movie",
			Item{Kind: "movie", ParentID: nil}, false},
		{"parentless film in a music library", "music",
			Item{Kind: "movie", ParentID: nil}, false},
	}
	for _, c := range cases {
		if got := ShapeUnsettled(c.libraryKind, c.item); got != c.want {
			t.Errorf("%s: ShapeUnsettled = %v, want %v", c.name, got, c.want)
		}
	}
}
