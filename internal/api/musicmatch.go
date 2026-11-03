package api

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"lancast/internal/meta"
	"lancast/internal/meta/musicbrainz"
	"lancast/internal/store"
)

/*
 * Fix match for albums and artists (ADR 0115, step 3).
 *
 * An album is searched in MusicBrainz by title and album artist, as the
 * background pass searches it, so the dialog shows the same scores the pass
 * acted on. An artist is never searched by name: its candidates are the
 * artists its albums credit, with each one's share of the votes. Correcting
 * the artist of a mistagged folder therefore starts with its albums, which is
 * where the evidence is.
 *
 * Both are refused while music matching is off: off means nothing is asked of
 * MusicBrainz (docs/privacy.md), and a dialog is no exception.
 */

// mbID finds a MusicBrainz id in what somebody pasted: the id itself, or a
// musicbrainz.org address with one in it.
var mbID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func (s *Server) musicMatchingOff(w http.ResponseWriter) bool {
	if s.settings != nil && !s.settings.Get().MatchMusic {
		writeError(w, http.StatusConflict, "music_matching_off",
			"Identifying albums with MusicBrainz is off. Turn it on in Settings → Metadata.")
		return true
	}
	return false
}

func (s *Server) musicProvider(w http.ResponseWriter) (meta.Provider, bool) {
	p, ok := s.reg.Provider(musicbrainz.ID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "MusicBrainz is not available")
	}
	return p, ok
}

// albumArtist is the title of an album's parent artist, or "".
func (s *Server) albumArtist(r *http.Request, it *store.Item) string {
	if it.ParentID == nil {
		return ""
	}
	p, err := s.st.GetItem(r.Context(), *it.ParentID, "")
	if err != nil || p.Kind != string(meta.KindArtist) {
		return ""
	}
	return p.Title
}

func (s *Server) albumCandidates(w http.ResponseWriter, r *http.Request, it *store.Item) {
	if s.musicMatchingOff(w) {
		return
	}
	p, ok := s.musicProvider(w)
	if !ok {
		return
	}
	year := 0
	if it.Year != nil {
		year = *it.Year
	}
	artist := s.albumArtist(r, it)
	if v := strings.TrimSpace(r.URL.Query().Get("artist")); v != "" {
		artist = v
	}
	title := it.Title
	if v := strings.TrimSpace(r.URL.Query().Get("q")); v != "" {
		// A pasted id or address is that record exactly, scored against the
		// album as it is so the number still means something.
		if id := mbID.FindString(strings.ToLower(v)); id != "" {
			s.exactAlbum(w, r, p, id, meta.AlbumQuery(it.Title, artist, year))
			return
		}
		// A typed title is a fresh search, so the year goes, as for films.
		title, year = v, 0
	}
	q := meta.AlbumQuery(title, artist, year)
	cands, err := p.Search(r.Context(), q)
	if err != nil {
		writeMusicError(w, err)
		return
	}
	for i := range cands {
		cands[i].Provider = p.ID()
	}
	meta.Rank(q, cands)
	if len(cands) > 20 {
		cands = cands[:20]
	}
	writeJSON(w, http.StatusOK, noCandidatesAsEmpty(cands))
}

func (s *Server) exactAlbum(w http.ResponseWriter, r *http.Request, p meta.Provider, id string, q meta.Query) {
	rec, err := p.Fetch(r.Context(), meta.Ref{Kind: meta.KindAlbum, ExternalID: id})
	if errors.Is(err, musicbrainz.ErrNotFound) || (err == nil && rec == nil) {
		writeJSON(w, http.StatusOK, []meta.Candidate{})
		return
	}
	if err != nil {
		writeMusicError(w, err)
		return
	}
	c := meta.Candidate{Provider: p.ID(), ExternalID: rec.ExternalID, Kind: meta.KindAlbum}
	if rec.Fields.Title != nil {
		c.Title = *rec.Fields.Title
	}
	if rec.Fields.Year != nil {
		c.Year = *rec.Fields.Year
	}
	if rec.Music != nil {
		c.ReleaseType = rec.Music.ReleaseType
		var names []string
		for _, a := range rec.Music.Artists {
			names = append(names, a.Name)
		}
		c.Artist = strings.Join(names, ", ")
		c.ArtistNames = names
	}
	cands := []meta.Candidate{c}
	meta.Rank(q, cands)
	writeJSON(w, http.StatusOK, cands)
}

