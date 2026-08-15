package probe

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"lancast/internal/store"
)

// Store is the persistence the worker needs.
type Store interface {
	PendingProbe(ctx context.Context, limit int) ([]store.Item, error)
	PendingProbeCount(ctx context.Context) (int, error)
	SaveProbe(ctx context.Context, itemID int64, r store.ProbeResult) error
	MarkProbeFailed(ctx context.Context, itemID int64) error
}

// Stats is a snapshot of probing progress.
type Stats struct {
	Running   bool  `json:"running"`
	Probed    int   `json:"probed"`
	Failed    int   `json:"failed"`
	Remaining int   `json:"remaining"`
	Total     int   `json:"total"`
	UpdatedAt int64 `json:"updated_at"`
}

// Worker probes pending items in the background.
//
// Separate from metadata enrichment on purpose: probing is local, fast, and
// needs no API key, while enrichment is networked, rate-limited, and stops
// early without a provider. Folding them together would mean a library with no
// TMDB key never gets probed either, and probing is what playback depends on.
type Worker struct {
	st     Store
	prober *Prober
	log    *slog.Logger

	// Concurrency defaults to half the CPUs. ffprobe is IO-bound at first and
	// CPU-bound on damaged files; saturating every core makes the machine
	// unpleasant to use while a first scan runs.
	Concurrency int
	BatchSize   int

	mu      sync.Mutex
	running bool
	stats   Stats
}

func NewWorker(st Store, p *Prober, log *slog.Logger) *Worker {
	conc := runtime.NumCPU() / 2
	if conc < 1 {
		conc = 1
	}
	return &Worker{
		st: st, prober: p, log: log,
		Concurrency: conc,
		BatchSize:   100,
	}
}

// Stats returns current progress.
func (w *Worker) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// Available reports whether probing can run at all.
func (w *Worker) Available() bool { return w.prober.Available() }

// Run probes pending items until the queue drains or ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	if !w.prober.Available() {
		// Not an error: LANcast works without ffmpeg, it just cannot make
		// informed playback decisions. Saying so once beats failing per item.
		w.log.Info("ffprobe not found; skipping media probing")
		return nil
	}

	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats = Stats{Running: true, UpdatedAt: time.Now().Unix()}
	w.mu.Unlock()

	if total, err := w.st.PendingProbeCount(ctx); err == nil {
		w.mu.Lock()
		w.stats.Total, w.stats.Remaining = total, total
		w.mu.Unlock()
	}

	defer func() {
		remaining, err := w.st.PendingProbeCount(context.WithoutCancel(ctx))
		w.mu.Lock()
		w.running = false
		w.stats.Running = false
		if err == nil {
			w.stats.Remaining = remaining
		}
		w.stats.UpdatedAt = time.Now().Unix()
		w.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		items, err := w.st.PendingProbe(ctx, w.BatchSize)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}

		progressed := w.processBatch(ctx, items)
		if err := ctx.Err(); err != nil {
			return err
		}

		// Same guard as enrichment: the queue is a query, not a cursor, so a
		// batch that stamps nothing would return identical rows forever.
		// Failures do stamp (see MarkProbeFailed), so this only trips if
		// something is deeply wrong.
		if progressed == 0 {
			w.log.Warn("probing made no progress; stopping", "pending", len(items))
			return nil
		}

		if remaining, err := w.st.PendingProbeCount(ctx); err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.mu.Unlock()
		}
	}
}

func (w *Worker) processBatch(ctx context.Context, items []store.Item) int {
	sem := make(chan struct{}, w.Concurrency)
	var wg sync.WaitGroup
	var progressed atomic.Int64

	for i := range items {
		item := items[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			res, err := w.prober.Probe(ctx, item.Path)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				// A file ffprobe cannot read is stamped anyway. Leaving it
				// pending means one corrupt file re-probes on every pass
				// forever and the queue never drains.
				w.log.Warn("probe failed", "item", item.ID, "title", item.Title, "error", err)
				if err := w.st.MarkProbeFailed(ctx, item.ID); err == nil {
					progressed.Add(1)
				}
				w.mu.Lock()
				w.stats.Failed++
				w.mu.Unlock()
				return
			}

			if err := w.st.SaveProbe(ctx, item.ID, toStoreResult(res)); err != nil {
				w.log.Warn("saving probe failed", "item", item.ID, "error", err)
				return
			}
			progressed.Add(1)
			w.mu.Lock()
			w.stats.Probed++
			w.stats.UpdatedAt = time.Now().Unix()
			w.mu.Unlock()
		}()
	}
	wg.Wait()
	return int(progressed.Load())
}

