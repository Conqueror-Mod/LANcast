package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lancast/internal/optimize"
	"lancast/internal/probe"
	"lancast/internal/store"
	"lancast/internal/transcode"
)

/*
 * Optimized copies (ADR 0087): the worker that makes them, the routes that
 * request and list them, and the two places playback uses them.
 *
 * The rules are in internal/optimize; this file runs them. One conversion at
 * a time, never while somebody is watching something converted live, and
 * never past the size limit — a request that would pass it waits, and nothing
 * is deleted to make room.
 */

// optimizedState is what the worker is doing, for GET /api/optimized.
type optimizedState struct {
	mu      sync.Mutex
	current int64         // item converting now, 0 for none
	done    time.Duration // how far into it
	total   time.Duration // its length
	waiting string        // why the queue is not moving, "" when it is
	wake    chan struct{}
}

func (s *Server) optimized() *optimizedState {
	s.optOnce.Do(func() { s.opt = &optimizedState{wake: make(chan struct{}, 1)} })
	return s.opt
}

// WakeOptimizer is wakeOptimizer for the task board's Run now.
func (s *Server) WakeOptimizer() { s.wakeOptimizer() }

// wakeOptimizer starts the worker on a new request now rather than at its
// next tick.
func (s *Server) wakeOptimizer() {
	select {
	case s.optimized().wake <- struct{}{}:
	default:
	}
}

// optimizedDir is where copies live: the setting, or "optimized" in the data
// folder.
func (s *Server) optimizedDir() string {
	if d := s.settings.Get().OptimizedDir; d != "" {
		return d
	}
	return filepath.Join(s.dataDir, "optimized")
}

// optimizedPath is one copy's file, re-verified to sit inside the folder
// (CLAUDE.md: every handler that turns a row into a path checks containment).
func (s *Server) optimizedPath(itemID int64) (string, error) {
	dir := s.optimizedDir()
	return containedPath(dir, filepath.Join(dir, optimize.FileName(itemID)))
}

// checkOptimizedDir answers why a folder will not do, or "" when it will.
func (s *Server) checkOptimizedDir(ctx context.Context, dir string) string {
	if !filepath.IsAbs(dir) {
		return "optimized_dir must be a full path, such as D:\\LANcast\\optimized"
	}
	libs, err := s.st.ListLibraries(ctx)
	if err != nil {
		return "could not read the libraries to check the folder against"
	}
	var roots []string
	for _, l := range libs {
		for _, r := range l.Roots {
			roots = append(roots, r.Path)
		}
		roots = append(roots, l.Path)
	}
	if optimize.Inside(dir, roots) {
		return "optimized_dir may not be inside a library, or a scan would take the copies for new films"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "the server could not create that folder"
	}
	return ""
}

func (s *Server) optimizedLimit() int64 {
	return int64(s.settings.Get().OptimizedLimitGB) << 30
}

/*
 * RunOptimizer converts the queue until ctx ends. Started once by serverapp.
 *
 * Each pass: sweep files no row accounts for (a deleted title, a removed copy),
 * then — unless somebody is watching something converted live, who outranks a
 * copy for later — take the oldest request and convert it, if the limit has
 * room for it.
 */
func (s *Server) RunOptimizer(ctx context.Context) {
	if err := s.st.ResumeOptimized(ctx); err != nil {
		s.log.Warn("optimized: resume", "error", err)
	}
	st := s.optimized()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		s.sweepOptimized(ctx)
		s.optimizeNext(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-st.wake:
		}
	}
}

func (s *Server) setWaiting(why string) {
	st := s.optimized()
	st.mu.Lock()
	st.waiting = why
	st.mu.Unlock()
}

// optimizeNext converts one queued title, if one is due and there is room.
func (s *Server) optimizeNext(ctx context.Context) {
	if s.trans == nil || !s.trans.Available() {
		s.setWaiting("ffmpeg is not installed")
		return
	}
	if n := len(s.trans.Sessions()); n > 0 {
		s.setWaiting("waiting while something is being converted for playback")
		return
	}
	next, err := s.st.NextOptimized(ctx)
	if errors.Is(err, store.ErrNotFound) {
		s.setWaiting("")
		return
	}
	if err != nil {
		s.log.Warn("optimized: next", "error", err)
		return
	}
	var length int64
	if next.DurationMS != nil {
		length = *next.DurationMS
	}
	used, _ := s.st.OptimizedBytes(ctx)
	if used+optimize.EstimateBytes(length) > s.optimizedLimit() {
		// Chris's decision: wait, never delete to make room.
		s.setWaiting("waiting for space: the size limit is reached")
		return
	}
	s.setWaiting("")
	if s.tasks != nil {
		_ = s.tasks.Track("optimized", func() error { return s.convertOne(ctx, next) })
		return
	}
	_ = s.convertOne(ctx, next)
}

