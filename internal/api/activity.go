package api

import (
	"net/http"
	"strconv"

	"lancast/internal/coverart"
	"lancast/internal/enrich"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/selfupdate"
	"lancast/internal/transcode"
	"lancast/internal/update"
)

// Activity is one thing the server is doing right now, normalized so the client
// renders a single shape rather than four. The pieces already existed behind
// /api/libraries/{id}/scan, /api/enrich and /api/probe; what was missing was one
// place that answers "what is the server doing?" without the caller knowing
// which worker to ask.
type Activity struct {
	// Kind names the worker: scan, enrich, probe, coverart, transcode.
	Kind string `json:"kind"`
	// ID is stable for the lifetime of the task, so a client can key a list.
	ID string `json:"id"`
	// Title is display text, already resolved server-side — a scan says which
	// library, because the client should not have to join to find out.
	Title string `json:"title"`
	// State is running, failed, or done. Only running and failed are returned;
	// done exists so a future recent-history view has a value to use.
	State string `json:"state"`
	// Done and Total describe progress. Total 0 means indeterminate: a scan
	// knows how many files it has seen, never how many it will see.
	Done  int `json:"done"`
	Total int `json:"total"`
	// Detail is a short human phrase, not a duplicate of Done/Total.
	Detail string `json:"detail,omitempty"`
	// LibraryID is set for scans only.
	LibraryID int64 `json:"library_id,omitempty"`
	// StartedAt is unix seconds, when the worker records one.
	StartedAt int64  `json:"started_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// snapshot is what the workers report at one instant. Gathering it and shaping
// it are split for the same reason probing is: the shaping rules are then
// testable against fixtures, with no worker running and no media on disk.
type snapshot struct {
	scans    []scanSnapshot
	enrich   enrich.Stats
	probe    probe.Stats
	covers   coverart.Stats
	sessions []transcode.SessionInfo
	update   update.State
	staged   string
	download update.Progress
}

// scanSnapshot pairs a scan's progress with the library name, resolved here so
// the client never has to join an id back to a name.
type scanSnapshot struct {
	name     string
	progress scan.Progress
}

// activity answers "what is the server doing right now?" in one request.
// It reports live state only: nothing here is persisted, so a restart shows an
// idle server, which is the truth.
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	libs, err := s.st.ListLibraries(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list libraries")
		return
	}
	snap := snapshot{}
	for _, lib := range libs {
		snap.scans = append(snap.scans, scanSnapshot{lib.Name, s.scanner.Status(lib.ID)})
	}
	if s.worker != nil {
		snap.enrich = s.worker.Stats()
	}
	if s.probes != nil {
		snap.probe = s.probes.Stats()
	}
	if s.covers != nil {
		snap.covers = s.covers.Stats()
	}
	if s.trans != nil {
		snap.sessions = s.trans.Sessions()
	}
	if s.updates != nil {
		snap.update = s.updates.State()
	}
	if m, ok := selfupdate.Pending(s.dataDir); ok {
		snap.staged = m.Version
	}
	if s.updates != nil {
		snap.download = s.updates.Progress()
	}

	tasks := buildActivity(snap)
	writeJSON(w, http.StatusOK, map[string]any{
		"active": len(tasks) > 0,
		"tasks":  tasks,
	})
}

// buildActivity turns worker state into the client's one shape. It always
// returns a non-nil slice: a client should not have to defend against two
// spellings of "nothing".
func buildActivity(snap snapshot) []Activity {
	tasks := []Activity{}

	for _, sc := range snap.scans {
		p := sc.progress
		switch p.State {
		case scan.StateRunning, scan.StateFailed:
		default:
			continue
		}
		a := Activity{
			Kind:      "scan",
			ID:        "scan:" + strconv.FormatInt(p.LibraryID, 10),
			Title:     "Scanning " + sc.name,
			State:     string(p.State),
			Done:      p.FilesSeen,
			LibraryID: p.LibraryID,
			StartedAt: p.StartedAt,
			Error:     p.Error,
		}
		if p.ItemsChanged > 0 {
			a.Detail = strconv.Itoa(p.ItemsChanged) + " changed"
		}
		tasks = append(tasks, a)
	}

	if st := snap.enrich; st.Running {
		tasks = append(tasks, Activity{
			Kind: "enrich", ID: "enrich", Title: "Fetching metadata",
			State: "running", Done: st.Enriched, Total: st.Total,
			Detail: failedDetail(st.Failed), StartedAt: st.UpdatedAt,
		})
	}
	if st := snap.probe; st.Running {
		tasks = append(tasks, Activity{
			Kind: "probe", ID: "probe", Title: "Reading media files",
			State: "running", Done: st.Probed, Total: st.Total,
			Detail: failedDetail(st.Failed),
		})
	}
	if st := snap.covers; st.Running {
		tasks = append(tasks, Activity{
			Kind: "coverart", ID: "coverart", Title: "Finding album artwork",
			State: "running", Done: st.Found, Total: st.Total,
			Detail: failedDetail(st.Failed),
		})
	}
	// Listed before live work: it is the one row that asks something of the
	// reader rather than reporting progress, and burying it under three
	// scanning rows would make it the thing nobody sees.
	if a, ok := updateActivity(snap.update, snap.staged, snap.download); ok {
		tasks = append(tasks, a)
	}

	for _, sess := range snap.sessions {
		if sess.Finished {
			continue
		}
		tasks = append(tasks, Activity{
			Kind: "transcode", ID: "transcode:" + sess.ID,
			Title: "Transcoding for playback", State: "running",
			Detail: sess.Output, Error: sess.Error,
		})
	}
	return tasks
}

func failedDetail(failed int) string {
	if failed <= 0 {
		return ""
	}
	return strconv.Itoa(failed) + " failed"
}
