package faces

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lancast/internal/photo"
	"lancast/internal/store"
)

/*
 * The face pass (ADR 0052).
 *
 * Walks a picture library, hands photographs to `lancast-faces`, records what
 * comes back, and re-groups the library at the end. It follows the shape the
 * other workers already have — scan, enrich, probe — because a fourth worker
 * that behaved differently would be a fourth thing to learn.
 *
 * One subprocess for the whole batch rather than one per photograph. The worker
 * loads a 38MB model on startup, so a process per photograph would spend most
 * of its life loading and the pass would take hours instead of minutes.
 */

// Store is the persistence this worker needs.
type Store interface {
	PendingFaces(ctx context.Context, libraryID int64, limit int) ([]store.Item, error)
	PendingFacesCount(ctx context.Context, libraryID int64) (int, error)
	RecordFaces(ctx context.Context, itemID int64, faces []store.Face) error
	MarkFacesDone(ctx context.Context, itemID int64) error
	ClusterLibrary(ctx context.Context, libraryID int64) error
}

// Stats is a snapshot of progress, in the shape the activity view already
// understands.
type Stats struct {
	Running   bool  `json:"running"`
	Examined  int   `json:"examined"`
	Found     int   `json:"found"`
	Failed    int   `json:"failed"`
	Remaining int   `json:"remaining"`
	UpdatedAt int64 `json:"updated_at"`
}

type Worker struct {
	st   Store
	tool *Tool
	log  *slog.Logger

	// BatchSize is how many photographs one subprocess handles. Large enough
	// that the model load is amortised, small enough that stopping the server
	// does not mean waiting for a library.
	BatchSize int

	/*
	 * Display finds the upright copy the photo worker cached for the grid.
	 *
	 * Used for a photograph the detector would see wrongly or not at all (see
	 * readsUpright). Nil, or no copy yet, and the file is used as before.
	 */
	Display func(ctx context.Context, it store.Item) (string, bool)

	// upright reports whether the detector, given this file, sees the picture
	// the right way up. A field so tests can answer without real photographs.
	upright func(path string) bool

	mu      sync.Mutex
	running bool
	stats   Stats
}

func NewWorker(st Store, tool *Tool, log *slog.Logger) *Worker {
	return &Worker{st: st, tool: tool, log: log, BatchSize: 200, upright: readsUpright}
}

// detectable is what the face worker's decoder reads: the image/* packages it
// registers. Anything else fails to decode there.
var detectable = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true}

/*
 * readsUpright reports whether the detector sees this file as it is meant to be
 * seen: a format it decodes, stored without an EXIF turn.
 *
 * Go's decoders ignore EXIF orientation, and so does the detector. On the live
 * library, photos stored upright had a face found in 61% of cases, photos
 * stored on their side in 10%, and upside down in none. HEIC, BMP and WebP were
 * never decoded at all.
 */
func readsUpright(path string) bool {
	if !detectable[strings.ToLower(filepath.Ext(path))] {
		return false
	}
	o := photo.Orientation(path)
	return o == 0 || o == 1
}

// target is one photograph the detector is about to read, and the picture its
// boxes will be measured in.
type target struct {
	item  store.Item
	frame string
}

/*
 * plan decides what the detector reads for each photograph, keyed by the path
 * sent to it.
 *
 * A path can stand for more than one photograph: exact duplicates share one
 * cached display copy, and each of them gets the faces found in it.
 */
func (w *Worker) plan(ctx context.Context, items []store.Item) (map[string][]target, []string) {
	byPath := make(map[string][]target, len(items))
	var order []string
	add := func(path string, t target) {
		if _, seen := byPath[path]; !seen {
			order = append(order, path)
		}
		byPath[path] = append(byPath[path], t)
	}
	for _, it := range items {
		if w.Display != nil && w.upright != nil && !w.upright(it.Path) {
			if shown, ok := w.Display(ctx, it); ok {
				add(shown, target{item: it, frame: store.FrameDisplay})
				continue
			}
		}
		add(it.Path, target{item: it})
	}
	return byPath, order
}

// Stats returns a copy of the current progress.
func (w *Worker) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

/*
 * Run examines one library, in batches, until nothing is pending.
 *
 * Refuses to start a second pass while one is running. Two subprocesses over
 * the same library would both be handed the same pending photographs — the
 * marker is written after a photograph is examined, not before — and would
 * duplicate every embedding in it.
 */
