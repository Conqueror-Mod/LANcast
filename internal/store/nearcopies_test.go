package store

import (
	"context"
	"testing"
)

/*
 * Near copies (ADR 0075, 2026-10-06 amendment).
 *
 * The rule was measured on a real library before it was built; these pin each
 * of its four conditions, so a change to one shows up as the case it was
 * there to exclude: a burst, a screenshot, a crop, an edit.
 */

// lean returns a unit vector whose cosine with base is exactly cos, in
// 4 dimensions: enough to set the cosine between two photos exactly.
func lean(cos float64) []float32 {
	// (cos, sin, 0, 0) against (1, 0, 0, 0) has cosine cos.
	s := 1 - cos*cos
	if s < 0 {
		s = 0
	}
	return []float32{float32(cos), float32(sqrt(s)), 0, 0}
}

func sqrt(x float64) float64 {
	z := x
	for i := 0; i < 30 && z > 0; i++ {
		z = (z + x/z) / 2
	}
	return z
}

var base = []float32{1, 0, 0, 0}

func cand(id int64, w, h int, dhash uint64, emb []float32) nearCandidate {
	return nearCandidate{id: id, w: w, h: h, dhash: dhash, emb: emb, path: string(rune('a' + id))}
}

func candIDs(g []nearCandidate) []int64 {
	out := []int64{}
	for _, c := range g {
		out = append(out, c.id)
	}
	return out
}

func TestAResizedCopyIsANearCopyAndTheLargerIsKept(t *testing.T) {
	small := cand(1, 720, 540, 0b1011, base)
	large := cand(2, 3648, 2736, 0b1011, lean(0.95))
	groups := nearCopyGroups([]nearCandidate{small, large})
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if got := candIDs(groups[0]); got[0] != 2 {
		t.Errorf("order = %v, want the 3648-pixel copy first: it is the one to keep", got)
	}
}

func TestEachConditionExcludesWhatItWasMeasuredAgainst(t *testing.T) {
	cases := []struct {
		name string
		a, b nearCandidate
	}{
		{"embedding too far: an edit", cand(1, 800, 600, 0, base), cand(2, 800, 600, 0, lean(0.92))},
		{"hash too far: a crop", cand(1, 800, 600, 0, base), cand(2, 800, 600, 0x1FF, lean(0.99))},
		{"both screenshot-sized", cand(1, 1920, 1080, 0, base), cand(2, 1920, 1080, 0, lean(0.99))},
	}
	for _, c := range cases {
		if g := nearCopyGroups([]nearCandidate{c.a, c.b}); len(g) != 0 {
			t.Errorf("%s: grouped %v", c.name, g)
		}
	}

	// A burst: as close as a re-save in both signals, taken a second apart.
	a, b := cand(1, 4160, 3120, 0, base), cand(2, 4160, 3120, 0, lean(0.99))
	a.taken, b.taken = 1_700_000_000, 1_700_000_001
	if g := nearCopyGroups([]nearCandidate{a, b}); len(g) != 0 {
		t.Errorf("a burst was grouped: %v", g)
	}
	// The same pair with one capture time unknown is a re-save that lost its
	// EXIF, and is grouped; so is one with the same time.
	b.taken = 0
	if g := nearCopyGroups([]nearCandidate{a, b}); len(g) != 1 {
		t.Errorf("an unknown capture time should not exclude a copy")
	}
	b.taken = a.taken
	if g := nearCopyGroups([]nearCandidate{a, b}); len(g) != 1 {
		t.Errorf("the same capture time should not exclude a copy")
	}

	// The edges are inclusive: exactly 8 bits, exactly 0.93.
	e1, e2 := cand(1, 800, 600, 0, base), cand(2, 640, 480, 0xFF, lean(NearCopyMinCosine+1e-6))
	if g := nearCopyGroups([]nearCandidate{e1, e2}); len(g) != 1 {
		t.Errorf("8 bits and cosine 0.93 should be a near copy")
	}
}