// toStoreResult flattens a probe into the storage shape, denormalizing the
// primary video and audio streams onto the item.
func toStoreResult(r *Result) store.ProbeResult {
	out := store.ProbeResult{
		DurationMS: r.DurationMS,
		Container:  r.Container,
	}
	if v := r.Video(); v != nil {
		out.VideoCodec, out.VideoProfile = v.Codec, v.Profile
		out.Width, out.Height, out.VideoBitRate = v.Width, v.Height, v.BitRate
		fmt.Sscanf(v.FrameRate, "%g", &out.FrameRate)
	}
	if a := r.Audio(); a != nil {
		out.AudioCodec, out.AudioChannels = a.Codec, a.Channels
	}
	for _, s := range r.Streams {
		out.Streams = append(out.Streams, store.MediaStream{
			Index: s.Index, Kind: string(s.Kind), Codec: s.Codec, Profile: s.Profile,
			PixFmt:        s.PixFmt,
			ColorTransfer: s.ColorTransfer, ColorPrimaries: s.ColorPrimaries,
			ColorSpace: s.ColorSpace,
			Language:   s.Language, Title: s.Title, Default: s.Default, Forced: s.Forced,
			Width: s.Width, Height: s.Height, Channels: s.Channels, BitRate: s.BitRate,
		})
	}
	return out
}

// ResultWithStreams rebuilds a Result from the item's summary columns and its
// real track list.
//
// Prefer this over ResultFromItem wherever a decision is actually made. The
// summary columns describe one video and one audio stream — the default track
// — so a decision built from them alone is blind to alternate audio. On a file
// whose default track is TrueHD and whose second track is AAC, that is the
// difference between an unnecessary re-encode and direct play, and it makes
// selecting a track impossible: the engine cannot decide about a stream it
// cannot see. An item with no stored streams falls back to the summary.
func ResultWithStreams(it *store.Item, streams []store.MediaStream) *Result {
	r := ResultFromItem(it)
	if r == nil || len(streams) == 0 {
		return r
	}
	r.Streams = make([]Stream, 0, len(streams))
	for _, s := range streams {
		r.Streams = append(r.Streams, Stream{
			Index: s.Index, Kind: StreamKind(s.Kind), Codec: s.Codec,
			Profile: s.Profile, PixFmt: s.PixFmt,
			ColorTransfer: s.ColorTransfer, ColorPrimaries: s.ColorPrimaries,
			ColorSpace: s.ColorSpace,
			Language:   s.Language, Title: s.Title, Default: s.Default, Forced: s.Forced,
			Width: s.Width, Height: s.Height, Channels: s.Channels, BitRate: s.BitRate,
		})
	}
	return r
}

// ResultFromItem rebuilds enough of a Result from stored columns to make a
// playback decision, without loading the full stream list.
func ResultFromItem(it *store.Item) *Result {
	if it == nil || it.ProbedAt == nil {
		return nil
	}
	r := &Result{DurationMS: derefInt64(it.DurationMS)}
	if it.Container != nil {
		r.Container = containerFromExtension(*it.Container)
	}
	if it.VideoCodec != nil {
		r.Streams = append(r.Streams, Stream{
			Kind: KindVideo, Codec: *it.VideoCodec,
			Profile: derefString(it.VideoProfile),
			Width:   derefInt(it.Width), Height: derefInt(it.Height),
			BitRate: derefInt64(it.VideoBitRate), Default: true,
		})
	}
	if it.AudioCodec != nil {
		r.Streams = append(r.Streams, Stream{
			Kind: KindAudio, Codec: *it.AudioCodec,
			Channels: derefInt(it.AudioChannels), Default: true,
		})
	}
	return r
}

// containerFromExtension maps a file extension to the format name ffprobe
// reports, since the decision engine compares against ffprobe's vocabulary.
func containerFromExtension(ext string) string {
	switch ext {
	case "mkv":
		return "matroska"
	case "mp4", "m4v", "mov":
		return "mov"
	case "webm":
		return "webm"
	case "avi":
		return "avi"
	case "ts", "m2ts":
		return "mpegts"

	// Audio. The extension and the format name agree for mp3, flac, ogg and
	// wav, which is why those need no case — but the ones that disagree are
	// exactly the common music formats, and getting them wrong is invisible:
	// the container simply never matches a profile, and every .m4a and .opus in
	// the library transcodes forever for no stated reason.
	case "m4a", "m4b":
		return "mov"
	case "opus", "oga":
		return "ogg"
	case "mka":
		return "matroska"
	case "aif", "aiff":
		// Normalised for consistency rather than for a match: no profile lists
		// aiff, because only Safari decodes it. It transcodes, and says so.
		return "aiff"
	case "wma":
		return "asf"

	default:
		return ext
	}
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
