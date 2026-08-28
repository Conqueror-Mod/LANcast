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
	t, ok := s.transcodeTarget(w, r)
	if !ok {
		return
	}
	it := t.item

	opts := transcode.Options{
		Input:      it.Path,
		Decision:   t.decision,
		StartAt:    queryFloat(r, "t"),
		AudioIndex: t.audioIndex,
	}

	// The caller's account, so a seek replaces this viewer's own stream for
	// this film rather than starting a second one beside it (and rather than
	// disturbing anyone else watching the same film).
	stream, err := s.trans.Progressive(r.Context(), it.ID, s.userID(r), opts)
	if err != nil {
		s.writeTranscodeError(w, err)
		return
	}
	defer stream.Close()

	// Audio-only output is still fragmented MP4, but labelling it video/mp4
	// makes an <audio> element reject a stream it can play perfectly well.
	if t.decision.AudioOnly {
		w.Header().Set("Content-Type", "audio/mp4")
	} else {
		w.Header().Set("Content-Type", "video/mp4")
	}
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
	t, ok := s.transcodeTarget(w, r)
	if !ok {
		return
	}
	it := t.item

	sess, err := s.trans.EnsureHLS(r.Context(), it.ID, transcode.Options{
		Input:      it.Path,
		Decision:   t.decision,
		StartAt:    queryFloat(r, "t"),
		AudioIndex: t.audioIndex,
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
		"encoder":   s.trans.Encoder(),
		"sessions":  s.trans.Sessions(),
	})
}

// playTarget is a resolved request: which file, how to deliver it, and which
// audio track the decision was made about.
//
// The audio index travels with the decision rather than being read from the
// query a second time at the point ffmpeg is invoked. Deciding against one
// stream and mapping another is how `-c:a copy` ends up on a track the client
// cannot decode.
type playTarget struct {
	item       *store.Item
	decision   probe.Decision
	audioIndex int
}

// transcodeTarget resolves the item and its playback decision, applying the
// same containment guard as direct streaming.
func (s *Server) transcodeTarget(w http.ResponseWriter, r *http.Request) (playTarget, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return playTarget{}, false
	}

	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return playTarget{}, false
	}
	if it.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "item has no playable file")
		return playTarget{}, false
	}

	// Transcoding hands a path to a subprocess, which is if anything a
	// stronger reason to verify containment than serving bytes directly.
	// Against the item's own location (ADR 0034).
	path, err := s.itemFilePath(r, it)
	if err != nil {
		s.log.Error("transcode containment check failed", "item", id, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return playTarget{}, false
	}
	if _, err := os.Stat(path); err != nil {
		/*
		 * Logged for the same reason a refused transcode is: this is a decision
		 * the server made, and it is invisible to the player.
		 *
		 * The element sees a failed request with no status and reports a bare
		 * error, so the screen said the conversion could not start and blamed
		 * the load — which sends somebody to Activity to investigate a file
		 * that is not on the disk. The row is marked missing and the client no
		 * longer offers Play for one, so reaching here means something asked
		 * anyway: a stale tab, a direct link, or a scan that has not run since
		 * the file went.
		 */
		s.log.Warn("refused playback: the file is not on disk",
			"item", id, "path", path)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
		return playTarget{}, false
	}
	it.Path = path

	// The full track list, not just the summary columns: the decision has to
	// be about the same stream ffmpeg will map.
	streams, err := s.st.Streams(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "load streams")
		return playTarget{}, false
	}
	res := probe.ResultWithStreams(it, streams)
	audioIndex := queryIntDefault(r, "audio", -1)
	if audioIndex >= 0 && res != nil && res.AudioByIndex(audioIndex) == nil {
		// Also logged: same argument as the refusal below. A stale index after
		// a rescan reads at the player as a file that simply stopped working.
		s.log.Debug("transcode refused, no such audio track", "item", id, "audio", audioIndex)
		writeError(w, http.StatusBadRequest, "bad_request", "no audio track at that index")
		return playTarget{}, false
	}

	decision := probe.DecideTrack(res, clientProfile(r), audioIndex)
	if decision.Method == probe.DirectPlay {
		// Nothing to do. Transcoding a file the client can already play is
		// pure waste, so say so rather than quietly burning CPU.
		//
		// Logged because this refusal is indistinguishable from a hang at the
		// player: the element gets an error, playback stops, and nothing is
		// recorded anywhere. A client asking to transcode something it was told
		// to direct-play means the two disagree about the request — a seek that
		// dropped its ?audio= did exactly that, and the silence here is what
		// made it take a code read to find rather than a log read.
		s.log.Debug("transcode refused, file direct-plays",
			"item", id, "audio", audioIndex, "profile", clientProfile(r).Name)
		writeError(w, http.StatusConflict, "conflict", "this file can be played directly")
		return playTarget{}, false
	}
	return playTarget{item: it, decision: decision, audioIndex: audioIndex}, true
}

/*
 * writeTranscodeError answers a refusal, and says so in the log.
 *
 * The logging is the point. These two cases used to write a status to the
 * client and record nothing, which made them the only playback outcomes the
 * server had no opinion about — and they are precisely the ones that leave a
 * player showing a spinner for ever, because a `<video>` element handed a 429
 * has nothing to display.
 *
 * The cost of that was not theoretical: a film sat converting with no session
 * in the log, and the question "was this refused, or did the request never
 * arrive" could not be answered at all. A refusal is a decision the server
 * made. It should be as visible as the sessions it declined to start.
 *
 * The live session count travels with it because it is the difference between
 * "the ceiling is doing its job" and "sessions are leaking" — the same number
 * read two ways, and unreadable without it.
 */
func (s *Server) writeTranscodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, transcode.ErrNotInstalled):
		s.log.Warn("refused a transcode: ffmpeg is not installed")
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"ffmpeg is not installed, so this file cannot be converted for playback")
	case errors.Is(err, transcode.ErrTooManySessions):
		s.log.Warn("refused a transcode: at the session ceiling",
			"running", len(s.trans.Sessions()), "max", s.trans.MaxSessions)
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