// Exact copies are the Duplicates list's, and are not paired with each other.
// A resized copy of either joins both, so the group shows all three.
func TestExactCopiesAreNotPairedButJoinThroughAResize(t *testing.T) {
	a, b := cand(1, 800, 600, 0, base), cand(2, 800, 600, 0, base)
	a.sha, b.sha = "same", "same"
	if g := nearCopyGroups([]nearCandidate{a, b}); len(g) != 0 {
		t.Errorf("two exact copies made a near-copy group: %v", g)
	}
	small := cand(3, 400, 300, 1, lean(0.97))
	g := nearCopyGroups([]nearCandidate{a, b, small})
	if len(g) != 1 || len(g[0]) != 3 {
		t.Errorf("groups = %v, want one group of all three", g)
	}
}

// A chain of near copies is one group, not two overlapping ones.
func TestNearCopiesJoinTransitively(t *testing.T) {
	g := nearCopyGroups([]nearCandidate{
		cand(1, 1000, 1000, 0, base),
		cand(2, 500, 500, 0, lean(0.95)),
		cand(3, 250, 250, 0, lean(0.95)),
		cand(4, 900, 900, 0xFFFF, lean(0.10)), // unrelated
	})
	if len(g) != 1 || len(g[0]) != 3 {
		t.Fatalf("groups = %v, want one group of three", g)
	}
}

// nearPhoto inserts a photo with everything the rule reads.
func nearPhoto(t *testing.T, s *Store, sh shots, name string, w, h int, dhash uint64, emb []float32) int64 {
	t.Helper()
	ctx := context.Background()
	id := sh.photo(t, s, name, nil, nil)
	// The digest first: hashPhoto records metadata, and sizes written before it
	// would be overwritten, leaving the "largest" decided by file name.
	hashPhoto(t, s, id, name)
	if _, err := s.db.Exec(`UPDATE media_item SET width = ?, height = ? WHERE id = ?`, w, h, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPhotoDHash(ctx, id, dhash); err != nil {
		t.Fatal(err)
	}
	if emb != nil {
		if err := s.SavePhotoEmbedding(ctx, id, "clip", emb); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestPhotoNearCopiesFromTheStore(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	big := nearPhoto(t, s, sh, "z-big.jpg", 3648, 2736, 0x8000_0000_0000_0001, base)
	small := nearPhoto(t, s, sh, "small.jpg", 720, 540, 0x8000_0000_0000_0001, lean(0.96))
	nearPhoto(t, s, sh, "notindexed.jpg", 720, 540, 0, nil)
	marked := nearPhoto(t, s, sh, "marked.jpg", 720, 540, 0x8000_0000_0000_0001, base)
	if err := s.SetSensitive(ctx, marked, true); err != nil {
		t.Fatal(err)
	}

	got, err := s.PhotoNearCopies(ctx, sh.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Copies) != 2 {
		t.Fatalf("groups = %+v, want one pair (the marked twin takes no part)", got.Groups)
	}
	g := got.Groups[0]
	if g.Keep != big || g.Copies[0].Item.ID != big || g.Copies[1].Item.ID != small {
		t.Errorf("keep %d, order %d,%d; want %d first and kept", g.Keep, g.Copies[0].Item.ID, g.Copies[1].Item.ID, big)
	}
	if got.ExtraCopies != 1 {
		t.Errorf("extra copies = %d, want 1", got.ExtraCopies)
	}
	// The photo with no embedding could not be compared, and says so. A hash
	// with its top bit set survived the round trip through a signed column.
	if got.Pending != 1 {
		t.Errorf("pending = %d, want 1 (the photo not indexed for search)", got.Pending)
	}
}

// Revision 60 sends every photo back through the worker so existing ones get a
// hash.
func TestRevision60QueuesEveryPhotoForAHash(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	id := sh.photo(t, s, `a.jpg`, nil, nil)
	if err := s.MarkArtworkChecked(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE meta SET value = '59' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pending, err := s.PendingPhotos(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("pending = %d photos, want the one already thumbnailed queued for its hash", len(pending))
	}
}
