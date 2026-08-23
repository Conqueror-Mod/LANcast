package api

import (
	"fmt"
	"net/http"
	"strconv"

	"lancast/internal/coverart"
	"lancast/internal/enrich"
	"lancast/internal/photo"
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
	photos   photo.Stats
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
	if s.photos != nil {
		snap.photos = s.photos.Stats()
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
	/*
	 * `staged` is stated outright, not left to be inferred from a task id.
	 *
	 * The shell's stale-client banner needs it and cannot ask /api/update,
	 * which is admin-only — the banner is shown to everyone, because a window
	 * older than its server is a fact about the app in front of you rather than
	 * an administrative one. This endpoint is already unrestricted and already
	 * knows.
	 *
	 * Without it the banner told everyone to close and reopen whenever the
	 * versions differed, including when nothing was waiting to be applied — in
	 * which case reopening runs the same binary, the versions still differ, and
	 * the advice repeats for ever.
	 */
	body := map[string]any{
		"active": len(tasks) > 0,
		"tasks":  tasks,
	}
	if snap.staged != "" {
		body["staged"] = snap.staged
	}
	writeJSON(w, http.StatusOK, body)
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
	if st := snap.photos; st.Running {
		// Unsupported is reported beside failures rather than folded into them:
		// a HEIC on a machine with no ffmpeg is not a broken file, and telling
		// someone their photos failed when the install is what is incomplete
		// sends them to look in the wrong place.
		tasks = append(tasks, Activity{
			Kind: "photos", ID: "photos", Title: "Making photo thumbnails",
			State: "running", Done: st.Done, Total: st.Total,
			Detail: photoDetail(st),
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

// photoDetail names what could not be turned into a thumbnail, keeping the two
// reasons apart. "3 failed" and "3 in a format this build cannot read" send a
// reader to different places, and the second is fixed by installing ffmpeg
// rather than by suspecting the files.
func photoDetail(st photo.Stats) string {
	switch {
	case st.Failed > 0 && st.Unsupported > 0:
		return fmt.Sprintf("%d failed, %d unsupported", st.Failed, st.Unsupported)
	case st.Unsupported > 0:
		return fmt.Sprintf("%d in an unsupported format", st.Unsupported)
	default:
		return failedDetail(st.Failed)
	}
}
