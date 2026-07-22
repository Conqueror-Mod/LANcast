package api

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// browseEntry is one navigable directory.
type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// browse lists directories so the client can offer a folder picker instead of
// making the user type a path.
//
// Directories only — never files, and never file contents. This does disclose
// filesystem layout to anyone who can reach the server, which matters because
// there is no authentication yet. It grants no capability that POST
// /api/libraries did not already have (that endpoint accepts and scans any
// path), but it does make enumeration convenient. See docs/api.md.
func (s *Server) browse(w http.ResponseWriter, r *http.Request) {
	requested := r.URL.Query().Get("path")

	// No path means "show me the roots": drive letters on Windows, / elsewhere.
	if strings.TrimSpace(requested) == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":    "",
			"parent":  nil,
			"entries": roots(),
		})
		return
	}

	abs, err := filepath.Abs(requested)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "path could not be resolved")
		return
	}

	info, err := os.Stat(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such directory")
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "bad_request", "path is not a directory")
		return
	}

	f, err := os.Open(abs)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "directory is not readable")
		return
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "directory is not readable")
		return
	}

	entries := make([]browseEntry, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue // dotfiles are noise in a media folder picker
		}
		child := filepath.Join(abs, name)
		st, err := os.Stat(child)
		if err != nil || !st.IsDir() {
			// Unreadable entries and plain files are both skipped; a picker
			// that lists things you cannot open is worse than one that omits
			// them.
			continue
		}
		if isHidden(child, st) {
			continue
		}
		entries = append(entries, browseEntry{Name: name, Path: child})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	var parent any
	if p := filepath.Dir(abs); p != abs {
		parent = p
	} else {
		// At a filesystem root, "up" goes back to the root list rather than
		// nowhere — otherwise a picker can strand you on the wrong drive.
		parent = ""
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    abs,
		"parent":  parent,
		"entries": entries,
	})
}

// roots lists the top-level starting points for browsing.
func roots() []browseEntry {
	if runtime.GOOS != "windows" {
		return []browseEntry{{Name: "/", Path: "/"}}
	}
	var out []browseEntry
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + `:\`
		if _, err := os.Stat(drive); err == nil {
			out = append(out, browseEntry{Name: string(c) + ":", Path: drive})
		}
	}
	return out
}
