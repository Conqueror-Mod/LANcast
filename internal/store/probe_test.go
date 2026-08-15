package store

import (
	"context"
	"testing"
)

func sampleProbe() ProbeResult {
	return ProbeResult{
		DurationMS: 7284512,
		Container:  "matroska",
		VideoCodec: "h264", VideoProfile: "High",
		Width: 1920, Height: 1080, VideoBitRate: 8_000_000,
		AudioCodec: "eac3", AudioChannels: 6,
		Streams: []MediaStream{
			{Index: 0, Kind: "video", Codec: "h264", Profile: "High", Width: 1920, Height: 1080, Default: true},
			{Index: 1, Kind: "audio", Codec: "eac3", Channels: 6, Language: "eng", Default: true},
			{Index: 2, Kind: "subtitle", Codec: "subrip", Language: "eng"},
			{Index: 3, Kind: "subtitle", Codec: "subrip", Language: "fre", Forced: true},
		},
	}
}

func TestSaveAndReadProbe(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	if err := st.SaveProbe(ctx, id, sampleProbe()); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.ProbedAt == nil {
		t.Fatal("ProbedAt is nil after a successful probe")
	}
	if it.DurationMS == nil || *it.DurationMS != 7284512 {
		t.Errorf("DurationMS = %v", it.DurationMS)
	}
	if it.VideoCodec == nil || *it.VideoCodec != "h264" {
		t.Errorf("VideoCodec = %v", it.VideoCodec)
	}
	if it.Height == nil || *it.Height != 1080 {
		t.Errorf("Height = %v", it.Height)
	}
	if it.AudioCodec == nil || *it.AudioCodec != "eac3" {
		t.Errorf("AudioCodec = %v", it.AudioCodec)
	}

	streams, err := st.Streams(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 4 {
		t.Fatalf("streams = %d, want 4", len(streams))
	}
	if streams[1].Language != "eng" || !streams[1].Default {
		t.Errorf("audio stream = %+v", streams[1])
	}
	if !streams[3].Forced {
		t.Error("the forced flag did not survive storage")
	}
}

// Re-probing must replace the stream list, not accumulate rows.
func TestSaveProbeReplacesStreams(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	st.SaveProbe(ctx, id, sampleProbe())

	shorter := sampleProbe()
	shorter.Streams = shorter.Streams[:2]
	if err := st.SaveProbe(ctx, id, shorter); err != nil {
		t.Fatal(err)
	}

	streams, _ := st.Streams(ctx, id)
	if len(streams) != 2 {
		t.Errorf("streams = %d after re-probe, want 2", len(streams))
	}
}

func TestProbeQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	a := seedItem(t, st, lib, `C:\m\a.mkv`)
	seedItem(t, st, lib, `C:\m\b.mkv`)

	pending, err := st.PendingProbe(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2", len(pending))
	}
	if n, _ := st.PendingProbeCount(ctx); n != 2 {
		t.Errorf("PendingProbeCount = %d, want 2", n)
	}

	st.SaveProbe(ctx, a, sampleProbe())

	pending, _ = st.PendingProbe(ctx, 10)
	if len(pending) != 1 {
		t.Errorf("pending after probing one = %d, want 1", len(pending))
	}
}

// A file ffprobe cannot read must leave the queue, or one corrupt file
// re-probes on every pass forever and the queue never drains.
func TestMarkProbeFailedDrainsQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\broken.mkv`)

	if err := st.MarkProbeFailed(ctx, id); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.PendingProbeCount(ctx); n != 0 {
		t.Errorf("PendingProbeCount = %d, want 0 — a failed probe must not requeue forever", n)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.ProbedAt == nil {
		t.Error("ProbedAt is nil after MarkProbeFailed")
	}
	if it.VideoCodec != nil {
		t.Error("a failed probe recorded codec data")
	}
}

// A changed file invalidates its probe: the stored codecs describe bytes that
// no longer exist.
func TestChangedFileClearsProbe(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	path := `C:\m\a.mkv`
	id := seedItem(t, st, lib, path)

	st.SaveProbe(ctx, id, sampleProbe())
	if it, _ := st.GetItem(ctx, id, "local"); it.ProbedAt == nil {
		t.Fatal("test setup: probe was not saved")
	}

	// The scanner upserts only when size or mtime changed.
	changed := file(lib.ID, path, "Seed")
	changed.SizeBytes = 999999
	changed.MTime = 12345
	if _, err := st.UpsertItem(ctx, changed); err != nil {
		t.Fatal(err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.ProbedAt != nil {
		t.Error("ProbedAt survived a file change; the stored probe describes different bytes")
	}
	if n, _ := st.PendingProbeCount(ctx); n != 1 {
		t.Errorf("PendingProbeCount = %d, want the changed file requeued", n)
	}
}

// Unknown must stay distinct from genuinely zero — a UI showing "0 kbps" for
// an unprobed file is worse than showing nothing.
func TestZeroValuesStoredAsNull(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\audio.flac`)

	st.SaveProbe(ctx, id, ProbeResult{
		DurationMS: 240000,
		Container:  "flac",
		AudioCodec: "flac", AudioChannels: 2,
		Streams: []MediaStream{{Index: 0, Kind: "audio", Codec: "flac", Channels: 2, Default: true}},
	})

	it, _ := st.GetItem(ctx, id, "local")
	if it.VideoCodec != nil {
		t.Errorf("VideoCodec = %v, want nil for an audio-only file", it.VideoCodec)
	}
	if it.Width != nil || it.Height != nil {
		t.Errorf("dimensions = %v x %v, want nil", it.Width, it.Height)
	}
	if it.AudioChannels == nil || *it.AudioChannels != 2 {
		t.Errorf("AudioChannels = %v", it.AudioChannels)
	}
}

