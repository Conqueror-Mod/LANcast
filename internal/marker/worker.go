package marker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"lancast/internal/store"
)

// Store is the persistence the worker needs.
type Store interface {
	PendingMarkers(ctx context.Context, limit int) ([]store.Item, error)
	PendingMarkersCount(ctx context.Context) (int, error)
	SaveMarkers(ctx context.Context, itemID int64, kinds []string, markers []store.Marker) error
}

// Stats is a snapshot of detection progress.
type Stats struct {
	Running   bool  `json:"running"`
	Examined  int   `json:"examined"`
	Found     int   `json:"found"`
	Failed    int   `json:"failed"`
	Remaining int   `json:"remaining"`
	UpdatedAt int64 `json:"updated_at"`
}

// Source names this detector on every marker it writes.
const Source = "blackdetect"

/*
 * Worker detects credit boundaries in the background.
 *
 * Its own worker rather than part of probing, because it is a second full
 * decode of a quarter of every file. Probing reads a header; this reads
 * pixels, and folding them together would make every scan pay for it.
 *
 * Concurrency is deliberately low and not derived from the core count. Unlike
 * ffprobe this saturates a core per file for minutes at a time, and a library
 * import that makes the machine unusable is a worse outcome than one that
 * finishes overnight. Nothing waits on a marker: no playback decision reads
 * one, so there is no reason to hurry.
 */
type Worker struct {
	st  Store
	log *slog.Logger

	/*
	 * FFmpegPath is the binary to run, or empty to find it on PATH.
	 *
	 * Empty is the ordinary case and not an oversight: mediatools.Ensure has
	 * already put the configured directory at the front of this process's
	 * PATH by the time a worker exists, which is what lets a service running
	 * as LocalSystem resolve the tools it cannot otherwise see.
	 */
	FFmpegPath string
	// Concurrency is how many files are decoded at once.
	Concurrency int
	BatchSize   int

	/*
	 * Threads caps what one ffmpeg may take, and it is the difference between
	 * a background job and a machine nobody can watch a film on.
	 *
	 * Concurrency alone does not do it. One ffmpeg decoding 10-bit HEVC with a
	 * filter attached will use every core it can find — measured at **15 of 24
	 * busy** on a real machine, while somebody was trying to watch something
	 * else. The worker was correctly limited to one file at a time and that
	 * one file took most of the computer.
	 *
	 * Zero means a quarter of the cores, minimum one. Nothing waits on a
	 * marker, so this pass has no claim on the machine at all.
	 */
	Threads int

	/*
	 * Enabled is checked before every file, not once per pass.
	 *
	 * A pass is a batch, so a setting consulted only at the start keeps
	 * decoding for the rest of it — up to twenty-five films after somebody has
	 * switched it off and is waiting for the noise to stop. Turning something
	 * off should stop it now.
	 */
	Enabled func() bool

	mu      sync.Mutex
	running bool
	stats   Stats
}

func NewWorker(st Store, log *slog.Logger) *Worker {
	return &Worker{st: st, log: log, Concurrency: 1, BatchSize: 25}
}

// Stats returns a snapshot.
func (w *Worker) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// threads is how many ffmpeg may use, defaulting to a quarter of the machine.
func (w *Worker) threads() int {
	if w.Threads > 0 {
		return w.Threads
	}
	if n := runtime.NumCPU() / 4; n > 0 {
		return n
	}
	return 1
}

// stillWanted reports whether the pass should keep going.
func (w *Worker) stillWanted() bool {
	return w.Enabled == nil || w.Enabled()
}

func (w *Worker) bin() string {
	if w.FFmpegPath != "" {
		return w.FFmpegPath
	}
	return "ffmpeg"
}

// Available reports whether ffmpeg can be found.
func (w *Worker) Available() bool {
	if w.FFmpegPath != "" {
		return true
	}
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

/*
 * Run examines one batch and returns.
 *
 * A batch at a time rather than draining the queue, so a caller decides how
 * much of the machine this may have and stopping is never more than one file
 * away. Re-entrant calls are refused rather than queued: two passes over the
 * same pending list would decode everything twice.
 */
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats.Running = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.stats.Running = false
		w.stats.UpdatedAt = time.Now().Unix()
		w.mu.Unlock()
	}()

	items, err := w.st.PendingMarkers(ctx, w.BatchSize)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	conc := w.Concurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		if !w.stillWanted() {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(it store.Item) {
			defer wg.Done()
			defer func() { <-sem }()
			w.examine(ctx, it)
		}(it)
	}
	wg.Wait()

	if n, err := w.st.PendingMarkersCount(ctx); err == nil {
		w.mu.Lock()
		w.stats.Remaining = n
		w.mu.Unlock()
	}
	return nil
}