// convertOne makes one copy, start to finish.
func (s *Server) convertOne(ctx context.Context, job store.OptimizedVersion) error {
	id := job.ItemID
	fail := func(msg string) error {
		_, _ = s.st.MarkOptimized(ctx, id, store.OptimizedFailed, 0, "", msg)
		s.log.Warn("optimized: failed", "item", id, "reason", msg)
		return errors.New(msg)
	}
	if ok, err := s.st.MarkOptimized(ctx, id, store.OptimizedConverting, 0, "", ""); err != nil || !ok {
		return err
	}

	it, err := s.st.GetItem(ctx, id, "")
	if err != nil {
		return fail("the title is no longer in the library")
	}
	root, err := s.st.RootForItem(ctx, id)
	if err != nil {
		return fail("the title's library location could not be found")
	}
	input, err := containedPath(root.Path, it.Path)
	if err != nil {
		return fail("the title's file is outside its library")
	}
	streams, err := s.st.Streams(ctx, id)
	if err != nil {
		return fail("the title has not been read yet")
	}
	res := probe.ResultWithStreams(it, streams)
	decision := probe.DecideTrack(res, optimize.Profile, -1)
	if decision.Method == probe.DirectPlay {
		return fail("it already plays directly on nearly every device, so a copy would add nothing")
	}

	dir := s.optimizedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail("the optimized folder could not be created")
	}
	final, err := s.optimizedPath(id)
	if err != nil {
		return fail("the optimized folder is not usable")
	}
	part := final + ".part"
	defer os.Remove(part) // gone on every path but the rename

	// Stopped if the request is withdrawn while it runs: its row going is the
	// signal (RemoveOptimized), checked every few seconds.
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
				ids, err := s.st.OptimizedIDs(cctx)
				if err == nil && !ids[id] {
					cancel()
					return
				}
			}
		}
	}()

	var length time.Duration
	if it.DurationMS != nil {
		length = time.Duration(*it.DurationMS) * time.Millisecond
	}
	st := s.optimized()
	st.mu.Lock()
	st.current, st.done, st.total = id, 0, length
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		st.current, st.done = 0, 0
		st.mu.Unlock()
	}()

	started := time.Now()
	s.log.Info("optimized: converting", "item", id, "title", it.Title, "decision", decision.Reason)
	err = s.trans.ConvertFile(cctx, transcode.Options{
		Input: input, Decision: decision, Duration: length.Seconds(), AudioIndex: -1,
		OutputPath: part, MaxRateKbps: optimize.RateKbps,
	}, func(done time.Duration) {
		st.mu.Lock()
		st.done = done
		st.mu.Unlock()
	})
	if errors.Is(err, context.Canceled) {
		if ctx.Err() != nil {
			// The server is stopping: put it back for next time.
			_, _ = s.st.MarkOptimized(context.WithoutCancel(ctx), id, store.OptimizedQueued, 0, "", "")
			return nil
		}
		s.log.Info("optimized: withdrawn while converting", "item", id)
		return nil
	}
	if err != nil {
		msg := err.Error()
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return fail(msg)
	}

	copyRes, err := s.probes.ProbeFile(ctx, part)
	if err != nil {
		return fail("the finished copy could not be read back: " + err.Error())
	}
	raw, _ := json.Marshal(copyRes)
	info, err := os.Stat(part)
	if err != nil {
		return fail("the finished copy disappeared")
	}
	if err := os.Rename(part, final); err != nil {
		return fail("the finished copy could not be put in place")
	}
	if ok, _ := s.st.MarkOptimized(ctx, id, store.OptimizedReady, info.Size(), string(raw), ""); !ok {
		// Withdrawn in the last moment: the row is gone, so the file goes too.
		_ = os.Remove(final)
		return nil
	}
	s.log.Info("optimized: ready", "item", id, "bytes", info.Size(),
		"took", time.Since(started).Round(time.Second))
	return nil
}

// sweepOptimized removes copy files no row accounts for: a title deleted, or
// a copy taken away. The only deletion this feature does by itself, and only
// of copies nobody holds a row for.
func (s *Server) sweepOptimized(ctx context.Context) {
	dir := s.optimizedDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	ids, err := s.st.OptimizedIDs(ctx)
	if err != nil {
		return
	}
	cur := s.optimized()
	cur.mu.Lock()
	converting := cur.current
	cur.mu.Unlock()
	for _, e := range entries {
		var id int64
		name := e.Name()
		if _, err := fmt.Sscanf(name, "%d.mp4", &id); err != nil || id <= 0 {
			continue // not ours; leave anything else in the folder alone
		}
		if ids[id] || (id == converting && strings.HasSuffix(name, ".part")) {
			continue
		}
		if path, err := s.optimizedPath(id); err == nil {
			target := path
			if strings.HasSuffix(name, ".part") {
				target = path + ".part"
			}
			if os.Remove(target) == nil {
				s.log.Info("optimized: removed a copy nothing holds", "file", name)
			}
		}
	}
}

