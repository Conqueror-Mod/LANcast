package api

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"lancast/internal/media"
	"lancast/internal/meta"
	"lancast/internal/retro/identify"
	"lancast/internal/retro/retrodb"
	"lancast/internal/store"
)

/*
 * Fix match for a ROM (ADR 0073).
 *
 * A game is not a film, and the film and TV providers are the wrong place to
 * look for one: Fix match on a ROM used to search them, found nothing for a
 * game, and would have offered a film of the same name to be applied to a
 * cartridge. A ROM is matched against the installed DATs instead, on its own
 * console, and the choice goes through the identify worker — the same code an
 * automatic match takes — and is then locked.
 */

// romCandidateLimit keeps a broad search ("fire emblem" across a console's
// DAT) to a list a person can read.
const romCandidateLimit = 40

func (s *Server) romIndex() *retrodb.Index {
	if s.retroDB == nil {
		return nil
	}
	return s.retroDB.Index()
}

func (s *Server) romCandidates(w http.ResponseWriter, r *http.Request, it *store.Item) {
	ix := s.romIndex()
	if ix == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"The ROM database is not installed. Download it in Settings → Retro games.")
		return
	}
	platform := ""
	if it.Platform != nil {
		platform = *it.Platform
	}
	if platform == "" {
		writeError(w, http.StatusConflict, "no_platform",
			"LANcast does not know which console this game is for, so there is nothing to search.")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		// The filename's title, not a title a previous match wrote: Fix match
		// exists to correct that match, and searching it would circle it.
		q = media.ROMTitle(filepath.Base(it.Path))
	}
	games := ix.Search(platform, q, romCandidateLimit)
	cands := make([]meta.Candidate, 0, len(games))
	want := strings.ToLower(strings.TrimSpace(q))
	for _, g := range games {
		// The DAT name is shown whole: the region and revision tags are what
		// tell one line from the next, and they are the choice being made.
		c := meta.Candidate{
			Provider:   identify.Provider,
			ExternalID: g.Name,
			Kind:       meta.Kind(media.KindROM),
			Title:      g.Name,
			Year:       g.Year,
			Overview:   g.Genre,
			PosterURL:  retrodb.ThumbnailURL(platform, retrodb.Boxart, g.Name),
		}
		// One measure, honestly named: how much of the title is the search.
		title := strings.ToLower(media.ROMTitle(g.Name))
		switch {
		case title == want:
			c.Breakdown.Title = 1
		case title != "":
			c.Breakdown.Title = float64(len(want)) / float64(max(len(title), len(want)))
		}
		c.Score = c.Breakdown.Title
		cands = append(cands, c)
	}
	writeJSON(w, http.StatusOK, cands)
}

func (s *Server) applyROMMatch(w http.ResponseWriter, r *http.Request, it *store.Item, provider, name string) {
	if provider != identify.Provider {
		writeError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("a game is matched from %s, not %s", identify.Provider, provider))
		return
	}
	if s.retro == nil || s.romIndex() == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"The ROM database is not installed. Download it in Settings → Retro games.")
		return
	}
	if err := s.retro.ApplyChosen(r.Context(), *it, name); err != nil {
		if errors.Is(err, identify.ErrNotInDAT) {
			writeError(w, http.StatusBadRequest, "not_found", err.Error())
			return
		}
		s.writeInternal(w, err, "apply rom match")
		return
	}
	s.audit(r, "item.match", "item", auditID(it.ID),
		fmt.Sprintf("Set %q to %s:%s (was %s)", it.Title, provider, name, formerIdentity(it)),
		map[string]any{
			"provider": provider, "external_id": name, "kind": string(media.KindROM),
			"previous_provider": it.Provider, "previous_external_id": it.ExternalID,
			"previous_state": it.MatchState,
		})
	s.respondItem(w, r, it.ID)
}
