package api

import (
	"encoding/base64"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"lancast/internal/media"
)

/*
 * Pre-roll: a clip of the owner's choosing before a film (ADR 0099).
 *
 * The server decides whether one plays, so every client follows the same
 * rule: the feature is on (a folder is set), the account has not turned it
 * off for itself, the item is a film, and it is starting from the beginning —
 * never on a resume, because a clip before minute 47 is only an obstacle. One
 * clip from the folder, at random.
 *
 * The clip plays as a file, like an extra: nothing is recorded, it is never
 * watched, and it is not in anybody's history.
 */

type prerollClip struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// prerollClips lists the videos directly in the pre-roll folder. A key is the
// file's name, made opaque; it is looked up here, never turned into a path.
func prerollClips(dir string) map[string]string {
	out := map[string]string{}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() || !media.IsVideo(p) {
			continue
		}
		// A link leading out of the folder is not a clip (ADR 0094).
		if _, err := containedPath(dir, p); err != nil {
			continue
		}
		out[base64.RawURLEncoding.EncodeToString([]byte(e.Name()))] = p
	}
	return out
}

// getPreroll answers GET /api/preroll?item=ID&from_start=true: the clip to play
// before that film, or null.
func (s *Server) getPreroll(w http.ResponseWriter, r *http.Request) {
	none := map[string]any{"clip": nil}
	dir := s.settings.Get().PrerollDir
	if dir == "" {
		writeJSON(w, http.StatusOK, none)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("item"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "item is required")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Kind != "movie" {
		writeJSON(w, http.StatusOK, none)
		return
	}
	fromStart := r.URL.Query().Get("from_start") == "true"
	// A resume: a saved place in a film not yet finished.
	if !fromStart && it.Progress != nil && it.Progress.PositionMS > 0 && !it.Progress.Watched {
		writeJSON(w, http.StatusOK, none)
		return
	}
	if sess, ok := sessionFromContext(r); ok {
		if p, err := s.st.PlaybackPrefsFor(r.Context(), sess.UserID); err == nil && p.SkipPreroll {
			writeJSON(w, http.StatusOK, none)
			return
		}
	}
	clips := prerollClips(dir)
	if len(clips) == 0 {
		writeJSON(w, http.StatusOK, none)
		return
	}
	keys := make([]string, 0, len(clips))
	for k := range clips {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	key := keys[rand.IntN(len(keys))]
	name := filepath.Base(clips[key])
	title := strings.TrimSuffix(name, filepath.Ext(name))
	writeJSON(w, http.StatusOK, map[string]any{"clip": prerollClip{Key: key, Title: title}})
}

// streamPreroll answers GET /api/preroll/{key}: the clip, with ranges.
func (s *Server) streamPreroll(w http.ResponseWriter, r *http.Request) {
	path, ok := prerollClips(s.settings.Get().PrerollDir)[r.PathValue("key")]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such clip")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such clip")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such clip")
		return
	}
	if ct := extraContentType(strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}

// validPrerollDir checks a folder an administrator typed: absolute, existing,
// a folder. Empty turns pre-roll off.
func validPrerollDir(dir string) (string, string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", ""
	}
	if !filepath.IsAbs(dir) {
		return "", "preroll_dir must be a full path to a folder"
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", "preroll_dir is not a folder this server can read"
	}
	return filepath.Clean(dir), ""
}