/*
 * artistCandidates are the artists an artist's albums credit, most votes
 * first. Score is each one's share; Breakdown.total says the same. The name
 * is the catalogue's, fetched per candidate, which is a request each: an
 * artist's albums rarely name more than two or three.
 */
func (s *Server) artistCandidates(w http.ResponseWriter, r *http.Request, it *store.Item) {
	if s.musicMatchingOff(w) {
		return
	}
	p, ok := s.musicProvider(w)
	if !ok {
		return
	}
	votes, _, err := s.st.ArtistCreditVotes(r.Context(), it.ID)
	if err != nil {
		s.writeInternal(w, err, "artist votes")
		return
	}
	if it.Provider != nil && *it.Provider == p.ID() && it.ExternalID != nil && *it.ExternalID != "" {
		if _, seen := votes[*it.ExternalID]; !seen {
			votes[*it.ExternalID] = 0 // the current answer, so it can be seen
		}
	}
	total := 0
	for _, n := range votes {
		total += n
	}
	cands := []meta.Candidate{}
	for id, n := range votes {
		rec, err := p.Fetch(r.Context(), meta.Ref{Kind: meta.KindArtist, ExternalID: id})
		if errors.Is(err, musicbrainz.ErrNotFound) {
			continue
		}
		if err != nil {
			writeMusicError(w, err)
			return
		}
		c := meta.Candidate{Provider: p.ID(), ExternalID: id, Kind: meta.KindArtist}
		if rec != nil && rec.Fields.Title != nil {
			c.Title = *rec.Fields.Title
		}
		if total > 0 {
			c.Score = float64(n) / float64(total)
		}
		c.Breakdown.Total = c.Score
		cands = append(cands, c)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Title < cands[j].Title
	})
	writeJSON(w, http.StatusOK, cands)
}

// applyMusicMatch applies a person's choice, or writes the refusal and
// reports false.
func (s *Server) applyMusicMatch(w http.ResponseWriter, r *http.Request, it *store.Item, provider, externalID string) bool {
	if provider != musicbrainz.ID {
		writeError(w, http.StatusBadRequest, "bad_request", "an album or an artist is matched from musicbrainz")
		return false
	}
	if s.musicMatchingOff(w) {
		return false
	}
	if _, ok := s.musicProvider(w); !ok {
		return false
	}
	if !mbID.MatchString(externalID) {
		writeError(w, http.StatusBadRequest, "bad_request", "not a MusicBrainz id")
		return false
	}
	if err := s.worker.ApplyMusicMatch(r.Context(), *it, provider, externalID); err != nil {
		if errors.Is(err, musicbrainz.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "not_found", "MusicBrainz has no such record")
			return false
		}
		writeMusicError(w, err)
		return false
	}
	return true
}

// writeMusicError says "busy" as busy: MusicBrainz turning a request away is
// a reason to try again shortly, not a server fault.
func writeMusicError(w http.ResponseWriter, err error) {
	if errors.Is(err, musicbrainz.ErrBusy) {
		writeError(w, http.StatusServiceUnavailable, "busy", "MusicBrainz is busy. Try again in a moment.")
		return
	}
	writeError(w, http.StatusBadGateway, "unavailable", "Could not reach MusicBrainz.")
}

func noCandidatesAsEmpty(c []meta.Candidate) []meta.Candidate {
	if c == nil {
		return []meta.Candidate{}
	}
	return c
}
