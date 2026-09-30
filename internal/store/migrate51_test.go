package store

import (
	"context"
	"testing"
)

/*
 * Revision 51 — films probed before the stretched-audio fix are probed again.
 *
 * The numbers are Public Enemies (2009).mp4's shape: a 2h20 picture in a file
 * whose container claimed 20,073s because one audio track ran on. Its stored
 * length makes the file's average bitrate far lower than its own video
 * stream's, which is impossible for a file that contains that stream.
 */

type probedFile struct {
	kind       string
	sizeBytes  int64
	durationMS int64
	videoBPS   int64 // 0 stores NULL
}

func seedProbed(t *testing.T, st *Store, lib *Library, name string, f probedFile) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: lib.Path + "/" + name, Kind: f.kind,
		Title: name, SortTitle: name, Container: "mp4", SizeBytes: f.sizeBytes, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var vbr any
	if f.videoBPS > 0 {
		vbr = f.videoBPS
	}
	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET probed_at = 1, duration_ms = ?, video_bitrate = ? WHERE id = ?`,
		f.durationMS, vbr, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func pendingProbeIDs(t *testing.T, st *Store) map[int64]bool {
	t.Helper()
	items, err := st.PendingProbe(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]bool{}
	for _, it := range items {
		out[it.ID] = true
	}
	return out
}

func TestRevision51RequeuesAFilmLongerThanItsOwnPicture(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// 3.5 Mb/s of picture for 8,383s, plus audio: about 3.9 GB.
	const video = 3_500_000
	size := int64(8_383) * 3_800_000 / 8

	stretched := seedProbed(t, st, lib, "Public Enemies (2009).mp4",
		probedFile{kind: "movie", sizeBytes: size, durationMS: 20_073_000, videoBPS: video})
	honest := seedProbed(t, st, lib, "Honest.mp4",
		probedFile{kind: "movie", sizeBytes: size, durationMS: 8_383_000, videoBPS: video})
	// A track running a few seconds past the picture is the ordinary
	// disagreement and must not cost a re-probe.
	overrun := seedProbed(t, st, lib, "Overrun.mp4",
		probedFile{kind: "movie", sizeBytes: size, durationMS: 8_390_000, videoBPS: video})
	// No stated video bitrate: nothing to compare, and nothing the probe
	// could correct.
	unknown := seedProbed(t, st, lib, "Unknown.mkv",
		probedFile{kind: "movie", sizeBytes: size, durationMS: 20_073_000})

	rewindTo50(t, st)
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	queued := pendingProbeIDs(t, st)
	if !queued[stretched] {
		t.Error("a film whose stored length is far past its picture was not re-queued, " +
			"so it keeps the length it was given before the fix")
	}
	for name, id := range map[string]int64{"honest": honest, "overrun": overrun, "unknown": unknown} {
		if queued[id] {
			t.Errorf("the %s film was re-queued; only stretched files should cost a re-probe", name)
		}
	}
}

func TestRevision51LeavesMusicAlone(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Cover art can carry a bitrate; a track must not be judged by it.
	id := seedProbed(t, st, lib, "track.m4a",
		probedFile{kind: "track", sizeBytes: 5_000_000, durationMS: 240_000, videoBPS: 900_000_000})

	rewindTo50(t, st)
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if pendingProbeIDs(t, st)[id] {
		t.Error("a music track was re-queued by a migration about films")
	}
}

// rewindTo50 winds the number back and undoes what later revisions added, so
// replaying from 50 does not re-add a column that is already there.
func rewindTo50(t *testing.T, st *Store) {
	t.Helper()
	if _, err := st.db.Exec(`ALTER TABLE user DROP COLUMN avatar`); err != nil { // revision 52
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE meta SET value = '50' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
}
