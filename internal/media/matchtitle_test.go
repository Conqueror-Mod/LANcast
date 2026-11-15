package media

import "testing"

func TestMatchTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		// Latin titles come out as the old a-to-z normalizer made them.
		{"The Matrix", "matrix"},
		{"Ocean's Eleven", "oceans eleven"},
		{"Spider-Man: No Way Home", "spiderman no way home"},
		{"WALL·E", "walle"},
		{"A.I. Artificial Intelligence", "ai artificial intelligence"},
		{"  Lots   of\tspace  ", "lots of space"},
		// Accents fold; they used to be dropped, letter and all.
		{"Léon", "leon"},
		{"Amélie", "amelie"},
		{"Y tu mamá también", "y tu mama tambien"},
		{"Das Boot ist Übervoll", "das boot ist ubervoll"},
		// Every script keeps its letters (R08-3).
		{"七人の侍", "七人の侍"},
		{"千と千尋の神隠し", "千と千尋の神隠し"},
		{"Брат", "брат"},
		{"Брат 2", "брат 2"},
		{"Ταξίδι στα Κύθηρα", "ταξιδι στα κυθηρα"},
		{"ΤΑΞΙΔΙ ΣΤΑ ΚΥΘΗΡΑ", "ταξιδι στα κυθηρα"},
		{"Ο Θίασος", "ο θιασοσ"},
		{"الرسالة", "الرسالة"},
		// Outside Latin and Greek a mark is part of the letter, not an accent.
		{"ガンダム", "ガンダム"},
		{"गाइड", "गाइड"},
		// Nothing a title could be made of survives as nothing.
		{"!!!", ""},
	}
	for _, tt := range tests {
		if got := MatchTitle(tt.in); got != tt.want {
			t.Errorf("MatchTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Folding accents is for matching only: SortTitle, which orders every grid,
// must not change with it.
func TestMatchTitleLeavesSortTitleAlone(t *testing.T) {
	if got := SortTitle("Léon"); got != "léon" {
		t.Errorf("SortTitle(Léon) = %q, want léon", got)
	}
}
