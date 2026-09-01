package api

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/faceinstall"
)

/*
 * Fetching the face models on request (ADR 0052, following ADR 0048).
 *
 * The same shape as the media-tools install, deliberately: a job in memory, a
 * 202 and a polled snapshot. Holding a request open for 55MB would make a
 * client timeout indistinguishable from a failed install, and this project has
 * already learned that lesson once.
 *
 * Nothing here is fetched automatically. A media server that reaches the
 * internet without being asked has broken no-phone-home, and there is no
 * convenience exception — the URLs and their digests are pinned in
 * `faceinstall`, never supplied by the request, because the payload is a model
 * this server loads and a library it executes.
 */
type faceJob struct {
	mu       sync.Mutex
	running  bool
	stage    faceinstall.Stage
	asset    string
	done     int64
	total    int64
	err      string
	finished time.Time
	cancel   context.CancelFunc
}

func (j *faceJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"running":     j.running,
		"stage":       string(j.stage),
		"asset":       j.asset,
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
 * faceModels reports what would be downloaded, what it costs, and under which
 * licences — before anything is fetched.
 *
 * A download somebody cannot identify is not consent, so this is a GET that
 * anybody may make and it names every file. It also reports whether the models
 * are already present, so the UI can offer an install rather than a
 * re-install.
 */
func (s *Server) faceModels(w http.ResponseWriter, r *http.Request) {
	dir := s.faceModelsDir()
	assets, err := faceinstall.AssetsForHost()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"supported": false,
			"reason":    "there is no pinned face-model download for this platform",
			"job":       s.faceModelJob.snapshot(),
		})
		return
	}

	list := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		list = append(list, map[string]any{
			"name":        a.Name,
			"size_bytes":  a.SizeBytes,
			"licence":     a.Licence,
			"licence_url": a.LicenceURL,
			// The URL travels so somebody can see where their machine is about
			// to connect. It is display-only: the server fetches the pinned
			// address regardless of anything a client sends back.
			"url": a.URL,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"supported":   true,
		"installed":   faceinstall.Installed(dir),
		"assets":      list,
		"bytes_total": faceinstall.TotalBytes(assets),
		"directory":   dir,
		"job":         s.faceModelJob.snapshot(),
	})
}

// installFaceModels starts the download and returns 202. Progress is polled
// from GET /api/faces/models.
func (s *Server) installFaceModels(w http.ResponseWriter, r *http.Request) {
	assets, err := faceinstall.AssetsForHost()
	if err != nil {
		writeError(w, http.StatusConflict, "unsupported",
			"there is no pinned face-model download for this platform")
		return
	}

	j := s.faceModelJob
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		// Not an error: two presses of one button is a person, not a fault.
		writeJSON(w, http.StatusAccepted, j.snapshot())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	*j = faceJob{running: true, stage: faceinstall.StageDownloading,
		total: faceinstall.TotalBytes(assets), cancel: cancel}
	j.mu.Unlock()

	dir := s.faceModelsDir()
	go func() {
		defer cancel()
		err := faceinstall.Install(ctx, assets, dir, func(p faceinstall.Progress) {
			j.mu.Lock()
			j.stage, j.asset, j.done, j.total = p.Stage, p.Asset, p.BytesDone, p.BytesTotal
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
			s.log.Error("face model install", "error", err)
		} else {
			s.log.Info("face models installed", "dir", dir)
		}
	}()

	s.audit(r, "faces.install", "server", "", "started downloading the face models", nil)
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

// cancelFaceModelsInstall stops a running download. A 55MB fetch on a metered
// connection is something somebody may change their mind about halfway through.
func (s *Server) cancelFaceModelsInstall(w http.ResponseWriter, r *http.Request) {
	j := s.faceModelJob
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, j.snapshot())
}

// faceModelsDir is where the models and the runtime live: under the data
// directory, not beside the binary. They are data, they are large, and Program
// Files is not somewhere a server should be writing.
func (s *Server) faceModelsDir() string {
	if s.faceTool != nil && s.faceTool.ModelsDir != "" {
		return s.faceTool.ModelsDir
	}
	return filepath.Join(s.dataDir, "faces")
}
