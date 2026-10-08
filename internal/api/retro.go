package api

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/media"
	"lancast/internal/retro/retrodb"
)

/*
 * The ROM database (ADR 0073): libretro's DAT files, fetched on request.
 *
 * The same shape as the face-model install — a job in memory, a 202, and a
 * polled snapshot — for the same reason: a client timeout must not be
 * indistinguishable from a failed install. Nothing is fetched until somebody
 * presses the button, and the URLs and digests are pinned in retrodb, never
 * supplied by the request.
 */
type retroJob struct {
	mu       sync.Mutex
	running  bool
	stage    retrodb.Stage
	file     string
	done     int64
	total    int64
	err      string
	finished time.Time
	cancel   context.CancelFunc
}

// reset is field by field for the reason faceJob.reset is: assigning a fresh
// struct over j would replace the mutex the caller is holding. The caller
// must hold j.mu.
func (j *retroJob) reset(total int64, cancel context.CancelFunc) {
	j.running = true
	j.stage = retrodb.StageDownloading
	j.file = ""
	j.done = 0
	j.total = total
	j.err = ""
	j.finished = time.Time{}
	j.cancel = cancel
}

func (j *retroJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"running":     j.running,
		"stage":       string(j.stage),
		"file":        j.file,
		"bytes_done":  j.done,
		"bytes_total": j.total,
	}
	if j.err != "" {
		out["error"] = j.err
	}
	if !j.finished.IsZero() {
		out["finished_at"] = j.finished.Unix()
	}
	return out
}

/*
 * retroDatabase reports what would be downloaded, its size and licence, and
 * whether it is already installed — before anything is fetched. It also
 * carries the identify worker's progress, which is the other half of the
 * same question: "are my games named yet, and if not, why not".
 */
func (s *Server) retroDatabase(w http.ResponseWriter, r *http.Request) {
	files := retrodb.Files()
	list := make([]map[string]any, 0, len(files))
	for _, f := range files {
		list = append(list, map[string]any{
			"name":       f.Name,
			"size_bytes": f.SizeBytes,
			// Display only: the server fetches the pinned address whatever a
			// client sends back.
			"url": f.URL(),
		})
	}
	out := map[string]any{
		"installed":   retrodb.Installed(s.retroDir()),
		"commit":      retrodb.Commit,
		"licence":     retrodb.Licence,
		"licence_url": retrodb.LicenceURL,
		"files":       list,
		"bytes_total": retrodb.TotalBytes(),
		"platforms":   media.Platforms,
		"job":         s.retroInstall.snapshot(),
	}
	if s.retro != nil {
		out["identify"] = s.retro.Stats()
	}
	writeJSON(w, http.StatusOK, out)
}

// installRetroDatabase starts the download and returns 202. Progress is
// polled from GET /api/retro/database.
func (s *Server) installRetroDatabase(w http.ResponseWriter, r *http.Request) {
	j := s.retroInstall
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSON(w, http.StatusAccepted, j.snapshot())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.reset(retrodb.TotalBytes(), cancel)
	j.mu.Unlock()

	dir := s.retroDir()
	go func() {
		defer cancel()
		err := retrodb.Install(ctx, dir, func(p retrodb.Progress) {
			j.mu.Lock()
			j.stage, j.file, j.done, j.total = p.Stage, p.File, p.BytesDone, p.BytesTotal
			j.mu.Unlock()
		})
		j.mu.Lock()
		j.running = false
		j.finished = time.Now()
		if err != nil {
			j.err = err.Error()
		}
		j.mu.Unlock()
		if err != nil {
			s.log.Error("rom database install", "error", err)
			return
		}
		s.log.Info("rom database installed", "dir", dir, "commit", retrodb.Commit)
		if s.retroDB != nil {
			s.retroDB.Forget()
		}
		s.requeueROMs(context.Background())
	}()

	s.audit(r, "retro.database.install", "server", "", "started downloading the ROM database", nil)
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

/*
 * requeueROMs makes an install, or turning box art on, take effect now.
 *
 * Every ROM checked before there was anything to check it against was
 * stamped, so without a re-queue the change would do nothing until each file
 * happened to change on disk. A locked match is left alone. The hashes are
 * kept, so this is a lookup per ROM rather than a read of every file.
 */
func (s *Server) requeueROMs(ctx context.Context) {
	n, err := s.st.RequeueROMs(ctx)
	if err != nil {
		s.log.Warn("re-queueing roms", "error", err)
		return
	}
	s.log.Info("roms queued for identification", "count", n)
	if s.retroSoon != nil {
		s.retroSoon()
	}
}

// cancelRetroDatabaseInstall stops a running download.
func (s *Server) cancelRetroDatabaseInstall(w http.ResponseWriter, r *http.Request) {
	j := s.retroInstall
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, j.snapshot())
}

// retroDir is where the DATs live: under the data directory, beside the
// other things LANcast fetches on request.
func (s *Server) retroDir() string {
	if s.retroDB != nil && s.retroDB.Dir != "" {
		return s.retroDB.Dir
	}
	return retroDirIn(s.dataDir)
}

func retroDirIn(dataDir string) string { return filepath.Join(dataDir, "retrodb") }

// knownPlatforms keeps the platform values this build recognises.
func knownPlatforms(vals []string) []string {
	var out []string
	for _, v := range nonEmpty(vals) {
		if media.IsPlatform(v) {
			out = append(out, v)
		}
	}
	return out
}
