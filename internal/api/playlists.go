package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"lancast/internal/store"
)

// Playlist editing (ADR 0030).
//
// Four writes, and the shape of them is the ADR's central claim in code: the
// database is the source of truth, an .m3u on disk seeded it once and is not
// watched afterwards. Nothing here writes a file.
//
// WHY EVERY WRITE LOCKS `members`
//
// The importer skips any playlist carrying a members lock (internal/scan/
// playlists.go), which is the locked-fields rule applied to membership: a
// rescan reconciles files, it does not re-litigate decisions. So the lock is
// not bookkeeping that could be deferred — it is the only thing standing
// between a human's edit and the next scan quietly restoring the .m3u. It is
// set on the way *in*, before the membership write, so a crash between the two
// leaves a playlist that is protected but stale rather than edited and
// unprotected. The importer re-running is recoverable; an undone edit is the
// failure this exists to prevent.
//
// # WHY THESE ARE NOT ADMIN-ONLY
//
// The management surfaces are gated on admin because they are filesystem
// access or account control — adding a library is arbitrary read access. A
// playlist edit is neither: it touches no file, no path, and no identity, and
// the audit log records who did it. Playlists are server-wide until ADR 0030's
// open question ("mine versus the server's") is decided awake, and a shared
// list every member can edit is the smaller claim of the two available; the
// alternative is a household where only the admin may make a playlist.
const membersLock = "members"

// playlistByID loads an item and insists it is a playlist.
//
// A kind check rather than trusting the route: /api/playlists/{id} with an
// album's id would otherwise write playlist_entry rows against a row nothing
// ever reads them for, and the caller would get a cheerful 204.
func (s *Server) playlistByID(w http.ResponseWriter, r *http.Request) (*store.Item, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid playlist id")
		return nil, false
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such playlist") {
		return nil, false
	}
	if it.Kind != "playlist" {
		writeError(w, http.StatusBadRequest, "bad_request", "that item is not a playlist")
		return nil, false
	}
	return it, true
}

// decodeItemIDs reads an {"item_ids": [...]} body and checks every id exists.
//
// Checked here rather than left to the foreign key, so a client holding a stale
// id gets a 400 naming it instead of a 500 with a constraint violation in the
// server log. Repeats are legal and are passed through untouched — a playlist
// holding the same track twice is the point of this table.
func (s *Server) decodeItemIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var req struct {
		ItemIDs []int64 `json:"item_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return nil, false
	}
	found, err := s.st.ExistingItemIDs(r.Context(), req.ItemIDs)
	if err != nil {
		s.writeInternal(w, err, "existing item ids")
		return nil, false
	}
	for _, id := range req.ItemIDs {
		if !found[id] {
			writeError(w, http.StatusBadRequest, "bad_request",
				"no such item: "+strconv.FormatInt(id, 10))
			return nil, false
		}
	}
	return req.ItemIDs, true
}

// createPlaylist makes an empty playlist in a library.
//
// A library is required rather than guessed. Every media_item belongs to one,
// and a server with a films library and a music library has no defensible
// default — a playlist filed in the wrong one disappears from the browse it was
// made in.
func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		LibraryID int64  `json:"library_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "title is required")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), req.LibraryID); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}

	// Not media.SortTitle: it strips leading articles, which is right for "The
	// Godfather" and wrong for a playlist someone named "The Gym One" and
	// expects to find under T. Same reasoning as the importer's local sortTitle.
	id, err := s.st.CreatePlaylist(r.Context(), req.LibraryID, title, strings.ToLower(title))
	if err != nil {
		s.writeInternal(w, err, "create playlist")
		return
	}
	// Locked from birth. This playlist has no .m3u behind it, so nothing would
	// re-import it today — but the lock is what marks membership as user-owned,
	// and a playlist whose protection depends on how it happened to be created
	// is a rule with a hole in it.
	if err := s.st.LockField(r.Context(), id, membersLock); err != nil {
		s.writeInternal(w, err, "lock members")
		return
	}
	s.audit(r, "playlist.create", "item", auditID(id), "created playlist "+title, nil)
	s.respondItem(w, r, id)
}

