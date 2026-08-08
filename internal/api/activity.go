package api

import (
	"net/http"

	"lancast/internal/coverart"
	"lancast/internal/enrich"
	"lancast/internal/scan"
)

// activity is what the server is doing right now, in one answer.
//
// The four background workers have had their own status endpoints since they
// were written, and each one is honest. What was missing is a place that shows
// them together: a library that looks wrong while a scan is halfway through, or
// a grid of blank tiles while cover art is still being extracted, are both
// normal states that look identical to broken ones. This is the "see the
// failure" rule (roadmap) applied to work in progress rather than to errors.
//
// One endpoint rather than four because the client polls it: four requests on a
// timer to answer one question — is anything happening — is three too many, and
// the aggregate is also the only place that can answer it truthfully in a
// single instant rather than across four staggered replies.
type activity struct {
	// Busy is the one field the UI branches on, and the one the client uses to
	// decide how often to ask again. Derived here rather than in the client so
	// that "is LANcast doing something" has exactly one definition.
	Busy bool `json:"busy"`

	Scans    []libraryScan  `json:"scans"`
	Enrich   enrich.Stats   `json:"enrich"`
	Probe    probeActivity  `json:"probe"`
	CoverArt coverart.Stats `json:"coverart"`
}

// libraryScan is one library's scan, named rather than numbered: "scanning
// library 3" is not something anyone can act on.
type libraryScan struct {
	LibraryID int64  `json:"library_id"`
	Name      string `json:"name"`
	scan.Progress
}

type probeActivity struct {
	Available bool `json:"available"`
	Running   bool `json:"running"`
	Probed    int  `json:"probed"`
	Failed    int  `json:"failed"`
	Remaining int  `json:"remaining"`
	Total     int  `json:"total"`
}

// activityStatus reports every background worker at once.
//
// Readable by any signed-in user, not just an admin: what the server is busy
// with is the explanation for what someone is looking at, and a member staring
// at a half-scanned library deserves the same answer an admin gets. Nothing
// here is a secret — it is counts and library names, both of which they can
// already see.
func (s *Server) activityStatus(w http.ResponseWriter, r *http.Request) {
	act := activity{Scans: []libraryScan{}}

	libs, err := s.st.ListLibraries(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list libraries")
		return
	}
	for _, lib := range libs {
		p := s.scanner.Status(lib.ID)
		// Only what is happening now. A finished scan is history, and a status
		// panel that lists every scan since boot is a log, not a status.
		if p.State != scan.StateRunning {
			continue
		}
		act.Scans = append(act.Scans, libraryScan{
			LibraryID: lib.ID, Name: lib.Name, Progress: p,
		})
		act.Busy = true
	}

	if s.worker != nil {
		st := s.worker.Stats()
		act.Enrich = st
		if st.Running {
			act.Busy = true
		}
	}
	if s.probes != nil {
		st := s.probes.Stats()
		act.Probe = probeActivity{
			Available: s.probes.Available(),
			Running:   st.Running,
			Probed:    st.Probed,
			Failed:    st.Failed,
			Remaining: st.Remaining,
			Total:     st.Total,
		}
		if st.Running {
			act.Busy = true
		}
	}
	if s.covers != nil {
		st := s.covers.Stats()
		act.CoverArt = st
		if st.Running {
			act.Busy = true
		}
	}

	writeJSON(w, http.StatusOK, act)
}
