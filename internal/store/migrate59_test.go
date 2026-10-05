package store

import (
	"context"
	"testing"
)

// A face found in the display copy says so, and one found in the file reads
// back with no frame, as every row written before revision 59 does.
func TestAFaceKeepsTheFrameItWasMeasuredIn(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	file := sh.photo(t, s, `upright.jpg`, nil, nil)
	shown := sh.photo(t, s, `sideways.heic`, nil, nil)

	emb := []float32{1, 0}
	if err := s.RecordFaces(ctx, file, []Face{{X: 1, Y: 2, W: 3, H: 4, Score: 0.9, Embedding: emb}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFaces(ctx, shown, []Face{{X: 1, Y: 2, W: 3, H: 4, Score: 0.9, Embedding: emb, Frame: FrameDisplay}}); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		item int64
		want string
	}{{file, ""}, {shown, FrameDisplay}} {
		var id int64
		if err := s.db.QueryRow(`SELECT id FROM face WHERE item_id = ?`, c.item).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f, _, err := s.GetFace(ctx, id, "")
		if err != nil {
			t.Fatal(err)
		}
		if f.Frame != c.want {
			t.Errorf("item %d: frame = %q, want %q", c.item, f.Frame, c.want)
		}
	}

	// The file's own frame is stored as NULL, the value every older row has,
	// so there is one way of saying it rather than two.
	var nulls int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM face WHERE frame IS NULL`).Scan(&nulls)
	if nulls != 1 {
		t.Errorf("rows with NULL frame = %d, want 1", nulls)
	}
}

// Revision 59 sends back the photos with no face found, and only those: a
// photo with faces has groups, names and rejections hanging from it.
func TestRevision59RequeuesOnlyPhotosWithNoFaces(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	empty := sh.photo(t, s, `IMG_0706.HEIC`, nil, nil)
	withFace := sh.photo(t, s, `group.jpg`, nil, nil)
	if err := s.RecordFaces(ctx, withFace, []Face{{X: 1, Y: 2, W: 3, H: 4, Score: 0.9, Embedding: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{empty, withFace} {
		if err := s.MarkFacesDone(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.db.Exec(`UPDATE meta SET value = '58' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pending, err := s.PendingFaces(ctx, sh.library, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != empty {
		ids := []int64{}
		for _, p := range pending {
			ids = append(ids, p.ID)
		}
		t.Errorf("pending = %v, want only %d (the photo with no face found)", ids, empty)
	}
}
