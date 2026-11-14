package scan

import (
	"context"
	"os"
	"testing"
	"time"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * The locked-fields rule, through a real rescan (R04-1).
 *
 * CLAUDE.md: "No provider refresh, rescan, or merge may touch a locked field."
 * TestLockedFieldSurvivesEnrichment (internal/enrich) holds the refresh half
 * end to end; this holds the rescan half. The cheap rescan of an untouched
 * file never reaches the database, so the two paths that do are the ones
 * tested: a file that was marked missing and comes back, and a file whose size
 * and mtime changed. Both used to rewrite the locked title from the filename.
 */

func rescanTriggers() []struct {
	name  string
	apply func(t *testing.T, sc *Scanner, lib store.Library, root, path string)
} {
	return []struct {
		name  string
		apply func(t *testing.T, sc *Scanner, lib store.Library, root, path string)
	}{
		{"missing then back", func(t *testing.T, sc *Scanner, lib store.Library, root, path string) {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// The drive is unplugged for one scan...
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			scanAndWait(t, sc, lib)
			// ...and the same bytes come back with the same time.
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		}},
		{"size and mtime changed", func(t *testing.T, sc *Scanner, lib store.Library, root, path string) {
			if err := os.WriteFile(path, make([]byte, 999), 0o644); err != nil {
				t.Fatal(err)
			}
			future := time.Now().Add(2 * time.Second)
			if err := os.Chtimes(path, future, future); err != nil {
				t.Fatal(err)
			}
		}},
	}
}

func TestARescanNeverOverwritesALockedField(t *testing.T) {
	for _, tr := range rescanTriggers() {
		t.Run(tr.name, func(t *testing.T) {
			ctx := context.Background()
			sc, st := newScanner(t)
			lib, root := fixture(t, sc, st)
			path := writeFile(t, root, "alien.1979.mkv", 10)

			scanAndWait(t, sc, lib)
			known, _ := st.KnownFiles(ctx, lib.ID)
			id := known[path].ID

			// A person corrects the title and year; the edit locks them.
			title, year := "Alien (Director's Cut, my edit)", 2003
			if err := st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Title: &title, Year: &year}); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{meta.FieldTitle, meta.FieldYear} {
				if err := st.LockField(ctx, id, f); err != nil {
					t.Fatal(err)
				}
			}

			tr.apply(t, sc, lib, root, path)
			p := scanAndWait(t, sc, lib)
			if p.ItemsChanged != 1 {
				t.Fatalf("ItemsChanged = %d, want 1 — the rescan never reached the upsert, so this proves nothing",
					p.ItemsChanged)
			}

			it, err := st.GetItem(ctx, id, "local")
			if err != nil {
				t.Fatal(err)
			}
			if it.Title != title {
				t.Errorf("locked title overwritten by a rescan: now %q", it.Title)
			}
			if it.Year == nil || *it.Year != year {
				t.Errorf("locked year overwritten by a rescan: now %v", yearOf(it.Year))
			}
			if it.Missing {
				t.Error("Missing = true after the rescan, want false")
			}
		})
	}
}

// A provider's match is unlocked field by field but settled as identity: a
// rescan that only sees the file again must not hand it back to the filename.
func TestARescanKeepsAMatchedTitle(t *testing.T) {
	for _, tr := range rescanTriggers() {
		t.Run(tr.name, func(t *testing.T) {
			ctx := context.Background()
			sc, st := newScanner(t)
			lib, root := fixture(t, sc, st)
			path := writeFile(t, root, "the.matrx.mkv", 10)

			scanAndWait(t, sc, lib)
			known, _ := st.KnownFiles(ctx, lib.ID)
			id := known[path].ID

			title, year := "The Matrix", 1999
			provider, ext, state := "tmdb", "603", meta.StateMatched
			if err := st.UpdateItemMetadata(ctx, id, store.ItemMetadata{
				Title: &title, Year: &year, Provider: &provider, ExternalID: &ext, MatchState: &state,
			}); err != nil {
				t.Fatal(err)
			}

			tr.apply(t, sc, lib, root, path)
			scanAndWait(t, sc, lib)

			it, err := st.GetItem(ctx, id, "local")
			if err != nil {
				t.Fatal(err)
			}
			if it.Title != title || it.Year == nil || *it.Year != year {
				t.Errorf("matched identity re-guessed by a rescan: %q / %v", it.Title, yearOf(it.Year))
			}
		})
	}
}

func yearOf(y *int) any {
	if y == nil {
		return "NULL"
	}
	return *y
}
