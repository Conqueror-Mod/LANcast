package api

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"lancast/internal/store"
)

/*
 * A start past the end of the file plays the file.
 *
 * ffmpeg given an offset beyond the last frame gets no frames, and on the GPU
 * decode path it fails opening the encoder with `hw_frames_ctx must be set when
 * using GPU frames as input` — an error that points at the graphics card. Seen
 * when autoplay carried 1301s into a 1291s episode. The client fault is fixed on
 * its own; this holds the server to the same rule the client's resumeSeconds
 * already uses for a saved position.
 */

func startAtServer() *Server {
	return &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func startAtFor(t *testing.T, query string, durationMS *int64) float64 {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/stream/37109/transcode"+query, nil)
	return startAtServer().startAt(r, &store.Item{ID: 37109, DurationMS: durationMS})
}

func ms(v int64) *int64 { return &v }

func TestAStartPastTheEndStartsFromTheBeginning(t *testing.T) {
	if got := startAtFor(t, "?t=1301", ms(1_291_049)); got != 0 {
		t.Errorf("start = %v, want 0 for 1301s into a 1291s file", got)
	}
}

// Exactly at the end has no frames either.
func TestAStartAtTheEndStartsFromTheBeginning(t *testing.T) {
	if got := startAtFor(t, "?t=1291.049", ms(1_291_049)); got != 0 {
		t.Errorf("start = %v, want 0", got)
	}
}

// A real resume point is left exactly alone: this is a guard, not a rule about
// where people like to start.
func TestAStartInsideTheFileIsKept(t *testing.T) {
	if got := startAtFor(t, "?t=1200", ms(1_291_049)); got != 1200 {
		t.Errorf("start = %v, want 1200", got)
	}
}

// With no probed duration there is nothing to check against, and guessing
// would be worse than trusting the caller.
func TestAStartWithNoKnownDurationIsKept(t *testing.T) {
	if got := startAtFor(t, "?t=1301", nil); got != 1301 {
		t.Errorf("start = %v, want 1301", got)
	}
}

func TestNoStartIsTheBeginning(t *testing.T) {
	if got := startAtFor(t, "", ms(1_291_049)); got != 0 {
		t.Errorf("start = %v, want 0", got)
	}
}
