package api

import (
	"net/http"
	"os"
	"strconv"

	"lancast/internal/preview"
	"lancast/internal/store"
)

/*
 * Scrub-bar previews (ADR 0105): the sheets the preview worker made for an
 * item, and what shape they are.
 *
 * The item is looked up the way every item route looks it up, so a title
 * kept from an account by a ceiling or a hidden library is a 404 here too.
 * A sheet is addressed by its number, never by a name, and the files are
 * under the data directory, not a library root, so nothing a client sends
 * becomes a path.
 */

// previewsFor resolves an item's manifest, or answers 404 and returns nil.
// A manifest for a file that has since been replaced is not the item's.
func (s *Server) previewsFor(w http.ResponseWriter, r *http.Request) (*store.Item, *preview.Manifest) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return nil, nil
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return nil, nil
	}
	if s.previewsDir == "" {
		writeError(w, http.StatusNotFound, "not_found", "no previews for this item")
		return nil, nil
	}
	m, err := preview.Load(s.previewsDir, it.ID)
	if err != nil || m.Key != store.PreviewKey(*it, preview.Rev) {
		/*
		 * None yet, and somebody has this open in the player: make it next
		 * rather than after the rest of the library, and say so, so the
		 * player knows asking again soon is worth it. Only films and
		 * episodes, and only while the setting is on.
		 */
		if s.previewsWant != nil && s.settings.Get().ScrubPreviews &&
			(it.Kind == "movie" || it.Kind == "episode") {
			s.previewsWant(it.ID)
			writeError(w, http.StatusNotFound, "queued", "previews for this item are being made next")
			return nil, nil
		}
		writeError(w, http.StatusNotFound, "not_found", "no previews for this item")
		return nil, nil
	}
	return it, m
}

// itemPreviews answers GET /api/items/{id}/previews: the sheets' shape.
func (s *Server) itemPreviews(w http.ResponseWriter, r *http.Request) {
	_, m := s.previewsFor(w, r)
	if m == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"interval_ms": m.IntervalMS,
		"columns":     m.Columns,
		"rows":        m.Rows,
		"width":       m.Width,
		"height":      m.Height,
		"sheets":      m.Sheets,
	})
}

// itemPreviewSheet answers GET /api/items/{id}/previews/{n}: sheet n, from 0.
func (s *Server) itemPreviewSheet(w http.ResponseWriter, r *http.Request) {
	it, m := s.previewsFor(w, r)
	if m == nil {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= m.Sheets {
		writeError(w, http.StatusNotFound, "not_found", "no such sheet")
		return
	}
	f, err := os.Open(preview.SheetPath(s.previewsDir, it.ID, n))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such sheet")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such sheet")
		return
	}
	// A sheet never changes under its key; a new file makes a new manifest
	// and the client asks again. Revalidated anyway, like every image here.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}
