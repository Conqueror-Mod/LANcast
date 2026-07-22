package api

import (
	"net/http"
	"os"

	"lancast/internal/artwork"
)

// serveArtwork returns a cached image at the requested size.
//
// Content addressing makes indefinite caching safe: the bytes behind a hash
// cannot change, so the response is immutable by construction.
func (s *Server) serveArtwork(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	size := artwork.Size(r.URL.Query().Get("size"))
	if size == "" {
		size = artwork.SizePoster
	}

	// The hash is validated inside the cache before it becomes a filesystem
	// path; an invalid one is indistinguishable from a miss.
	f, err := s.art.Open(hash, size)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "not_found", "no such artwork")
			return
		}
		s.writeInternal(w, err, "open artwork")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		s.writeInternal(w, err, "stat artwork")
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", `"`+hash+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	http.ServeContent(w, r, hash+".jpg", info.ModTime(), f)
}
