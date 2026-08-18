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
		s.log.Info("media tools installed", "dir", dir, "version", src.Version)
	} else {
		s.log.Warn("media tools install failed", "error", err)
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
