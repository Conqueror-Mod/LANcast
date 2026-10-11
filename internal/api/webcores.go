package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/retro/cores"
	"lancast/internal/retro/webcores"
)

/*
 * The browser's emulator cores
 * ([ADR 0077](../../docs/adr/0077-a-browser-can-play-a-game.md)).
 *
 * Fetched by the server, on an administrator's request, from a pinned libretro
 * archive, and served only to this server's own signed-in accounts. LANcast
 * never hosts or ships a core. The same shape as the ROM database install: a
 * job in memory, a 202, and a polled snapshot, so a client timeout is never
 * mistaken for a failed install. Nothing in a request names what is fetched;
 * the archive and every file in it are pinned in internal/retro/webcores.
 */

type webCoreJob struct {
	mu       sync.Mutex
	running  bool
	stage    string
	done     int64
	total    int64
	err      string
	finished time.Time
	cancel   context.CancelFunc
}

func (j *webCoreJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"running": j.running,
		"stage":   j.stage,
		"done":    j.done,
		"total":   j.total,
	}
	if j.err != "" {
		out["error"] = j.err
	}
	if !j.finished.IsZero() {
		out["finished_at"] = j.finished.Unix()
	}
	return out
}

// webCoreDir is where the web cores live: under the data directory, beside
// the other things LANcast fetches on request.
func (s *Server) webCoreDir() string { return filepath.Join(s.dataDir, "webcores") }

/*
 * webCores reports which consoles the browser can play here, what installing
 * would download, and how an install is going.
 *
 * Any signed-in account may ask: a game page needs to know whether to offer
 * Play in the browser or say "plays in the desktop app", and that is not an
 * administrator's question.
 */
func (s *Server) webCores(w http.ResponseWriter, r *http.Request) {
	dir := s.webCoreDir()
	list := make([]map[string]any, 0, len(webcores.All()))
	for _, c := range webcores.All() {
		list = append(list, map[string]any{
			"name":       c.Name,
			"display":    c.Display,
			"licence":    c.Licence,
			"source_url": c.SourceURL,
			"consoles":   c.Consoles,
			"size_bytes": c.SizeBytes(),
			"installed":  c.Present(dir),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cores": list,
		"archive": map[string]any{
			// Display only: the server fetches the pinned address whatever a
			// client sends back.
			"url":        webcores.Archive.URL,
			"size_bytes": webcores.Archive.SizeBytes,
			"version":    webcores.Archive.Version,
		},
		"job": s.webCoreInstall.snapshot(),
	})
}

// installWebCores starts the download and returns 202. Progress is polled
// from GET /api/retro/web-cores.
func (s *Server) installWebCores(w http.ResponseWriter, r *http.Request) {
	j := s.webCoreInstall
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSON(w, http.StatusAccepted, j.snapshot())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Field by field: assigning a fresh struct over j would replace the mutex
	// this is holding.
	j.running, j.stage, j.done, j.total = true, "download", 0, webcores.Archive.SizeBytes
	j.err, j.finished, j.cancel = "", time.Time{}, cancel
	j.mu.Unlock()

	dir := s.webCoreDir()
	go func() {
		defer cancel()
		err := webcores.Install(ctx, dir, func(p cores.Progress) {
			j.mu.Lock()
			j.stage, j.done, j.total = p.Stage, p.Done, p.Total
			j.mu.Unlock()
		})
		j.mu.Lock()
		j.running = false
		j.finished = time.Now()
		if err != nil {
			j.err = err.Error()
			if errors.Is(err, context.Canceled) {
				j.err = "cancelled"
			}
		}
		j.mu.Unlock()
		if err != nil {
			s.log.Warn("web cores install", "error", err)
			return
		}
		s.log.Info("web cores installed", "dir", dir, "version", webcores.Archive.Version)
	}()

	s.audit(r, "retro.webcores.install", "server", "", "started downloading the browser's emulator cores", nil)
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

// cancelWebCoresInstall stops a running download.
func (s *Server) cancelWebCoresInstall(w http.ResponseWriter, r *http.Request) {
	j := s.webCoreInstall
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, j.snapshot())
}

/*
 * webCoreFile serves one installed core file to a signed-in account.
 *
 * The core name and kind only ever select from the pinned list
 * (webcores.Path), so nothing a caller sends can reach a file outside the
 * install directory. The pin is the ETag: the bytes behind a name change only
 * when a release moves the pin, and then the tag changes with them, so a
 * browser revalidates cheaply and never runs a stale core.
 */
func (s *Server) webCoreFile(w http.ResponseWriter, r *http.Request) {
	p, f, ok := webcores.Path(s.webCoreDir(), r.PathValue("core"), r.PathValue("kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such core, or it is not installed")
		return
	}
	fh, err := os.Open(p)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such core, or it is not installed")
		return
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		s.writeInternal(w, err, "web core")
		return
	}
	switch r.PathValue("kind") {
	case "wasm":
		// The type WebAssembly.instantiateStreaming insists on.
		w.Header().Set("Content-Type", "application/wasm")
	default:
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", `"`+f.SHA256+`"`)
	http.ServeContent(w, r, "", st.ModTime(), fh)
}