// setPlaylistEntries replaces a playlist's membership with exactly this list.
//
// Replace rather than merge, because a playlist is an ordered sequence and
// there is no sensible way to merge two orderings — a reorder, an insert, and a
// removal are all "the caller has decided the whole thing", and this is how
// they say so. Add-to-the-end has its own route because it is the one edit
// whose position the caller does not have to decide.
func (s *Server) setPlaylistEntries(w http.ResponseWriter, r *http.Request) {
	pl, ok := s.playlistByID(w, r)
	if !ok {
		return
	}
	ids, ok := s.decodeItemIDs(w, r)
	if !ok {
		return
	}
	if err := s.st.LockField(r.Context(), pl.ID, membersLock); err != nil {
		s.writeInternal(w, err, "lock members")
		return
	}
	if err := s.st.SetPlaylistEntries(r.Context(), pl.ID, ids); err != nil {
		s.writeInternal(w, err, "set playlist entries")
		return
	}
	s.audit(r, "playlist.edit", "item", auditID(pl.ID),
		"set "+strconv.Itoa(len(ids))+" entries in "+pl.Title, map[string]any{"item_ids": ids})
	w.WriteHeader(http.StatusNoContent)
}

// addPlaylistEntries appends to the end, in the order given.
func (s *Server) addPlaylistEntries(w http.ResponseWriter, r *http.Request) {
	pl, ok := s.playlistByID(w, r)
	if !ok {
		return
	}
	ids, ok := s.decodeItemIDs(w, r)
	if !ok {
		return
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "item_ids must not be empty")
		return
	}
	if err := s.st.LockField(r.Context(), pl.ID, membersLock); err != nil {
		s.writeInternal(w, err, "lock members")
		return
	}
	if err := s.st.AppendPlaylistEntries(r.Context(), pl.ID, ids); err != nil {
		s.writeInternal(w, err, "append playlist entries")
		return
	}
	s.audit(r, "playlist.edit", "item", auditID(pl.ID),
		"added "+strconv.Itoa(len(ids))+" to "+pl.Title, map[string]any{"item_ids": ids})
	w.WriteHeader(http.StatusNoContent)
}

// removePlaylistEntry deletes one entry by its position.
//
// By position, not by item id: an id does not identify an entry when the same
// track may appear twice, and "remove the one I am looking at" is the only
// request a client can actually make. Positions are 0-based and dense, so the
// position is the index the client rendered.
func (s *Server) removePlaylistEntry(w http.ResponseWriter, r *http.Request) {
	pl, ok := s.playlistByID(w, r)
	if !ok {
		return
	}
	pos, err := strconv.Atoi(r.PathValue("pos"))
	if err != nil || pos < 0 {
		writeError(w, http.StatusBadRequest, "bad_request",
			"position must be a whole number, counting from 0")
		return
	}
	if err := s.st.LockField(r.Context(), pl.ID, membersLock); err != nil {
		s.writeInternal(w, err, "lock members")
		return
	}
	err = s.st.RemovePlaylistEntry(r.Context(), pl.ID, pos)
	if s.notFoundOr(w, err, "remove playlist entry", "no entry at that position") {
		return
	}
	s.audit(r, "playlist.edit", "item", auditID(pl.ID),
		"removed entry "+strconv.Itoa(pos)+" from "+pl.Title, nil)
	w.WriteHeader(http.StatusNoContent)
}

// deletePlaylist removes the playlist and its entries, and nothing else.
//
// Its own route rather than DELETE /api/items/{id}, which asks for a mode
// because it is about files: 'delete' would remove the .m3u that seeded an
// imported playlist, and 'ignore' would add that .m3u to the ignore list — two
// different filesystem side effects for what a person meant as "I don't want
// this list any more". Entries go with it by cascade; the tracks themselves are
// untouched, because being in a playlist was never where they lived.
func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	pl, ok := s.playlistByID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteItems(r.Context(), []int64{pl.ID}); err != nil {
		s.writeInternal(w, err, "delete playlist")
		return
	}
	s.audit(r, "playlist.delete", "item", auditID(pl.ID), "deleted playlist "+pl.Title, nil)
	w.WriteHeader(http.StatusNoContent)
}
