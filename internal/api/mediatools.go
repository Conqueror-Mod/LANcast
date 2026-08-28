package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/mediatools"
)

/*
 * Installing the media tools from inside the app (ADR 0043).
 *
 * Admin-only, and for a stronger reason than most admin gates here: this makes
 * the server download a binary and then execute it. The URL is not a parameter —
 * it is pinned in `mediatools` with a checksum — because a server that fetches
 * an address the caller chose is the server-side request forgery the channel and
 * guide endpoints already refuse, and here the payload is an executable rather
 * than a playlist.
 *
 * It is also never automatic. Nothing here runs on first start or when a probe
 * fails: a media server that contacts the internet without being asked has
 * broken no phone-home, and that principle has no convenience exception.
 */

// toolsJob is the state of the one install that may be running.
//
// One at a time, in memory. Two concurrent downloads of the same 160MB archive
// into the same directory is not a case worth supporting, and an install that
// survived a restart would be an install whose progress nobody could see.
type toolsJob struct {
	mu       sync.Mutex
	running  bool
	stage    mediatools.Stage
	done     int64
	total    int64
	err      string
	finished time.Time
	cancel   context.CancelFunc
}

func (j *toolsJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"running":     j.running,
		"stage":       string(j.stage),
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
 * installMediaTools starts the download.
 *
 * Returns 202 immediately: this is 160MB, so holding the request open for it
 * would mean a client timeout looked like a failed install. Progress is polled
 * from the same path.
 */
/*
 * beginToolsInstall starts a download and reports whether it started.
 *
 * Split out of installMediaTools so first-run setup can begin the same install
 * without going through an admin-gated HTTP handler — at account creation there
 * are no accounts yet, so that gate cannot be the thing protecting it.
 *
 * What protects it instead is that nothing calls this without a person having
 * pressed a button that said what it does (ADR 0048). Any future caller
 * inherits that obligation: this function does not ask, it only starts.
 *
 * Returns false when there is nothing to start — an unsupported platform, or an
 * install already running — which the two callers report differently and
 * neither treats as fatal.
 */
func (s *Server) beginToolsInstall() (mediatools.Source, bool) {
	src, err := mediatools.SourceForHost()
	if err != nil {
		return mediatools.Source{}, false
	}

	s.tools.mu.Lock()
	if s.tools.running {
		s.tools.mu.Unlock()
		return src, false
	}
	// Not tied to any request context: the install must survive the browser tab
	// that started it, which is the difference between a background job and a
	// long request.
	ctx, cancel := context.WithCancel(context.Background())
	s.tools.running = true
	s.tools.stage = mediatools.StageDownloading
	s.tools.done, s.tools.total = 0, src.SizeBytes
	s.tools.err = ""
	s.tools.finished = time.Time{}
	s.tools.cancel = cancel
	s.tools.mu.Unlock()

	go s.runToolsInstall(ctx, src, s.mediaToolsDir())
	return src, true
}

func (s *Server) installMediaTools(w http.ResponseWriter, r *http.Request) {
	src, err := mediatools.SourceForHost()
	if errors.Is(err, mediatools.ErrUnsupportedPlatform) {
		// Not an error the user did anything about, and the honest answer names
		// the better route rather than implying LANcast is broken.
		writeError(w, http.StatusNotImplemented, "unsupported",
			"there is no pinned ffmpeg build for this platform; install ffmpeg with your package manager and LANcast will find it")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "media tools source")
		return
	}

	s.tools.mu.Lock()
	if s.tools.running {
		s.tools.mu.Unlock()
		writeError(w, http.StatusConflict, "conflict", "an install is already running")
		return
	}
	// Not tied to the request context: the install must survive the browser tab
	// that started it, which is the difference between a background job and a
	// long request.
	ctx, cancel := context.WithCancel(context.Background())
	s.tools.running = true
	s.tools.stage = mediatools.StageDownloading
	s.tools.done, s.tools.total = 0, src.SizeBytes
	s.tools.err = ""
	s.tools.finished = time.Time{}
	s.tools.cancel = cancel
	s.tools.mu.Unlock()

	dir := s.mediaToolsDir()
	go s.runToolsInstall(ctx, src, dir)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"source": map[string]any{
			"version":     src.Version,
			"licence":     src.Licence,
			"licence_url": src.LicenceURL,
			"size_bytes":  src.SizeBytes,
			"url":         src.URL,
		},
	})
}

