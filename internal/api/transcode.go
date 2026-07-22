package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lancast/internal/probe"
	"lancast/internal/store"
	"lancast/internal/transcode"
)

// transcodeStream serves a progressive fragmented MP4.
//
// The pragmatic path: Chrome has no native HLS, and shipping hls.js is a
// 300KB client dependency this build does not carry yet. Progressive fMP4
// plays everywhere with no client library. The cost is that seeking is limited
// to what has already been produced — which is why the HLS endpoints exist
// alongside it for clients that can use them.
func (s *Server) transcodeStream(w http.ResponseWriter, r *http.Request) {
	it, decision, ok := s.transcodeTarget(w, r)
	if !ok {
		return
	}

	opts := transcode.Options{
		Input:      it.Path,
		Decision:   decision,
		StartAt:    queryFloat(r, "t"),
		AudioIndex: queryIntDefault(r, "audio", -1),
	}

	stream, err := s.trans.Progressive(r.Context(), it.ID, opts)
	if err != nil {
		s.writeTranscodeError(w, err)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "video/mp4")
	// A live transcode has no known length and cannot be range-served: bytes
	// do not exist until ffmpeg produces them. Saying so plainly stops
	// browsers issuing range requests that could never be satisfied.
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if _, err := copyToClient(w, stream); err != nil {
		// A client closing the tab mid-stream is normal, not an error worth
		// logging at anything above debug.
		s.log.Debug("transcode stream ended", "item", it.ID, "error", err)
	}
}

// hlsPlaylist serves the HLS playlist, starting a session if needed.
func (s *Server) hlsPlaylist(w http.ResponseWriter, r *http.Request) {
	it, decision, ok := s.transcodeTarget(w, r)
	if !ok {
		return
	}

	sess, err := s.trans.EnsureHLS(r.Context(), it.ID, transcode.Options{
		Input:      it.Path,
		Decision:   decision,
		StartAt:    queryFloat(r, "t"),
		AudioIndex: queryIntDefault(r, "audio", -1),
	})
	if err != nil {
		s.writeTranscodeError(w, err)
		return
	}

	path, err := s.trans.WaitForFile(r.Context(), sess, "index.m3u8", 30*time.Second)
	if err != nil {
		s.log.Warn("hls playlist unavailable", "item", it.ID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "could not start transcoding")
		return
	}

	body, err := os.ReadFile(path)
	if err != nil {
		s.writeInternal(w, err, "read playlist")
		return
	}

	// ffmpeg writes bare filenames; rewrite them to this session's endpoints.
	rewritten := rewritePlaylist(string(body), "/api/stream/"+itoa64(it.ID)+"/hls/"+sess.ID+"/")

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(rewritten))
}

// hlsSegment serves one segment or the init file from a session directory.
func (s *Server) hlsSegment(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session")
	name := r.PathValue("name")

	// The name comes from a URL and becomes a filesystem path, so it is
	// validated rather than trusted — the same rule as artwork hashes.
	if !validSegmentName(name) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid segment name")
		return
	}

	sess := s.trans.Session(sessionID)
	if sess == nil {
		// The session was reaped or never existed. A 404 lets the player
		// re-request the playlist and get a fresh one.
		writeError(w, http.StatusNotFound, "not_found", "transcode session has ended")
		return
	}
	sess.Touch()

	path, err := s.trans.WaitForFile(r.Context(), sess, name, 30*time.Second)
	if err != nil {
		s.log.Warn("hls segment unavailable", "session", sessionID, "name", name, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "segment not ready")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such segment")
		return
	}
	defer f.Close()

	if strings.HasSuffix(name, ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
	} else {
		w.Header().Set("Content-Type", "video/iso.segment")
	}
	w.Header().Set("Cache-Control", "no-store")

	info, err := f.Stat()
	if err != nil {
		s.writeInternal(w, err, "stat segment")
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// transcodeSessions lists running transcodes.
func (s *Server) transcodeSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"available": s.trans.Available(),
		"sessions":  s.trans.Sessions(),
	})
}

// transcodeTarget resolves the item and its playback decision, applying the
// same containment guard as direct streaming.
func (s *Server) transcodeTarget(w http.ResponseWriter, r *http.Request) (*store.Item, probe.Decision, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return nil, probe.Decision{}, false
	}

	it, err := s.st.GetItem(r.Context(), id, localUser)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return nil, probe.Decision{}, false
	}
	if it.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "item has no playable file")
		return nil, probe.Decision{}, false
	}

	lib, err := s.st.GetLibrary(r.Context(), it.LibraryID)
	if s.notFoundOr(w, err, "get library", "owning library is gone") {
		return nil, probe.Decision{}, false
	}

	// Transcoding hands a path to a subprocess, which is if anything a
	// stronger reason to verify containment than serving bytes directly.
	path, err := containedPath(lib.Path, it.Path)
	if err != nil {
		s.log.Error("transcode containment check failed", "item", id, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return nil, probe.Decision{}, false
	}
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
		return nil, probe.Decision{}, false
	}
	it.Path = path

	decision := probe.Decide(probe.ResultFromItem(it), probe.BrowserProfile())
	if decision.Method == probe.DirectPlay {
		// Nothing to do. Transcoding a file the client can already play is
		// pure waste, so say so rather than quietly burning CPU.
		writeError(w, http.StatusConflict, "conflict", "this file can be played directly")
		return nil, probe.Decision{}, false
	}
	return it, decision, true
}

func (s *Server) writeTranscodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, transcode.ErrNotInstalled):
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"ffmpeg is not installed, so this file cannot be converted for playback")
	case errors.Is(err, transcode.ErrTooManySessions):
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many transcodes are already running; try again shortly")
	default:
		s.writeInternal(w, err, "start transcode")
	}
}

// rewritePlaylist points segment references at the API rather than at bare
// filenames sitting in a scratch directory.
func rewritePlaylist(body, prefix string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "#EXT-X-MAP:URI="):
			lines[i] = `#EXT-X-MAP:URI="` + prefix + "init.mp4\""
		case strings.HasPrefix(trimmed, "#"):
			continue
		default:
			lines[i] = prefix + trimmed
		}
	}
	return strings.Join(lines, "\n")
}

// validSegmentName allows only the shapes ffmpeg produces.
func validSegmentName(name string) bool {
	if name == "init.mp4" {
		return true
	}
	if !strings.HasPrefix(name, "seg") || !strings.HasSuffix(name, ".m4s") {
		return false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(name, "seg"), ".m4s")
	if digits == "" || len(digits) > 8 {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	// Belt and braces: reject anything that is not a plain filename.
	return filepath.Base(name) == name
}

func queryFloat(r *http.Request, key string) float64 {
	f, err := strconv.ParseFloat(r.URL.Query().Get(key), 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

func queryIntDefault(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// copyToClient streams ffmpeg's output, flushing as it goes.
//
// Without explicit flushing, Go buffers and the browser sits on an empty
// <video> element while bytes pile up server-side — playback appears not to
// start at all on a transcode that is working fine.
func copyToClient(w http.ResponseWriter, src io.Reader) (int64, error) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 64<<10)
	var total int64

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := w.Write(buf[:n])
			total += int64(written)
			if flusher != nil {
				flusher.Flush()
			}
			if writeErr != nil {
				return total, writeErr
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return total, nil
			}
			return total, readErr
		}
	}
}