func TestStreamsCascadeOnDelete(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)
	st.SaveProbe(ctx, id, sampleProbe())

	if _, err := st.db.ExecContext(ctx, `DELETE FROM media_item WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	var n int
	st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_stream WHERE item_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("%d orphaned stream rows remain", n)
	}
}

// pix_fmt is what decides whether a 10-bit H.264 file direct-plays, so it must
// survive storage rather than being dropped on the way in.
func TestPixFmtRoundTrips(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\tenbit.mkv`)

	p := sampleProbe()
	p.Streams[0].PixFmt = "yuv420p10le"
	if err := st.SaveProbe(ctx, id, p); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}

	streams, err := st.Streams(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if streams[0].PixFmt != "yuv420p10le" {
		t.Errorf("PixFmt = %q, want yuv420p10le", streams[0].PixFmt)
	}
}

// ClearProbe puts items back in the pending queue. The stream rows stay: the
// window where an item has no codec information at all is the one where every
// playback decision for it silently falls back to direct play.
func TestClearProbeRequeuesWithoutLosingStreams(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	if err := st.SaveProbe(ctx, id, sampleProbe()); err != nil {
		t.Fatal(err)
	}
	if pending, _ := st.PendingProbe(ctx, 10); len(pending) != 0 {
		t.Fatalf("pending = %d before clearing, want 0", len(pending))
	}

	n, err := st.ClearProbe(ctx, 0)
	if err != nil {
		t.Fatalf("ClearProbe: %v", err)
	}
	if n != 1 {
		t.Errorf("ClearProbe queued %d, want 1", n)
	}

	pending, err := st.PendingProbe(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Errorf("pending = %+v, want the cleared item", pending)
	}
	if streams, _ := st.Streams(ctx, id); len(streams) != 4 {
		t.Errorf("streams = %d after clearing, want the 4 still there", len(streams))
	}

	// Nothing left to clear on a second pass.
	if n, _ := st.ClearProbe(ctx, 0); n != 0 {
		t.Errorf("second ClearProbe queued %d, want 0", n)
	}
}

