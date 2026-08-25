package api

import (
	"net/http"
	"strconv"

	"lancast/internal/scan"
	"lancast/internal/store"
)

/*
 * The collision report (ADR 0042).
 *
 * Two files claiming one work is five different situations and the server
 * cannot tell which: on the library that motivated the decision, thirteen pairs
 * shared a provider id and two of them were not duplicates at all — one film in
 * two parts, and one outright misfile where a stale `.nfo` gave a 1989 film a
 * 2022 film's identity.
 *
 * So this endpoint reports and does nothing else. There is no merge, no rank,
 * no "keep the best copy", and no delete. It is the `shape_warning` posture
 * applied to items rather than libraries: be loud at the moment it happens, and
 * let the person decide.
 *
 * **Admin only, because it returns paths.** Every other item response withholds
 * `path` deliberately, and this one cannot: the whole value of the report is
 * being able to go and look at the two files. A collision the reader cannot
 * locate on disk is a notification, not a report.
 */
func (s *Server) collisions(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("library_id"), 10, 64)

	found, err := s.st.Collisions(r.Context(), libraryID)
	if err != nil {
		s.writeInternal(w, err, "collisions")
		return
	}

	/*
	 * The byte comparison is opt-in, per collision.
	 *
	 * Sampling three windows of a 14.6 GB file is cheap next to reading it and
	 * expensive next to nothing, and a report is opened far more often than any
	 * one row in it is investigated. Doing every collision on every load would
	 * make the page's cost proportional to a library's mistakes.
	 *
	 * `?compare=<external_id>` asks for exactly one. The client asks when a
	 * person clicks, which is the moment the answer is wanted.
	 */
	compare := r.URL.Query().Get("compare")

	/*
	 * Explicit response types rather than embedding the store's.
	 *
	 * An embedded struct plus a shadowing field of the same JSON name works and
	 * is a puzzle at the call site; the wire shape of a report somebody builds
	 * a client against is worth stating outright (ADR 0018).
	 */
	type member struct {
		store.CollisionMember
		// Fingerprint is present only for a compared collision. Absent means
		// "not asked", never "differs".
		Fingerprint string `json:"fingerprint,omitempty"`
		// Unreadable separates an absence of evidence from a mismatch: a file
		// that could not be opened must not read as a different file.
		Unreadable bool `json:"unreadable,omitempty"`
	}
	type collision struct {
		Provider   string   `json:"provider"`
		ExternalID string   `json:"external_id"`
		SameSize   bool     `json:"same_size"`
		Members    []member `json:"members"`
		/*
		 * SameBytes is a tri-state and the name is deliberate: it says
		 * "identical so far as sampled", never "identical". Three 1 MB windows
		 * and a size cannot prove equality. Nothing acts on it -- it is shown
		 * to somebody deciding, which is a different risk from a sampled hash
		 * that authorises a delete.
		 */
		SameBytes *bool `json:"same_bytes,omitempty"`
	}

	rows := make([]collision, 0, len(found))
	for _, c := range found {
		row := collision{
			Provider: c.Provider, ExternalID: c.ExternalID, SameSize: c.SameSize,
			Members: make([]member, 0, len(c.Members)),
		}
		wanted := compare != "" && compare == c.ExternalID

		var hashes []string
		for _, m := range c.Members {
			mem := member{CollisionMember: m}
			if wanted {
				if hash, ok := s.fingerprintItem(r, m.ID); ok {
					mem.Fingerprint = hash
					hashes = append(hashes, hash)
				} else {
					mem.Unreadable = true
				}
			}
			row.Members = append(row.Members, mem)
		}

		// Only when every member was read. A partial comparison cannot answer
		// the question that was asked.
		if wanted && len(hashes) == len(c.Members) && len(hashes) > 1 {
			same := true
			for _, h := range hashes[1:] {
				if h != hashes[0] {
					same = false
					break
				}
			}
			row.SameBytes = &same
		}
		rows = append(rows, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{"collisions": rows})
}

/*
 * fingerprintItem samples one item's file, or reports that it could not.
 *
 * Resolved through `itemFilePath`, which is the same containment check every
 * other handler that turns a row into a path uses. The database is trusted and
 * this is the boundary where a bad row becomes arbitrary file access -- a
 * report is not an exemption from that rule.
 *
 * The three failures are deliberately one answer. Whether the row vanished, its
 * path escaped its root, or the file would not open, the honest thing to tell a
 * reader is the same: no evidence. Distinguishing them here would offer a
 * diagnosis the report cannot support, and conflating any of them with "the
 * files differ" would be worse -- a missing file is not a different file.
 */
func (s *Server) fingerprintItem(r *http.Request, id int64) (string, bool) {
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if err != nil {
		return "", false
	}
	path, err := s.itemFilePath(r, it)
	if err != nil {
		return "", false
	}
	fp, err := scan.FingerprintFile(path)
	if err != nil {
		return "", false
	}
	return fp.Hash, true
}
