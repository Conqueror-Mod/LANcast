package diskuse

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func layout(dir string) Layout {
	return Layout{
		DataDir:  dir,
		ByName:   map[string]string{"lancast.db": "database", "lancast.db-wal": "database", "artwork": "artwork", "previews": "previews"},
		ByPrefix: map[string]string{"lancastd": "logs"},
		Order:    []string{"database", "artwork", "previews", "logs", "optimized"},
	}
}

func byID(r Report) map[string]Part {
	out := map[string]Part{}
	for _, p := range r.Parts {
		out[p.ID] = p
	}
	return out
}

func TestEveryFileIsCountedOnceAgainstItsPart(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "lancast.db"), 1000)
	write(t, filepath.Join(dir, "lancast.db-wal"), 200)
	write(t, filepath.Join(dir, "artwork", "ab", "abcd.jpg"), 300)
	write(t, filepath.Join(dir, "artwork", "cd", "cdef.jpg"), 400)
	write(t, filepath.Join(dir, "lancastd.log"), 50)
	write(t, filepath.Join(dir, "lancastd.log.1"), 60)
	write(t, filepath.Join(dir, "lancast.db.moved-20261019"), 900)
	write(t, filepath.Join(dir, "pre-rev54", "lancast.db"), 700)
	write(t, filepath.Join(dir, "notes.txt"), 5)

	r := Measure(context.Background(), layout(dir))
	p := byID(r)
	for id, want := range map[string][2]int64{
		"database": {1200, 2}, "artwork": {700, 2}, "logs": {110, 2}, "other": {1605, 3}, "previews": {0, 0},
	} {
		if p[id].Bytes != want[0] || p[id].Files != want[1] {
			t.Errorf("%s = %d bytes in %d files, want %d in %d", id, p[id].Bytes, p[id].Files, want[0], want[1])
		}
	}
	if r.TotalBytes != 1200+700+110+1605 {
		t.Errorf("total = %d", r.TotalBytes)
	}
	// "lancast.db.moved-…" is not the database: only exact names are.
	if got := p["other"].Largest; len(got) != 3 || got[0].Name != "lancast.db.moved-20261019" || got[1].Name != "pre-rev54" {
		t.Errorf("other's largest = %+v, want the moved database, then the old folder", got)
	}
	// Every listed part is reported, in order, with other last.
	var ids []string
	for _, part := range r.Parts {
		ids = append(ids, part.ID)
	}
	if want := []string{"database", "artwork", "previews", "logs", "optimized", "other"}; len(ids) != len(want) || ids[len(ids)-1] != "other" {
		t.Errorf("parts = %v, want %v", ids, want)
	}
	if r.DiskTotal == 0 {
		t.Error("the drive's size was not reported")
	}
}

func TestAFolderOnAnotherDriveIsWalkedOnItsOwn(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	write(t, filepath.Join(elsewhere, "123.mp4"), 5000)
	l := layout(dir)
	l.Elsewhere = map[string]string{"optimized": elsewhere}
	p := byID(Measure(context.Background(), l))
	if p["optimized"].Bytes != 5000 || !p["optimized"].Outside || p["optimized"].Path != elsewhere {
		t.Errorf("optimized = %+v, want 5000 bytes outside, at %s", p["optimized"], elsewhere)
	}
}

func TestAFolderInsideIsNotCountedTwice(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "optimized", "1.mp4"), 800)
	l := layout(dir)
	l.ByName["optimized"] = "optimized"
	l.Elsewhere = map[string]string{"optimized": filepath.Join(dir, "optimized")}
	r := Measure(context.Background(), l)
	if p := byID(r)["optimized"]; p.Bytes != 800 || p.Outside {
		t.Errorf("optimized = %+v, want 800 bytes, counted once, inside", p)
	}
	if r.TotalBytes != 800 {
		t.Errorf("total = %d, want 800", r.TotalBytes)
	}
}

func TestAStoppedWalkSaysItIsPartial(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "artwork", "a.jpg"), 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := Measure(ctx, layout(dir)); !r.Partial {
		t.Error("a cancelled walk was reported as complete")
	}
}

func TestInsideComparesAsWindowsDoes(t *testing.T) {
	for _, c := range []struct {
		dir, path string
		want      bool
	}{
		{`C:\Data`, `C:\Data\optimized`, true},
		{`C:\Data`, `c:\data\Optimized`, true},
		{`C:\Data`, `C:\Data`, true},
		{`C:\Data`, `C:\Database`, false},
		{`C:\Data`, `D:\optimized`, false},
	} {
		if filepath.Separator != '\\' {
			continue // Windows paths; the rule is the same on a forward-slash system
		}
		if got := inside(c.dir, c.path); got != c.want {
			t.Errorf("inside(%q, %q) = %v, want %v", c.dir, c.path, got, c.want)
		}
	}
}
