package marker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * The server's own shutdown is not a failure.
 *
 * An installer stopping the service mid-season logged "episode credits scan
 * failed" and "intro detection failed" (v0.9.54), which read as a broken file
 * and a broken show. The season is unstamped and comes back on the next pass;
 * nothing failed.
 */

func TestASeasonStoppedByShutdownLogsNoFailure(t *testing.T) {
	var buf bytes.Buffer
	w, se := seasonWorker(func(string) string { return "" })
	w.log = slog.New(slog.NewTextHandler(&buf, nil))
	ctx, cancel := context.WithCancel(context.Background())
	w.tailFn = func(context.Context, string, float64) (string, error) {
		cancel()
		return "", errors.New("ffmpeg: exit status 1")
	}
	st := &seasonStore{}
	if err := w.examineSeason(ctx, st, se); err == nil {
		t.Error("examineSeason returned nil after being stopped; the season would be stamped")
	}
	if strings.Contains(buf.String(), "failed") {
		t.Errorf("shutdown logged as a failure:\n%s", buf.String())
	}
	if len(st.saved) != 0 {
		t.Errorf("saved %v for a season that was stopped part-way", st.saved)
	}
}

func TestTheIntroPassDoesNotReportAShutdownAsAFailedSeason(t *testing.T) {
	var buf bytes.Buffer
	q := newSeasonQueue(3)
	w := NewWorker(q, slog.New(slog.NewTextHandler(&buf, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	w.examineSeasonFn = func(ctx context.Context, _ IntroStore, _ store.Season) error {
		cancel()
		return ctx.Err()
	}
	_ = w.RunIntros(ctx)
	if strings.Contains(buf.String(), "intro detection failed") {
		t.Errorf("a cancelled season was reported as failed:\n%s", buf.String())
	}
}

// A real failure, with the server running, still says so.
func TestAGenuineScanFailureIsStillLogged(t *testing.T) {
	var buf bytes.Buffer
	w, se := seasonWorker(func(string) string { return "" })
	w.log = slog.New(slog.NewTextHandler(&buf, nil))
	w.tailFn = func(context.Context, string, float64) (string, error) {
		return "", errors.New("ffmpeg: exit status 1")
	}
	_ = w.examineSeason(context.Background(), &seasonStore{}, se)
	if !strings.Contains(buf.String(), "episode credits scan failed") {
		t.Error("a scan that failed on its own was not logged")
	}
}