// runToolsInstall performs the install and records the outcome.
func (s *Server) runToolsInstall(ctx context.Context, src mediatools.Source, dir string) {
	/*
	 * Logged on the way in, not only on the way out.
	 *
	 * The first report of this feature was "it hangs at 0%", and the log had
	 * nothing to say about it — there was no line until the install finished or
	 * failed, so a stall was indistinguishable from never having started. An
	 * operation that takes minutes says when it begins.
	 */
	started := time.Now()
	s.log.Info("media tools install started",
		"dir", dir, "version", src.Version, "size_bytes", src.SizeBytes, "url", src.URL)

	err := mediatools.Install(ctx, src, dir, func(p mediatools.Progress) {
		s.tools.mu.Lock()
		s.tools.stage, s.tools.done, s.tools.total = p.Stage, p.BytesDone, p.BytesTotal
		s.tools.mu.Unlock()
	})

	if err == nil {
		/*
		 * Make the running server use what was just installed.
		 *
		 * Ensure puts the directory on this process's PATH; Prober resolves
		 * ffprobe on every call and so needs nothing more, while the transcode
		 * manager holds its path and has to be told. Without this the install
		 * succeeds and the server keeps reporting the tools as missing until a
		 * restart, which is indistinguishable from the install having failed.
		 */
		mediatools.Ensure(dir)
		s.trans.Rescan()
		if cur := s.settings.Get(); cur.FFmpegDir != dir {
			cur.FFmpegDir = dir
			if serr := s.settings.Set(cur); serr != nil {
				s.log.Warn("could not record the media tools location", "error", serr)
			}
		}
		s.log.Info("media tools installed",
			"dir", dir, "version", src.Version, "took", time.Since(started).Round(time.Second))
	} else {
		// Warn with the elapsed time: a failure after four seconds is a refusal,
		// and one after a minute is the stall watchdog, and the two want
		// different next steps.
		s.log.Warn("media tools install failed",
			"error", err, "after", time.Since(started).Round(time.Second))
	}

	s.tools.mu.Lock()
	s.tools.running = false
	s.tools.finished = time.Now()
	s.tools.cancel = nil
	if err != nil {
		s.tools.err = err.Error()
	}
	s.tools.mu.Unlock()
}

// mediaToolsStatus reports the install's progress. Polled, so it is cheap and
// says nothing a caller has to interpret.
func (s *Server) mediaToolsStatus(w http.ResponseWriter, r *http.Request) {
	/*
	 * Never cached, and this is the bug it fixes rather than a precaution.
	 *
	 * The first report was a progress bar frozen at 0% while the install ran to
	 * completion underneath it. Every poll is a GET of the same URL with no
	 * cache-buster, and with no cache headers a browser may heuristically reuse
	 * the first response — so the client asked once, was answered "0 bytes", and
	 * was handed that same answer for the rest of the download.
	 *
	 * The same mistake as a stale continue-watching read, in a different place:
	 * a value that changes every second must say so.
	 */
	w.Header().Set("Cache-Control", "no-store")

	out := s.tools.snapshot()
	out["probe_available"] = s.probes.Available()
	out["transcode_available"] = s.trans.Available()
	out["directory"] = s.settings.Get().FFmpegDir

	if src, err := mediatools.SourceForHost(); err == nil {
		// What a caller is consenting to, before they consent to it.
		out["available_source"] = map[string]any{
			"version":     src.Version,
			"licence":     src.Licence,
			"licence_url": src.LicenceURL,
			"size_bytes":  src.SizeBytes,
			"url":         src.URL,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// cancelMediaToolsInstall stops a running download. A 160MB fetch that cannot be
// stopped is a button nobody should press.
func (s *Server) cancelMediaToolsInstall(w http.ResponseWriter, r *http.Request) {
	s.tools.mu.Lock()
	cancel := s.tools.cancel
	running := s.tools.running
	s.tools.mu.Unlock()

	if !running || cancel == nil {
		writeError(w, http.StatusConflict, "conflict", "no install is running")
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

/*
 * mediaToolsDir is where a fetched toolchain lives.
 *
 * The data directory, not the install directory: Program Files is not writable
 * by a service account or by a non-elevated desktop process, and an installer
 * that needs elevation to fix a playback problem is the extra step this feature
 * exists to remove. `mediatools` searches a `tools` directory beside the server
 * too, so a bundled or hand-dropped copy resolves the same way.
 */
func (s *Server) mediaToolsDir() string {
	return filepath.Join(s.dataDir, "tools")
}
