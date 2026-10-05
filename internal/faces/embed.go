package faces

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/store"
)

/*
 * The pass that gives every photograph a vector (ADR 0060).
 *
 * A separate worker from the face pass rather than a mode on it, and the
 * separation goes all the way down: its own models, its own progress, its own
 * store methods, its own line in `capabilities`. They share a binary and
 * nothing else.
 *
 * Folding them together would tie two independent optional downloads to one
 * another — a household that wants search and not face grouping would be told
 * the feature is unavailable because a *different* model is missing, which is
 * the report ADR 0052 built the reason field to avoid.
 *
 * What it does share is the batching, and that is deliberate: the same
 * feed-and-read-concurrently shape, for the same reason written on the face
 * worker. A worker whose output buffer is full stops reading its input, so
 * writing a whole batch before reading any of it deadlocks on the first library
 * big enough to matter.
 */

// EmbedStore is the storage the indexer needs. Narrow on purpose: it is the
// whole list of what a semantic-search pass may do to the database.
type EmbedStore interface {
	PhotosPendingEmbedding(ctx context.Context, libraryID int64, model string, limit int) ([]store.Item, error)
	SavePhotoEmbedding(ctx context.Context, itemID int64, model string, v []float32) error
	PhotosPendingEmbeddingCount(ctx context.Context, libraryID int64, model string) (int, error)
}

// EmbedStats is progress, in the shape the activity view already understands.
type EmbedStats struct {
	Running   bool  `json:"running"`
	Embedded  int   `json:"embedded"`
	Failed    int   `json:"failed"`
	Remaining int   `json:"remaining"`
	UpdatedAt int64 `json:"updated_at"`
}

// Indexer drives the worker's `embed` command over a library's photographs.
type Indexer struct {
	st   EmbedStore
	tool *Tool
	log  *slog.Logger

	/*
	 * Thumbnail finds the display copy the photo worker cached for a
	 * photograph, for a second attempt when the original cannot be read.
	 *
	 * The embedding worker reads what its decoder reads, and HEIC and BMP are
	 * not that: on a real library 19 HEIC and 7 BMP photographs failed every
	 * time. The photo worker already decoded every one of them — through ffmpeg
	 * for HEIC — into a JPEG for the grid, and a model that looks at 224
	 * pixels loses nothing by looking at a 1600-pixel copy. Nil means no
	 * second attempt.
	 */
	Thumbnail func(ctx context.Context, it store.Item) (string, bool)

	// embedFn stands in for the worker process in tests. Nil is the process.
	embedFn func(ctx context.Context, paths []string) ([]embedLine, error)

	mu    sync.Mutex
	stats EmbedStats
}

func NewIndexer(st EmbedStore, tool *Tool, log *slog.Logger) *Indexer {
	return &Indexer{st: st, tool: tool, log: log}
}

func (ix *Indexer) Stats() EmbedStats {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.stats
}

func (ix *Indexer) set(f func(*EmbedStats)) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	f(&ix.stats)
	ix.stats.UpdatedAt = time.Now().Unix()
}

// embedLine is one line of the worker's `embed` output.
type embedLine struct {
	Path   string    `json:"path"`
	Vector []float32 `json:"vector"`
	Error  string    `json:"error"`
}

/*
 * Run embeds every photograph the current model has not seen.
 *
 * Batched rather than one process per photograph: a session is expensive to
 * build and cheap to reuse, and starting one per file would spend more time
 * loading a model than looking at pictures — the same arithmetic the sidecar's
 * own comment makes.
 */
func (ix *Indexer) Run(ctx context.Context, libraryID int64) error {
	caps := ix.tool.Capabilities(ctx)
	if !caps.SemanticReady {
		// The reason travels rather than a bare failure: "not installed" and
		// "no model" want different things from the person reading it.
		return fmt.Errorf("semantic search is not available: %s", caps.SemanticReason)
	}
	model := caps.SemanticModel
	if model == "" {
		/*
		 * Refused rather than defaulted.
		 *
		 * The model name is what tells a stored vector which coordinate system
		 * it belongs to. Guessing one here would file this pass's vectors under
		 * a name the next worker may not agree with, and the library would be
		 * ranked against a mixture of two spaces — which sorts, and is wrong,
		 * and reports nothing.
		 */
		return fmt.Errorf("the worker did not name its model; refusing to store vectors under a guess")
	}
	return ix.runModel(ctx, libraryID, model)
}

