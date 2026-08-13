package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"lancast/internal/store"
	"lancast/internal/subtitle"
)

// subtitleTrack is one selectable track, embedded or external.
type subtitleTrack struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Language string `json:"language,omitempty"`
	Source   string `json:"source"` // embedded | sidecar | downloaded
	Codec    string `json:"codec,omitempty"`
	Forced   bool   `json:"forced"`
	Default  bool   `json:"default"`

	// Available is false for tracks that cannot become WebVTT. They are still
	// listed, with a reason — hiding them would leave a viewer wondering why a
	// film they know has subtitles appears to have none.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// listSubtitles returns every subtitle track for an item.
func (s *Server) listSubtitles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	streams, err := s.st.Streams(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "load streams")
		return
	}
	externals, err := s.st.ExternalSubtitles(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "load subtitles")
		return
	}

	tracks := make([]subtitleTrack, 0, len(streams)+len(externals))

	for _, st := range streams {
		if st.Kind != "subtitle" {
			continue
		}
		kind := subtitle.ClassifyCodec(st.Codec)
		tracks = append(tracks, subtitleTrack{
			Key:       "embedded-" + strconv.Itoa(st.Index),
			Label:     subtitleLabel(st.Language, st.Title, st.Forced, "Embedded"),
			Language:  subtitle.NormalizeLanguage(st.Language),
			Source:    "embedded",
			Codec:     st.Codec,
			Forced:    st.Forced,
			Default:   st.Default,
			Available: kind == subtitle.Text,
			Reason:    subtitle.UnsupportedReason(st.Codec),
		})
	}

	for _, ext := range externals {
		tracks = append(tracks, subtitleTrack{
			Key:       "external-" + strconv.FormatInt(ext.ID, 10),
			Label:     subtitleLabel(ext.Language, ext.Title, ext.Forced, "File"),
			Language:  subtitle.NormalizeLanguage(ext.Language),
			Source:    ext.Source,
			Codec:     ext.Format,
			Forced:    ext.Forced,
			Available: true,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"item_id": id,
		"tracks":  tracks,
	})
}

// serveSubtitle returns one track as WebVTT.
func (s *Server) serveSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	key := strings.TrimSuffix(r.PathValue("key"), ".vtt")
	kind, rest, ok := strings.Cut(key, "-")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid subtitle key")
		return
	}

	var body []byte
	switch kind {
	case "embedded":
		body, err = s.embeddedSubtitle(r, id, it, rest)
	case "external":
		body, err = s.externalSubtitle(r, id, rest)
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "invalid subtitle key")
		return
	}

	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "no such subtitle track")
		case errors.Is(err, subtitle.ErrUnsupported):
			// A bitmap track is not a server fault; the client should have
			// shown it as unavailable, and saying so plainly beats a 500.
			writeError(w, http.StatusUnprocessableEntity, "unsupported", err.Error())
		case errors.Is(err, subtitle.ErrNotInstalled):
			writeError(w, http.StatusServiceUnavailable, "unavailable",
				"ffmpeg is needed to read this subtitle track")
		default:
			s.log.Warn("subtitle conversion failed", "item", id, "key", key, "error", err)
			writeError(w, http.StatusUnprocessableEntity, "unsupported",
				"this subtitle track could not be converted")
		}
		return
	}

	// A transcode restarts the media timeline at zero for the offset it was
	// asked to begin at, so a resumed film needs its cues moved earlier by the
	// same offset or they never reach the screen. The client passes that offset
	// as ?t=; direct play omits it and the subtitle is served unshifted.
	if t, err := strconv.ParseFloat(r.URL.Query().Get("t"), 64); err == nil && t > 0 {
		body = subtitle.ShiftVTT(body, t)
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	// Content depends on both the track and the offset, and both are in the URL,
	// so a short private cache is safe and keeps track switching instant.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(body)
}

func (s *Server) embeddedSubtitle(r *http.Request, id int64, it *store.Item, rest string) ([]byte, error) {
	index, err := strconv.Atoi(rest)
	if err != nil {
		return nil, store.ErrNotFound
	}

	streams, err := s.st.Streams(r.Context(), id)
	if err != nil {
		return nil, err
	}
	// The requested index must be a subtitle stream of this item — an index
	// from a URL is not a licence to extract an arbitrary stream.
	var codec string
	for _, st := range streams {
		if st.Index == index && st.Kind == "subtitle" {
			codec = st.Codec
			break
		}
	}
	if codec == "" {
		return nil, store.ErrNotFound
	}

	path, err := s.itemFilePath(r, it)
	if err != nil {
		return nil, err
	}
	return s.subs.Embedded(r.Context(), path, index, codec)
}

func (s *Server) externalSubtitle(r *http.Request, id int64, rest string) ([]byte, error) {
	subID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return nil, store.ErrNotFound
	}
	// Scoped to the item, so a crafted id cannot read a subtitle belonging to
	// something else.
	ext, err := s.st.ExternalSubtitle(r.Context(), id, subID)
	if err != nil {
		return nil, err
	}
	return s.subs.Sidecar(r.Context(), ext.Path, ext.Format)
}

/*
 * itemFilePath resolves an item's file, re-checking containment against the
 * location that item was scanned under (ADR 0034).
 *
 * The root comes from the *item*, not from its library. A library may hold
 * several locations, and the check must stay one root against one path: asking
 * "does any of this library's roots contain it" would accept a row pointing
 * under root B while belonging to root A, on the strength of some root
 * matching. That is the difference between a boundary and a search, and this is
 * the boundary where a bad row becomes arbitrary file access.
 *
 * An item with no recorded root resolves to nothing rather than falling back to
 * the library. Falling back would reintroduce exactly the ambiguity above, and
 * failing closed here costs one unplayable item where guessing costs the
 * property this function exists to hold.
 */
func (s *Server) itemFilePath(r *http.Request, it *store.Item) (string, error) {
	root, err := s.st.RootForItem(r.Context(), it.ID)
	if err != nil {
		return "", err
	}
	return containedPath(root.Path, it.Path)
}

// subtitleLabel builds what a person reads in the picker.
func subtitleLabel(language, title string, forced bool, fallback string) string {
	label := subtitle.DisplayLanguage(language)
	if language == "" {
		label = fallback
		if title != "" {
			label = title
		}
	} else if title != "" {
		label += " — " + title
	}
	if forced {
		label += " (forced)"
	}
	return label
}