func TestClearProbeScopedToOneLibrary(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	other, err := st.CreateLibrary(ctx, "Other", "movie", `C:\other`)
	if err != nil {
		t.Fatal(err)
	}

	a := seedItem(t, st, lib, `C:\m\a.mkv`)
	b := seedItem(t, st, other, `C:\other\b.mkv`)
	for _, id := range []int64{a, b} {
		if err := st.SaveProbe(ctx, id, sampleProbe()); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.ClearProbe(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ClearProbe queued %d, want only the other library's item", n)
	}
	pending, _ := st.PendingProbe(ctx, 10)
	if len(pending) != 1 || pending[0].ID != b {
		t.Errorf("pending = %+v, want only the item from the named library", pending)
	}
}

// The targeted case: re-probe only what a current build would learn something
// from. Re-probing a whole library is hours of ffprobe.
func TestClearIncompleteProbeOnlyTouchesItemsMissingPixFmt(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	old := seedItem(t, st, lib, `C:\m\old.mkv`) // probed before pix_fmt existed
	fresh := seedItem(t, st, lib, `C:\m\fresh.mkv`)

	if err := st.SaveProbe(ctx, old, sampleProbe()); err != nil {
		t.Fatal(err)
	}
	p := sampleProbe()
	p.Streams[0].PixFmt = "yuv420p"
	if err := st.SaveProbe(ctx, fresh, p); err != nil {
		t.Fatal(err)
	}

	n, err := st.ClearIncompleteProbe(ctx)
	if err != nil {
		t.Fatalf("ClearIncompleteProbe: %v", err)
	}
	if n != 1 {
		t.Fatalf("queued %d, want only the item missing pix_fmt", n)
	}

	pending, _ := st.PendingProbe(ctx, 10)
	if len(pending) != 1 || pending[0].ID != old {
		t.Errorf("pending = %+v, want only the pre-pix_fmt item", pending)
	}
}

// An audio-only file has no video stream, so it has no pix_fmt to be missing
// and must not be re-probed forever.
func TestClearIncompleteProbeIgnoresFilesWithNoVideo(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\audio.m4a`)

	err := st.SaveProbe(ctx, id, ProbeResult{
		DurationMS: 200_000, Container: "mov",
		AudioCodec: "aac", AudioChannels: 2,
		Streams: []MediaStream{
			{Index: 0, Kind: "audio", Codec: "aac", Channels: 2, Default: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if n, _ := st.ClearIncompleteProbe(ctx); n != 0 {
		t.Errorf("queued %d, want 0 — there is no video stream to learn about", n)
	}
}

// A locked field is never overwritten, by any write path (ADR 0008). Editing a
// track's title and then rescanning must not undo the edit — a rescan
// reconciles files, it does not re-litigate identity.
func TestApplyTrackTagsRespectsLocks(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\01 track.mp3`)

	// An operator edited the title, which locks it.
	title := "My Own Title"
	if err := st.UpdateItemMetadata(ctx, id, ItemMetadata{Title: &title}); err != nil {
		t.Fatalf("UpdateItemMetadata: %v", err)
	}
	if err := st.LockField(ctx, id, "title"); err != nil {
		t.Fatalf("LockField: %v", err)
	}

	err := st.ApplyTrackTags(ctx, id, TrackTags{
		Title: "Tagged Title", SortTitle: "tagged title",
		Album: "An Album", Track: 4,
	})
	if err != nil {
		t.Fatalf("ApplyTrackTags: %v", err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "My Own Title" {
		t.Errorf("Title = %q — a locked field was overwritten by a rescan", it.Title)
	}
	// Unlocked fields still take the tag.
	if it.Series == nil || *it.Series != "An Album" {
		t.Errorf("album = %v, want the tag to apply to unlocked fields", it.Series)
	}
	if it.Episode == nil || *it.Episode != 4 {
		t.Errorf("track = %v, want 4", it.Episode)
	}
}

// Empty values never overwrite: a tagger that filled in a title but left the
// album blank must not erase an album a folder name supplied.
func TestApplyTrackTagsEmptyValuesDoNotErase(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\02 track.mp3`)

	if err := st.ApplyTrackTags(ctx, id, TrackTags{
		Title: "First", SortTitle: "first", Album: "Album A", Track: 2, Year: 1999,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyTrackTags(ctx, id, TrackTags{
		Title: "Second", SortTitle: "second", // nothing else
	}); err != nil {
		t.Fatal(err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Second" {
		t.Errorf("Title = %q, want the newer tag", it.Title)
	}
	if it.Series == nil || *it.Series != "Album A" {
		t.Errorf("album = %v — a blank tag erased a value that was already there", it.Series)
	}
	if it.Year == nil || *it.Year != 1999 {
		t.Errorf("Year = %v — a blank tag erased it", it.Year)
	}
}

// Colour has to survive the round trip, because it is the only thing that tells
// an HDR file from a 10-bit SDR one (ADR 0033). pix_fmt cannot: yuv420p10le is
// what both report.
func TestProbeRoundTripsColourMetadata(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\hdr.mkv`)

	if err := st.SaveProbe(ctx, id, ProbeResult{
		DurationMS: 1000, Container: "matroska",
		Streams: []MediaStream{{
			Index: 0, Kind: "video", Codec: "hevc", PixFmt: "yuv420p10le",
			ColorTransfer: "smpte2084", ColorPrimaries: "bt2020", ColorSpace: "bt2020nc",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.Streams(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("streams = %d, want 1", len(got))
	}
	if got[0].ColorTransfer != "smpte2084" {
		t.Errorf("ColorTransfer = %q, want smpte2084", got[0].ColorTransfer)
	}
	if got[0].ColorPrimaries != "bt2020" || got[0].ColorSpace != "bt2020nc" {
		t.Errorf("primaries/space = %q/%q", got[0].ColorPrimaries, got[0].ColorSpace)
	}
}

// A file with no colour metadata is not an error and not HDR. Every row
// predates revision 19 until re-probed, and the empty string has to read back
// cleanly rather than as a scan failure.
func TestProbeWithoutColourMetadataReadsBackEmpty(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\sdr.mp4`)
	if err := st.SaveProbe(ctx, id, ProbeResult{
		DurationMS: 1000, Container: "mp4",
		Streams: []MediaStream{{Index: 0, Kind: "video", Codec: "h264"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Streams(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ColorTransfer != "" {
		t.Errorf("ColorTransfer = %q, want empty", got[0].ColorTransfer)
	}
}
