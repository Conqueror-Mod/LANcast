// Package preview makes the frames shown over the scrub bar (ADR 0105): a
// picture every ten seconds, tiled ten by ten into JPEG sheets, made once per
// file in the background and kept beside the database.
package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // sheets are JPEG
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"lancast/internal/childproc"
	"lancast/internal/store"
)

// Rev is how previews are made. Raising it sends every item back for new
// ones, because it is part of the key each item records.
const Rev = 1

// The shape of every sheet. A frame every IntervalMS, Width pixels wide,
// Columns × Rows to a sheet: one sheet is 1,000 seconds of film.
const (
	IntervalMS = 10_000
	Columns    = 10
	Rows       = 10
	Width      = 240
)

// Manifest describes one item's previews, beside its sheets.
type Manifest struct {
	// Key is the file the sheets were made from (store.PreviewKey). A
	// manifest whose key no longer matches the item is for a file that has
	// since been replaced, and is not served.
	Key        string `json:"key"`
	IntervalMS int    `json:"interval_ms"`
	Columns    int    `json:"columns"`
	Rows       int    `json:"rows"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Sheets     int    `json:"sheets"`
	MadeAt     int64  `json:"made_at"`
}

// Dir is where one item's previews live.
func Dir(root string, itemID int64) string {
	return filepath.Join(root, strconv.FormatInt(itemID, 10))
}

// SheetPath is sheet n, from 0.
func SheetPath(root string, itemID int64, n int) string {
	return filepath.Join(Dir(root, itemID), fmt.Sprintf("sheet-%03d.jpg", n+1))
}

// Load reads an item's manifest.
func Load(root string, itemID int64) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(Dir(root, itemID), "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Store is what the worker needs.
type Store interface {
	PendingPreviews(ctx context.Context, rev, limit int) ([]store.Item, error)
	SetPreviewKey(ctx context.Context, itemID int64, key string) error
	ExistingItems(ctx context.Context, ids []int64) (map[int64]bool, error)
	// PendingPreviewItem is one item, if it still needs previews at rev.
	PendingPreviewItem(ctx context.Context, id int64, rev int) (*store.Item, error)
}

/*
 * Worker makes previews for items that have none, or have them for a file
 * that has since changed.
 *
 * One file at a time, and ffmpeg capped at a quarter of the cores: nothing
 * waits on a preview, and a library import that makes the machine unusable
 * for a film somebody is watching is worse than one that finishes overnight.
 * The marker worker learned that cap the hard way (15 of 24 cores) and this
 * shares the reason.
 *
 * Only keyframes are decoded (-skip_frame nokey), which is what makes this
 * cheap: about 40 s for a two-hour film, measured on H.264 and 10-bit HEVC.
 * Nothing asks the GPU for anything, so it runs the same as a service as it
 * does from a shell (CLAUDE.md, the v0.8.0 rule), and that was still checked
 * as the service before it shipped.
 */
type Worker struct {
	st   Store
	root string
	log  *slog.Logger

	// FFmpegPath is the binary, or empty to find it on PATH (which
	// mediatools has set up by the time a worker exists).
	FFmpegPath string
	// Threads caps one ffmpeg; zero is a quarter of the cores, at least one.
	Threads int
	// Enabled is asked before every file, so turning the setting off stops
	// the work now rather than at the end of a batch.
	Enabled func() bool

	// makeFn stands in for make in tests, so the queue is tested without
	// ffmpeg. Nil is the real one.
	makeFn func(ctx context.Context, it store.Item, dir string) (*Manifest, error)

	mu      sync.Mutex
	running bool
	// wanted is items somebody has opened in the player, most recent first:
	// made before the rest of the queue (Want). Guarded by mu.
	wanted []int64
}

// maxWanted bounds the wanted list. Somebody flicking through ten films has
// asked for the last few; the rest wait their turn in the ordinary queue.
const maxWanted = 8

/*
 * Want puts an item at the front of the queue: somebody has opened it in the
 * player and it has no previews yet, so it should not wait behind the rest of
 * the library (2,136 items ahead of one film, the first time this ran on a
 * real library). The most recent ask goes first; asking again moves it back to
 * the front. The caller starts a pass if none is running; a running pass picks
 * it up before its next file.
 */
func (w *Worker) Want(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next := []int64{id}
	for _, x := range w.wanted {
		if x != id && len(next) < maxWanted {
			next = append(next, x)
		}
	}
	w.wanted = next
}

// Wanted reports whether an item is waiting at the front of the queue.
func (w *Worker) Wanted(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, x := range w.wanted {
		if x == id {
			return true
		}
	}
	return false
}

func (w *Worker) popWanted() (int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.wanted) == 0 {
		return 0, false
	}
	id := w.wanted[0]
	w.wanted = w.wanted[1:]
	return id, true
}

// NewWorker makes a worker writing under root.
func NewWorker(st Store, root string, log *slog.Logger) *Worker {
	return &Worker{st: st, root: root, log: log}
}

func (w *Worker) bin() string {
	if w.FFmpegPath != "" {
		return w.FFmpegPath
	}
	return "ffmpeg"
}

func (w *Worker) threads() int {
	if w.Threads > 0 {
		return w.Threads
	}
	if n := runtime.NumCPU() / 4; n > 0 {
		return n
	}
	return 1
}

// Run makes previews until none are pending, the setting is off, or ctx
// ends. A second call while one runs returns at once.
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	if err := os.MkdirAll(w.root, 0o755); err != nil {
		return err
	}
	w.prune(ctx)
	made, failed := 0, 0
	// Items made this pass because somebody wanted them, so a batch already
	// read does not make them a second time.
	done := map[int64]bool{}
	// drain makes every wanted item that still needs previews.
	drain := func() error {
		for {
			id, ok := w.popWanted()
			if !ok {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			it, err := w.st.PendingPreviewItem(ctx, id, Rev)
			if err != nil || it == nil {
				continue // gone, already made, or not a film: nothing to do
			}
			if w.one(ctx, *it) {
				made++
			} else if ctx.Err() == nil {
				failed++
			}
			done[id] = true
		}
	}
	defer func() {
		if made+failed > 0 {
			w.log.Info("scrub-bar previews pass", "made", made, "failed", failed)
		}
	}()
	for {
		if w.Enabled != nil && !w.Enabled() {
			return nil
		}
		if err := drain(); err != nil {
			return err
		}
		items, err := w.st.PendingPreviews(ctx, Rev, 25)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, it := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			if w.Enabled != nil && !w.Enabled() {
				return nil
			}
			// Somebody opened a film while this batch was running: theirs first.
			if err := drain(); err != nil {
				return err
			}
			if done[it.ID] {
				continue
			}
			if w.one(ctx, it) {
				made++
			} else if ctx.Err() == nil {
				failed++
			}
		}
	}
}

// one makes one item's previews and records that the file was looked at,
// whether or not that worked: a file ffmpeg cannot read is tried again when
// it changes, not on every pass.
func (w *Worker) one(ctx context.Context, it store.Item) bool {
	key := store.PreviewKey(it, Rev)
	final := Dir(w.root, it.ID)
	tmp := filepath.Join(w.root, ".tmp-"+strconv.FormatInt(it.ID, 10))
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)

	makeFn := w.make
	if w.makeFn != nil {
		makeFn = w.makeFn
	}
	m, err := makeFn(ctx, it, tmp)
	if ctx.Err() != nil {
		return false // stopped, not failed: try again next pass
	}
	ok := err == nil
	if ok {
		m.Key = key
		m.MadeAt = time.Now().Unix()
		raw, _ := json.Marshal(m)
		if err = os.WriteFile(filepath.Join(tmp, "manifest.json"), raw, 0o644); err == nil {
			_ = os.RemoveAll(final)
			err = os.Rename(tmp, final)
		}
		ok = err == nil
	}
	if !ok {
		w.log.Warn("could not make scrub-bar previews", "item", it.ID, "error", err)
	}
	if err := w.st.SetPreviewKey(context.WithoutCancel(ctx), it.ID, key); err != nil {
		w.log.Warn("could not record a previews pass", "item", it.ID, "error", err)
	}
	return ok
}

// make runs ffmpeg into dir and describes what it wrote.
func (w *Worker) make(ctx context.Context, it store.Item, dir string) (*Manifest, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, w.bin(),
		"-nostdin", "-v", "error",
		"-threads", strconv.Itoa(w.threads()),
		// Keyframes only: the whole reason this is cheap.
		"-skip_frame", "nokey",
		"-i", it.Path,
		"-an", "-sn", "-dn",
		"-vf", fmt.Sprintf("fps=1000/%d,scale=%d:-2,tile=%dx%d", IntervalMS, Width, Columns, Rows),
		"-q:v", "5",
		"-f", "image2",
		filepath.Join(dir, "sheet-%03d.jpg"))
	childproc.Hide(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, firstLine(stderr.String()))
	}
	return Describe(dir)
}

/*
 * Describe reads the sheets ffmpeg wrote into a manifest.
 *
 * "ffmpeg exited cleanly" is not trusted on its own: measuring this, one run
 * on a file that worked every time after wrote no sheets and exited 0. A pass
 * with no sheets is a failure.
 */
func Describe(dir string) (*Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sheets := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sheet-") && strings.HasSuffix(e.Name(), ".jpg") {
			sheets++
		}
	}
	if sheets == 0 {
		return nil, errors.New("ffmpeg wrote no sheets")
	}
	f, err := os.Open(filepath.Join(dir, "sheet-001.jpg"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("first sheet: %w", err)
	}
	return &Manifest{
		IntervalMS: IntervalMS, Columns: Columns, Rows: Rows,
		Width: cfg.Width / Columns, Height: cfg.Height / Rows, Sheets: sheets,
	}, nil
}

// prune removes previews whose item no longer exists, and anything a pass
// cut short left behind.
func (w *Worker) prune(ctx context.Context) {
	entries, err := os.ReadDir(w.root)
	if err != nil {
		return
	}
	var ids []int64
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			_ = os.RemoveAll(filepath.Join(w.root, e.Name()))
			continue
		}
		if id, err := strconv.ParseInt(e.Name(), 10, 64); err == nil && e.IsDir() {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	exist, err := w.st.ExistingItems(ctx, ids)
	if err != nil {
		return
	}
	for _, id := range ids {
		if !exist[id] {
			_ = os.RemoveAll(Dir(w.root, id))
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