// examine decodes one file's tail and stores what it finds.
func (w *Worker) examine(ctx context.Context, it store.Item) {
	if it.DurationMS == nil || *it.DurationMS <= 0 || it.Path == "" {
		return
	}
	dur := float64(*it.DurationMS) / 1000

	stderr, err := w.scanTail(ctx, it.Path, ScanFrom(dur))
	if err != nil {
		w.mu.Lock()
		w.stats.Failed++
		w.mu.Unlock()

		/*
		 * Whether to stamp turns on one question: is the file there?
		 *
		 * Originally nothing was stamped on failure, so that an unmounted
		 * drive could not retire a library permanently. That was right about
		 * the drive and wrong about the file. Two damaged films in a real
		 * library — one with an unreadable EBML header, one with no moov atom
		 * — were re-decoded on every pass for ever, logging the same warning
		 * each time. ffmpeg will never read them, and asking again nightly is
		 * not patience, it is a loop.
		 *
		 * A file we cannot even see is the drive; a file we can see and ffmpeg
		 * cannot read is the file. Absence stays transient, which is the same
		 * distinction the scanner makes when it marks missing rather than
		 * deleting — and probing already stamps a failure for exactly this
		 * reason, which is why these two left the probe queue and not this one.
		 *
		 * A stamped failure is not permanent: POST /api/markers/refresh clears
		 * every stamp, so a replaced file is re-examined when somebody says so.
		 */
		if _, statErr := os.Stat(it.Path); statErr != nil {
			w.log.Warn("marker detection failed", "item", it.ID,
				"error", err, "note", "file unreachable; will try again")
			return
		}
		w.log.Warn("marker detection failed", "item", it.ID, "error", err,
			"note", "file is present but unreadable; not asking again")
		if err := w.st.SaveMarkers(ctx, it.ID, []string{store.MarkerCredits}, nil); err != nil {
			w.log.Warn("saving markers failed", "item", it.ID, "error", err)
		}
		return
	}

	c := CreditsFrom(ParseBlackDetect(stderr, ScanFrom(dur)), dur)

	var markers []store.Marker
	if c.Found {
		markers = append(markers, store.Marker{
			Kind:    store.MarkerCredits,
			StartMS: c.StartMS,
			// No end: credits run to the end of the file.
			Source:     Source,
			Confidence: c.Confidence,
		})
	}
	// Stamped either way. An abstention is an answer, and re-deciding it every
	// pass is how a library of un-faded films never stops decoding.
	if err := w.st.SaveMarkers(ctx, it.ID, []string{store.MarkerCredits}, markers); err != nil {
		w.log.Warn("saving markers failed", "item", it.ID, "error", err)
		return
	}

	w.mu.Lock()
	w.stats.Examined++
	if c.Found {
		w.stats.Found++
	}
	w.stats.UpdatedAt = time.Now().Unix()
	w.mu.Unlock()
}

/*
 * scanTail decodes from startSec to the end with blackdetect attached and
 * returns ffmpeg's stderr for the pure half to read.
 *
 * -an because audio is not read. Silence was measured as a credits signal and
 * rejected: it answered for half a sample and landed late when it did, because
 * credits have music over them. Decoding audio to ignore it is work for
 * nothing.
 *
 * No -hwaccel, and that is not an oversight. LANcast runs as a service in
 * session 0 where there is no D3D device, and `-hwaccel auto` chose DXVA2 and
 * broke every HEVC title in v0.8.0. Nothing here needs the GPU: this is a
 * background pass that no one is waiting on.
 */
func (w *Worker) scanTail(ctx context.Context, path string, startSec float64) (string, error) {
	cmd := exec.CommandContext(ctx, w.bin(),
		"-hide_banner", "-nostats",
		"-threads", strconv.Itoa(w.threads()),
		"-ss", strconv.FormatFloat(startSec, 'f', 3, 64),
		"-i", path,
		"-an",
		"-vf", fmt.Sprintf("blackdetect=d=%.2f:pix_th=0.10", FallbackLen/2),
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg: %w", err)
	}
	return string(out), nil
}