// optimizedFor is a title's finished copy and its probe, or nil.
func (s *Server) optimizedFor(ctx context.Context, itemID int64) *probe.Result {
	v, err := s.st.ReadyOptimized(ctx, itemID)
	if err != nil || v.ProbeJSON == "" {
		return nil
	}
	var res probe.Result
	if json.Unmarshal([]byte(v.ProbeJSON), &res) != nil {
		return nil
	}
	return &res
}

// serveOptimized sends a title's copy, for GET /api/stream/{id}?version=optimized.
// The caller has already passed GetItem for this account.
func (s *Server) serveOptimized(w http.ResponseWriter, r *http.Request, it *store.Item) {
	if _, err := s.st.ReadyOptimized(r.Context(), it.ID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "this title has no optimized copy")
		return
	}
	path, err := s.optimizedPath(it.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "this title has no optimized copy")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the optimized copy is missing from disk")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the optimized copy is missing from disk")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// leafIDs is every playable title under an item: itself for a film or an
// episode, every episode for a show or a season, every film for a collection.
func (s *Server) leafIDs(ctx context.Context, it *store.Item) ([]int64, error) {
	switch it.Kind {
	case "movie", "episode", "video":
		return []int64{it.ID}, nil
	case "collection":
		members, err := s.st.CollectionMembers(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		var out []int64
		for _, m := range members {
			if m.Kind == "movie" || m.Kind == "video" {
				out = append(out, m.ID)
			}
		}
		return out, nil
	case "show", "season":
		var out []int64
		var walk func(parent int64) error
		walk = func(parent int64) error {
			kids, _, err := s.st.ListItems(ctx, store.ItemFilter{ParentID: &parent, Limit: 5000})
			if err != nil {
				return err
			}
			for _, k := range kids {
				if k.Kind == "episode" {
					out = append(out, k.ID)
				} else if k.Kind == "season" {
					if err := walk(k.ID); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return out, walk(it.ID)
	}
	return nil, nil
}

// requestOptimized answers PUT /api/items/{id}/optimized. Admin only.
func (s *Server) requestOptimized(w http.ResponseWriter, r *http.Request) {
	it, ids, ok := s.optimizedTarget(w, r)
	if !ok {
		return
	}
	n, err := s.st.RequestOptimized(r.Context(), ids)
	if err != nil {
		s.writeInternal(w, err, "request optimized")
		return
	}
	s.audit(r, "optimized.request", "item", fmt.Sprint(it.ID),
		fmt.Sprintf("Asked for an optimized copy of %q (%d titles)", it.Title, n), nil)
	s.wakeOptimizer()
	writeJSON(w, http.StatusOK, map[string]any{"queued": n, "titles": len(ids)})
}

// removeOptimized answers DELETE /api/items/{id}/optimized. Admin only. The
// sweep removes the files; a copy being converted stops.
func (s *Server) removeOptimized(w http.ResponseWriter, r *http.Request) {
	it, ids, ok := s.optimizedTarget(w, r)
	if !ok {
		return
	}
	if err := s.st.RemoveOptimized(r.Context(), ids); err != nil {
		s.writeInternal(w, err, "remove optimized")
		return
	}
	s.audit(r, "optimized.remove", "item", fmt.Sprint(it.ID),
		fmt.Sprintf("Removed the optimized copy of %q", it.Title), nil)
	s.wakeOptimizer()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) optimizedTarget(w http.ResponseWriter, r *http.Request) (*store.Item, []int64, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return nil, nil, false
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return nil, nil, false
	}
	ids, err := s.leafIDs(r.Context(), it)
	if err != nil {
		s.writeInternal(w, err, "list titles")
		return nil, nil, false
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "only films, episodes, shows and collections have optimized copies")
		return nil, nil, false
	}
	return it, ids, true
}

// listOptimized answers GET /api/optimized. Admin only.
func (s *Server) listOptimized(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListOptimized(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list optimized")
		return
	}
	used, _ := s.st.OptimizedBytes(r.Context())
	st := s.optimized()
	st.mu.Lock()
	current, done, total, waiting := st.current, st.done, st.total, st.waiting
	st.mu.Unlock()
	var progress float64
	if current != 0 && total > 0 {
		progress = float64(done) / float64(total)
		if progress > 1 {
			progress = 1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":       list,
		"used_bytes":  used,
		"limit_bytes": s.optimizedLimit(),
		"folder":      s.optimizedDir(),
		"current":     current,
		"progress":    progress,
		"waiting":     waiting,
	})
}