func (w *Worker) Run(ctx context.Context, libraryID int64) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("a face pass is already running")
	}
	w.running = true
	w.stats = Stats{Running: true, UpdatedAt: time.Now().Unix()}
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.stats.Running = false
		w.stats.UpdatedAt = time.Now().Unix()
		w.mu.Unlock()
	}()

	if c := w.tool.Capabilities(ctx); !c.Ready {
		// Refused rather than run to completion finding nothing, which would
		// mark the whole library examined and never look again.
		return fmt.Errorf("face worker unavailable: %s", c.Reason)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := w.st.PendingFaces(ctx, libraryID, w.BatchSize)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		if err := w.batch(ctx, items); err != nil {
			return err
		}
		remaining, err := w.st.PendingFacesCount(ctx, libraryID)
		if err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.stats.UpdatedAt = time.Now().Unix()
			w.mu.Unlock()
		}
	}

	/*
	 * Grouping runs once, at the end, rather than per batch.
	 *
	 * Clustering is over the whole library by definition — a face is compared
	 * with everything already known — so doing it per batch would do the same
	 * work repeatedly and, worse, would let a half-finished library form groups
	 * that the rest of it then has to be fitted around.
	 */
	return w.st.ClusterLibrary(ctx, libraryID)
}

// result is one line of the worker's output.
type result struct {
	Path  string `json:"path"`
	Faces []struct {
		// One tag per field, deliberately. Written first as
		// `X, Y, W, H int `json:"x,y,w,h"``, which compiles, looks tidy, and
		// gives all four fields the *same* tag — so three of them silently
		// never populate and every face is recorded at zero size.
		X         int       `json:"x"`
		Y         int       `json:"y"`
		W         int       `json:"w"`
		H         int       `json:"h"`
		Score     float64   `json:"score"`
		Embedding []float32 `json:"embedding"`
	} `json:"faces"`
	Error string `json:"error"`
}

/*
 * batch runs one subprocess over a set of photographs.
 *
 * Paths go in on stdin and results come back on stdout, one JSON object per
 * line, keyed by path — not by position. Position would be a contract that
 * breaks silently the first time the worker skips something, and "silently" is
 * the operative word: the faces would be attached to the wrong photographs and
 * everything would still look like it worked.
 */
func (w *Worker) batch(ctx context.Context, items []store.Item) error {
	path, ok := w.tool.Path()
	if !ok {
		return fmt.Errorf("the face worker is not installed")
	}

	byPath, order := w.plan(ctx, items)

	args := []string{"detect", "-models", w.tool.ModelsDir}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = w.tool.env()

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

	// Feeding and reading concurrently, because a worker that has filled its
	// output buffer stops reading its input: writing the whole batch first
	// deadlocks on any library big enough to matter.
	go func() {
		for _, p := range order {
			if _, err := fmt.Fprintln(stdin, p); err != nil {
				break
			}
		}
		stdin.Close()
	}()

	sc := bufio.NewScanner(stdout)
	// An embedding is 128 float32s written as JSON text, so a line carrying
	// several faces is comfortably past the default 64KB.
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	for sc.Scan() {
		var r result
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			w.log.Warn("unreadable line from the face worker", "error", err)
			continue
		}
		targets, ok := byPath[r.Path]
		if !ok {
			w.log.Warn("the face worker returned a path that was not sent",
				"path", r.Path)
			continue
		}
		for _, t := range targets {
			w.record(ctx, t.item, t.frame, r)
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("face worker: %w", err)
	}
	return sc.Err()
}

func (w *Worker) record(ctx context.Context, it store.Item, frame string, r result) {
	if r.Error != "" {
		// A photograph that cannot be read is marked examined anyway. It will
		// not become readable by being tried again immediately, and leaving it
		// unmarked puts it at the front of the queue for ever — the pass would
		// spend its life failing on one truncated JPEG and never reach the
		// rest of the library.
		w.log.Debug("face worker could not read a photograph",
			"item", it.ID, "error", r.Error)
		w.bump(0, 1)
		_ = w.st.MarkFacesDone(ctx, it.ID)
		return
	}

	faces := make([]store.Face, 0, len(r.Faces))
	for _, f := range r.Faces {
		faces = append(faces, store.Face{
			ItemID: it.ID, X: f.X, Y: f.Y, W: f.W, H: f.H,
			Score: f.Score, Embedding: f.Embedding, Frame: frame,
		})
	}
	if err := w.st.RecordFaces(ctx, it.ID, faces); err != nil {
		// Recording refused — the likeliest reason is that the folder was
		// marked sensitive while the pass was running, which is not an error
		// and must not mark the photograph examined: if it is unmarked later,
		// it should be looked at again.
		w.log.Debug("faces not recorded", "item", it.ID, "error", err)
		w.bump(0, 1)
		return
	}
	if err := w.st.MarkFacesDone(ctx, it.ID); err != nil {
		w.log.Warn("could not mark a photograph examined", "item", it.ID, "error", err)
	}
	w.bump(len(faces), 0)
}

func (w *Worker) bump(found, failed int) {
	w.mu.Lock()
	w.stats.Examined++
	w.stats.Found += found
	w.stats.Failed += failed
	w.stats.UpdatedAt = time.Now().Unix()
	w.mu.Unlock()
}
