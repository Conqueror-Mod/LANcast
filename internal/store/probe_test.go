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
