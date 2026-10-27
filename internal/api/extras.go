package api

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lancast/internal/media"
)

/*
 * Extras on a film's (or a show's) page (ADR 0098).
 *
 * Found on demand in the film's folder, by the rules that keep them out of the
 * library (ADR 0038), and never imported: an extra is not a row, so it cannot
 * appear in a grid, a count, a shelf or a search. It is played straight from
 * the file, by a key that is checked against a fresh listing on every request,
 * so a key names one of this film's extras or nothing.
 */

type extraView struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Bytes int64  `json:"bytes"`
	// Container is the file's extension, lower case, so a client can say what
	// it could not play rather than only that it could not.
	Container string `json:"container"`
}

type foundExtra struct {
	extraView
	path string
}

// extraKey is an extra's path relative to the folder it was found from, made
// opaque for the URL. It is never decoded into a path: it is looked up.
func extraKey(rel string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(filepath.ToSlash(rel)))
}

/*
 * findExtras lists one item's extras.
 *
 * For a film in a folder of its own: the marked files beside it, and every
 * video in the extras folders directly inside it (Trailers/, Featurettes/,
 * Behind The Scenes/ and the rest). For a film sharing a folder with others,
 * only the marked files that name it ("Alien-trailer" for "Alien"), because an
 * extras folder there cannot say whose it is. For a show, its folder is the
 * show folder and the same rules apply. A library root is never a film's own
 * folder: "Trailers" directly under a root is a category (ADR 0038).
 *
 * Samples are left out, as they are from the library.
 */
func findExtras(root, itemPath string, isFolder bool) []foundExtra {
	dir := itemPath
	if !isFolder {
		dir = filepath.Dir(itemPath)
	}
	if filepath.Clean(dir) == filepath.Clean(root) {
		if isFolder {
			return nil
		}
		return suffixExtrasFor(dir, itemPath)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// Does this film have the folder to itself? Count the works in it.
	works := 0
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if !e.IsDir() && media.IsVideo(p) && !media.IsExtra(root, p) && !media.IsSample(p) {
			works++
		}
	}
	ownFolder := isFolder || works <= 1
	if !ownFolder {
		return suffixExtrasFor(dir, itemPath)
	}

	var out []foundExtra
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir() && media.IsExtraDir(e.Name()):
			sub, err := os.ReadDir(p)
			if err != nil {
				continue
			}
			for _, f := range sub {
				fp := filepath.Join(p, f.Name())
				if f.IsDir() || !media.IsVideo(fp) || media.IsSample(fp) {
					continue
				}
				out = append(out, newFoundExtra(dir, e.Name(), fp))
			}
		case !e.IsDir() && !isFolder && media.IsVideo(p) && media.IsExtra(root, p) && !media.IsSample(p):
			out = append(out, newFoundExtra(dir, "", p))
		}
	}
	sortExtras(out)
	return out
}

func suffixExtrasFor(dir, filmPath string) []foundExtra {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []foundExtra
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() || !media.IsVideo(p) || media.IsSample(p) {
			continue
		}
		if media.ExtraStemMatches(filmPath, p) {
			out = append(out, newFoundExtra(dir, "", p))
		}
	}
	sortExtras(out)
	return out
}

func newFoundExtra(base, folder, path string) foundExtra {
	kind, title := media.ClassifyExtra(folder, path)
	rel, _ := filepath.Rel(base, path)
	var size int64
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	return foundExtra{
		extraView: extraView{
			Key: extraKey(rel), Kind: kind, Title: title, Bytes: size,
			Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		},
		path: path,
	}
}

func sortExtras(xs []foundExtra) {
	sort.SliceStable(xs, func(i, j int) bool {
		a, b := media.ExtraKindOrder[xs[i].Kind], media.ExtraKindOrder[xs[j].Kind]
		if a != b {
			return a < b
		}
		return strings.ToLower(xs[i].Title) < strings.ToLower(xs[j].Title)
	})
}

// extrasFor resolves an item the caller may see to its extras, or a reason it
// has none. Only films and shows have extras.
func (s *Server) extrasFor(r *http.Request) ([]foundExtra, string, int, bool) {
	id, ok := pathID(r)
	if !ok {
		return nil, "", http.StatusBadRequest, false
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if err != nil {
		return nil, "", http.StatusNotFound, false
	}
	if (it.Kind != "movie" && it.Kind != "show") || it.Path == "" {
		return []foundExtra{}, "", http.StatusOK, true
	}
	root, err := s.st.RootForItem(r.Context(), it.ID)
	if err != nil {
		return []foundExtra{}, "", http.StatusOK, true
	}
	path, err := containedPath(root.Path, it.Path)
	if err != nil {
		s.log.Error("extras containment check failed", "item", it.ID, "path", it.Path, "error", err)
		return nil, "", http.StatusNotFound, false
	}
	found := findExtras(root.Path, path, it.Kind == "show")
	// Every extra is held to the library root as well, links followed, so a
	// file in an extras folder that links elsewhere is not served (ADR 0094).
	kept := found[:0]
	for _, x := range found {
		if _, err := containedPath(root.Path, x.path); err == nil {
			kept = append(kept, x)
		}
	}
	return kept, root.Path, http.StatusOK, true
}

// listExtras answers GET /api/items/{id}/extras.
func (s *Server) listExtras(w http.ResponseWriter, r *http.Request) {
	found, _, status, ok := s.extrasFor(r)
	if !ok {
		writeError(w, status, "not_found", "no such item")
		return
	}
	out := make([]extraView, 0, len(found))
	for _, x := range found {
		out = append(out, x.extraView)
	}
	writeJSON(w, http.StatusOK, map[string]any{"extras": out})
}

// streamExtra answers GET /api/items/{id}/extras/{key}: the file itself, with
// range requests, for an extra the listing would return right now.
func (s *Server) streamExtra(w http.ResponseWriter, r *http.Request) {
	found, _, status, ok := s.extrasFor(r)
	if !ok {
		writeError(w, status, "not_found", "no such item")
		return
	}
	key := r.PathValue("key")
	for _, x := range found {
		if x.Key != key {
			continue
		}
		f, err := os.Open(x.path)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "no such extra")
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "no such extra")
			return
		}
		if ct := extraContentType(x.Container); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		http.ServeContent(w, r, filepath.Base(x.path), fi.ModTime(), f)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "no such extra")
}

func extraContentType(container string) string {
	switch container {
	case "mp4", "m4v":
		return "video/mp4"
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "avi":
		return "video/x-msvideo"
	}
	return ""
}
