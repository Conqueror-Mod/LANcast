package api

import (
	"context"
	"net/http"
	"time"

	"lancast/internal/faceinstall"
)

/*
 * Fetching the semantic-search models on request (ADR 0060).
 *
 * The same job shape as the face-model install, and deliberately the same type:
 * `faceJob` is a download with a stage, a byte count and a cancel, and a second
 * identical struct beside it would be one place for the unlock-of-unlocked-mutex
 * trap its comment documents to be reintroduced by somebody copying the file.
 *
 * What is *not* shared is the job itself. Two features, two downloads, two
 * progress bars: a household installing search while face grouping is already
 * running should see two rows, not one that overwrites the other's byte count.
 *
 * Nothing here is fetched automatically, and the URLs are pinned in
 * `faceinstall` rather than supplied by the request, for the reason written
 * there — the payload is a model this server loads.
 */

/*
 * semanticModels reports what would be downloaded and under which licences,
 * before anything is fetched.
 *
 * `bytes_total` is what is actually *missing*, not what the set weighs. The
 * runtime is 76MB and both features need it, so a household that installed face
 * grouping last week is told this costs 600MB rather than 680 — and the number
 * they are shown is the number the progress bar will count to.
 */
func (s *Server) semanticModels(w http.ResponseWriter, r *http.Request) {
	dir := s.faceModelsDir()
	assets, err := faceinstall.SemanticAssetsForHost()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"supported": false,
			"reason":    "there is no pinned semantic-search download for this platform",
			"job":       s.semanticModelJob.snapshot(),
		})
		return
	}

	missing := faceinstall.Missing(dir, assets)
	have := map[string]bool{}
	for _, a := range assets {
		have[a.Name] = true
	}
	for _, a := range missing {
		have[a.Name] = false
	}

	list := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		list = append(list, map[string]any{
			"name":        a.Name,
			"size_bytes":  a.SizeBytes,
			"licence":     a.Licence,
			"licence_url": a.LicenceURL,
			// Per asset, so the shared runtime can be shown as already present
			// rather than silently dropped from a total that no longer adds up.
			"present": have[a.Name],
			// The URL travels so somebody can see where their machine is about
			// to connect. Display-only: the server fetches the pinned address
			// regardless of anything a client sends back.
			"url": a.URL,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"supported":   true,
		"installed":   faceinstall.SemanticInstalled(dir),
		"assets":      list,
		"bytes_total": faceinstall.TotalBytes(missing),
		"directory":   dir,
		"job":         s.semanticModelJob.snapshot(),
	})
}

// installSemanticModels starts the download and returns 202. Progress is polled
// from GET /api/photos/semantic/models.
func (s *Server) installSemanticModels(w http.ResponseWriter, r *http.Request) {
	assets, err := faceinstall.SemanticAssetsForHost()
	if err != nil {
		writeError(w, http.StatusConflict, "unsupported",
			"there is no pinned semantic-search download for this platform")
		return
	}
	dir := s.faceModelsDir()
	/*
	 * Only what is missing is fetched, and by digest rather than by name.
	 *
	 * That makes pressing the button again after a failed install repair the
	 * one file that was truncated instead of spending 600MB on the two that
	 * were fine — while still repairing them, which skipping on name would not.
	 */
	assets = faceinstall.Missing(dir, assets)
	if len(assets) == 0 {
		// Already complete. Not an error, and not a download: somebody pressing
		// install on an installed feature has asked for a state it is already
		// in.
		writeJSON(w, http.StatusAccepted, s.semanticModelJob.snapshot())
		return
	}

	j := s.semanticModelJob
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		// Not an error: two presses of one button is a person, not a fault.
		writeJSON(w, http.StatusAccepted, j.snapshot())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.reset(faceinstall.TotalBytes(assets), cancel)
	j.mu.Unlock()

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
			s.log.Error("semantic model install", "error", err)
			return
		}
		// The worker's readiness is cached for a minute, and this is the one
		// event that changes it. The probe taken while the models were still
		// downloading said they could not be loaded, and that is precisely the
		// answer somebody watching the progress bar would otherwise be left
		// reading after it finished.
		if s.faceTool != nil {
			s.faceTool.Forget()
		}
		s.log.Info("semantic search models installed", "dir", dir)
	}()

	s.audit(r, "photos.semantic.install", "server", "",
		"started downloading the semantic search models", nil)
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

// cancelSemanticModelsInstall stops a running download. Six hundred megabytes
// on a metered connection is something somebody may change their mind about
// halfway through.
func (s *Server) cancelSemanticModelsInstall(w http.ResponseWriter, r *http.Request) {
	j := s.semanticModelJob
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, j.snapshot())
}