// runModel is the pass itself, once the worker is known to be ready and has
// named its model. Split from Run so the loop is tested without a worker.
func (ix *Indexer) runModel(ctx context.Context, libraryID int64, model string) error {
	ix.set(func(s *EmbedStats) { *s = EmbedStats{Running: true} })
	defer ix.set(func(s *EmbedStats) { s.Running = false })

	/*
	 * Each photograph is tried once per pass.
	 *
	 * A photograph that cannot be embedded keeps no vector, so it stays
	 * pending, and the pending query hands it straight back. The loop asked
	 * again until nothing was pending, which for a photograph that will never
	 * embed is never: on a real library the last 26 — HEIC and BMP — were sent
	 * round and round, the worker restarted and its model reloaded each time,
	 * and the failed count passed 5,000 for a library of 3,079. A batch holding
	 * nothing new ends the pass; what failed is tried again on the next one,
	 * which is still right for a drive that was asleep.
	 */
	tried := map[int64]bool{}
	var failures []embedFailure
	defer func() { ix.report(failures) }()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := ix.st.PhotosPendingEmbedding(ctx, libraryID, model, embedBatch)
		if err != nil {
			return err
		}
		var fresh []store.Item
		for _, it := range items {
			if !tried[it.ID] {
				tried[it.ID] = true
				fresh = append(fresh, it)
			}
		}
		if len(fresh) == 0 {
			if n, err := ix.st.PhotosPendingEmbeddingCount(ctx, libraryID, model); err == nil {
				ix.set(func(s *EmbedStats) { s.Remaining = n })
			}
			return nil
		}
		failed, err := ix.attempt(ctx, fresh, model)
		if err != nil {
			return err
		}
		failures = append(failures, failed...)
		/*
		 * Remaining is re-read rather than decremented.
		 *
		 * A scan can add photographs while this runs, and marking a folder
		 * removes some from the queue entirely — so a counter that only fell
		 * would drift from the truth in both directions. The activity view has
		 * been bitten once already by a total measured at the start and never
		 * revised.
		 */
		if n, err := ix.st.PhotosPendingEmbeddingCount(ctx, libraryID, model); err == nil {
			ix.set(func(s *EmbedStats) { s.Remaining = n })
		}
	}
}

// embedBatch is how many photographs one worker invocation handles. Large
// enough that the model load amortises, small enough that cancelling between
// batches is responsive.
const embedBatch = 200

// embedFailure is one photograph that could not be embedded, and why.
type embedFailure struct {
	Path   string
	Reason string
}

/*
 * attempt embeds a batch, gives what failed a second try from its cached
 * thumbnail, and counts what still failed — once, here, so a photograph that
 * fails its original and its thumbnail is one failure and not two.
 */
func (ix *Indexer) attempt(ctx context.Context, items []store.Item, model string) ([]embedFailure, error) {
	failed, err := ix.batch(ctx, items, model)
	if err != nil {
		return nil, err
	}
	var out []embedFailure
	var retry []store.Item
	original := map[int64]string{}
	reason := map[int64]string{}
	for _, f := range failed {
		if ix.Thumbnail != nil {
			if thumb, ok := ix.Thumbnail(ctx, f.item); ok {
				c := f.item
				original[c.ID], reason[c.ID] = c.Path, f.reason
				c.Path = thumb
				retry = append(retry, c)
				continue
			}
		}
		out = append(out, embedFailure{Path: f.item.Path, Reason: f.reason})
	}
	if len(retry) > 0 {
		again, err := ix.batch(ctx, retry, model)
		if err != nil {
			return nil, err
		}
		for _, f := range again {
			out = append(out, embedFailure{Path: original[f.item.ID], Reason: reason[f.item.ID]})
		}
	}
	for range out {
		ix.set(func(s *EmbedStats) { s.Failed++ })
	}
	return out, nil
}

// report says once, at the end of a pass, what could not be indexed. Not a
// line per photograph: a library of unreadable files would bury the log.
func (ix *Indexer) report(failures []embedFailure) {
	if len(failures) == 0 {
		return
	}
	var examples []string
	for i, f := range failures {
		if i == 5 {
			break
		}
		examples = append(examples, filepath.Base(f.Path))
	}
	ix.log.Warn("photographs could not be indexed for search",
		"count", len(failures), "examples", examples, "reason", failures[0].Reason)
}

type batchFailure struct {
	item   store.Item
	reason string
}

