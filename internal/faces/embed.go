package faces

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
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

	ix.set(func(s *EmbedStats) { *s = EmbedStats{Running: true} })
	defer ix.set(func(s *EmbedStats) { s.Running = false })

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := ix.st.PhotosPendingEmbedding(ctx, libraryID, model, embedBatch)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			ix.set(func(s *EmbedStats) { s.Remaining = 0 })
			return nil
		}
		if err := ix.batch(ctx, items, model); err != nil {
			return err
		}
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

func (ix *Indexer) batch(ctx context.Context, items []store.Item, model string) error {
	path, ok := ix.tool.Path()
	if !ok {
		return fmt.Errorf("the worker is not installed")
	}

	byPath := make(map[string]store.Item, len(items))
	for _, it := range items {
		byPath[it.Path] = it
	}

	cmd := exec.CommandContext(ctx, path, "embed", "-models", ix.tool.ModelsDir)
	cmd.Env = ix.tool.env()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Feeding and reading at once — see the file comment. A full output buffer
	// stops the worker reading, and writing everything first deadlocks.
	go func() {
		for _, it := range items {
			if _, err := fmt.Fprintln(stdin, it.Path); err != nil {
				break
			}
		}
		stdin.Close()
	}()

	sc := bufio.NewScanner(stdout)
	// 512 float32s as JSON text is comfortably past the default 64KB.
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	for sc.Scan() {
		var line embedLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			ix.log.Warn("unreadable line from the embedding worker", "error", err)
			continue
		}
		it, ok := byPath[line.Path]
		if !ok {
			ix.log.Warn("the embedding worker returned a path that was not sent",
				"path", line.Path)
			continue
		}
		ix.record(ctx, it, line, model)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("embedding worker: %w", err)
	}
	return sc.Err()
}

func (ix *Indexer) record(ctx context.Context, it store.Item, line embedLine, model string) {
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
		}
		ix.set(func(s *EmbedStats) { s.Failed++ })
		return
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
		ix.set(func(s *EmbedStats) { s.Failed++ })
		return
	}
	ix.set(func(s *EmbedStats) { s.Embedded++ })
}