/*
 * batch sends one batch to the worker, stores what embeds, and returns what
 * did not without counting it — attempt decides whether a failure is final.
 *
 * Paths map to a list rather than one item: two photographs that are the same
 * file share one cached thumbnail, so a retry can send one path for two items.
 */
func (ix *Indexer) batch(ctx context.Context, items []store.Item, model string) ([]batchFailure, error) {
	byPath := make(map[string][]store.Item, len(items))
	var paths []string
	for _, it := range items {
		if _, seen := byPath[it.Path]; !seen {
			paths = append(paths, it.Path)
		}
		byPath[it.Path] = append(byPath[it.Path], it)
	}

	embed := ix.embedFn
	if embed == nil {
		embed = ix.embedProcess
	}
	lines, err := embed(ctx, paths)
	if err != nil {
		return nil, err
	}
	answered := map[string]bool{}
	var failed []batchFailure
	for _, line := range lines {
		its, ok := byPath[line.Path]
		if !ok {
			ix.log.Warn("the embedding worker returned a path that was not sent",
				"path", line.Path)
			continue
		}
		answered[line.Path] = true
		for _, it := range its {
			if ok, reason := ix.store(ctx, it, line, model); !ok {
				failed = append(failed, batchFailure{item: it, reason: reason})
			}
		}
	}
	// A path the worker never answered for is a failure too, not a silence.
	for _, p := range paths {
		if !answered[p] {
			for _, it := range byPath[p] {
				failed = append(failed, batchFailure{item: it, reason: "no answer from the worker"})
			}
		}
	}
	return failed, nil
}

// embedProcess runs the worker's `embed` command over paths and collects its
// answers. Feeding and reading at once — see the file comment: a full output
// buffer stops the worker reading, and writing everything first deadlocks.
func (ix *Indexer) embedProcess(ctx context.Context, paths []string) ([]embedLine, error) {
	path, ok := ix.tool.Path()
	if !ok {
		return nil, fmt.Errorf("the worker is not installed")
	}
	cmd := exec.CommandContext(ctx, path, "embed", "-models", ix.tool.ModelsDir)
	cmd.Env = ix.tool.env()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		for _, p := range paths {
			if _, err := fmt.Fprintln(stdin, p); err != nil {
				break
			}
		}
		stdin.Close()
	}()

	var lines []embedLine
	sc := bufio.NewScanner(stdout)
	// 512 float32s as JSON text is comfortably past the default 64KB.
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	for sc.Scan() {
		var line embedLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			ix.log.Warn("unreadable line from the embedding worker", "error", err)
			continue
		}
		lines = append(lines, line)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("embedding worker: %w", err)
	}
	return lines, sc.Err()
}

// record stores one answer and counts it, success or failure.
func (ix *Indexer) record(ctx context.Context, it store.Item, line embedLine, model string) {
	if ok, _ := ix.store(ctx, it, line, model); !ok {
		ix.set(func(s *EmbedStats) { s.Failed++ })
	}
}

// store writes one answer and reports whether it was stored, counting a
// success and leaving a failure for the caller to count.
func (ix *Indexer) store(ctx context.Context, it store.Item, line embedLine, model string) (bool, string) {
	if line.Error != "" || len(line.Vector) == 0 {
		/*
		 * A photograph that cannot be embedded is counted and left.
		 *
		 * Unlike the face pass there is no "examined" stamp to write: the
		 * pending query is "no vector for this model", so a failure stays
		 * pending and is retried on the next run. That is the right shape for a
		 * transient failure — a file on a drive that was asleep — and it does
		 * mean a permanently unreadable photograph is retried every pass. It is
		 * a handful of files and a decode attempt each; a stamp table to avoid
		 * that would be a second thing to keep in step with the model name.
		 */
		if line.Error != "" {
			ix.log.Debug("could not embed a photograph", "path", it.Path, "error", line.Error)
			return false, line.Error
		}
		return false, "an empty vector"
	}

	if err := ix.st.SavePhotoEmbedding(ctx, it.ID, model, line.Vector); err != nil {
		/*
		 * Refused rather than failed, most likely.
		 *
		 * The store declines an embedding for a photograph a mark covers, and a
		 * folder can be marked while this pass is running — between the pending
		 * query that selected it and the write. That is the rule working rather
		 * than an error to escalate, so it is counted and logged at debug.
		 */
		ix.log.Debug("could not store an embedding", "path", it.Path, "error", err)
		return false, err.Error()
	}
	ix.set(func(s *EmbedStats) { s.Embedded++ })
	return true, ""
}
