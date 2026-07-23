// Package api serves the LANcast HTTP contract documented in docs/api.md.
//
// That document and these handlers must agree exactly — it is what third-party
// clients build against.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"lancast/internal/artwork"
	"lancast/internal/auth"
	"lancast/internal/config"
	"lancast/internal/enrich"
	"lancast/internal/meta"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/subtitle"
	"lancast/internal/transcode"
)

// Version is reported by GET /api/health.
const Version = "0.2.0"

// localUser is the single-user identity for M1/M2. The schema is already keyed
// by user (ADR 0006), so multi-user arrives without a migration.
const localUser = "local"

// Deps are the Server's collaborators.
type Deps struct {
	Store    *store.Store
	Scanner  *scan.Scanner
	Registry *meta.Registry
	Artwork  *artwork.Cache
	Worker   *enrich.Worker
	Probes   *probe.Worker
	Trans    *transcode.Manager
	Subs     *subtitle.Extractor
	Settings *config.SettingsStore
	// DataDir is where downloaded subtitles are written — never beside the
	// media, which is the same rule NFO writing follows.
	DataDir string
	Log     *slog.Logger
	Web     http.Handler

	// Rebuild reconfigures providers after a settings change, so a newly
	// entered API key takes effect without a restart.
	Rebuild func(config.Settings)
	// Enrich triggers a background enrichment pass.
	Enrich func()
	// LANBound reports whether the server is listening beyond loopback. An
	// unsecured server is loopback-only, so the client can explain why a
	// restart is needed after setting a password.
	LANBound bool
}

// Server holds the API dependencies.
type Server struct {
	st       *store.Store
	scanner  *scan.Scanner
	reg      *meta.Registry
	art      *artwork.Cache
	worker   *enrich.Worker
	probes   *probe.Worker
	trans    *transcode.Manager
	subs     *subtitle.Extractor
	settings *config.SettingsStore
	dataDir  string
	log      *slog.Logger
	web      http.Handler
	rebuild  func(config.Settings)
	enrich   func()
	lanBound bool
	throttle *auth.Throttle
}

func New(d Deps) *Server {
	web := d.Web
	if web == nil {
		web = http.NotFoundHandler()
	}
	return &Server{
		st: d.Store, scanner: d.Scanner, reg: d.Registry, art: d.Artwork,
		worker: d.Worker, probes: d.Probes, trans: d.Trans, subs: d.Subs,
		settings: d.Settings, dataDir: d.DataDir, log: d.Log, web: web,
		rebuild: d.Rebuild, enrich: d.Enrich, lanBound: d.LANBound,
		throttle: auth.NewThrottle(),
	}
}

// enrichSoon kicks the background worker, if one is wired up.
func (s *Server) enrichSoon() {
	if s.enrich != nil {
		s.enrich()
	}
}

// Handler builds the router. Go 1.22 method-and-pattern routing covers this
// without a third-party dependency.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.health)

	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/setup", s.authSetup)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)
	mux.HandleFunc("POST /api/auth/password", s.authChangePassword)

	mux.HandleFunc("GET /api/browse", s.browse)

	mux.HandleFunc("GET /api/libraries", s.listLibraries)
	mux.HandleFunc("POST /api/libraries", s.createLibrary)
	mux.HandleFunc("POST /api/libraries/{id}/scan", s.startScan)
	mux.HandleFunc("GET /api/libraries/{id}/scan", s.scanStatus)
	mux.HandleFunc("POST /api/libraries/{id}/refresh", s.refreshLibrary)

	mux.HandleFunc("GET /api/items", s.listItems)
	mux.HandleFunc("GET /api/items/{id}", s.getItem)
	mux.HandleFunc("PATCH /api/items/{id}", s.patchItem)
	mux.HandleFunc("PUT /api/items/{id}/progress", s.putProgress)
	mux.HandleFunc("DELETE /api/items/{id}/locks/{field}", s.deleteLock)
	mux.HandleFunc("GET /api/items/{id}/candidates", s.candidates)
	mux.HandleFunc("POST /api/items/{id}/match", s.applyMatch)
	mux.HandleFunc("POST /api/items/{id}/refresh", s.refreshItem)

	mux.HandleFunc("GET /api/items/{id}/playback", s.playback)
	mux.HandleFunc("GET /api/items/{id}/subtitles", s.listSubtitles)
	mux.HandleFunc("GET /api/items/{id}/subtitles/search", s.searchSubtitles)
	mux.HandleFunc("POST /api/items/{id}/subtitles/download", s.downloadSubtitle)
	mux.HandleFunc("GET /api/items/{id}/subtitles/{key}", s.serveSubtitle)

	mux.HandleFunc("GET /api/review", s.reviewQueue)
	mux.HandleFunc("GET /api/enrich", s.enrichStatus)
	mux.HandleFunc("GET /api/probe", s.probeStatus)
	mux.HandleFunc("GET /api/artwork/{hash}", s.serveArtwork)

	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)

	mux.HandleFunc("GET /api/stream/{id}", s.stream)
	mux.HandleFunc("GET /api/stream/{id}/transcode", s.transcodeStream)
	mux.HandleFunc("GET /api/stream/{id}/hls/index.m3u8", s.hlsPlaylist)
	mux.HandleFunc("GET /api/stream/{id}/hls/{session}/{name}", s.hlsSegment)
	mux.HandleFunc("GET /api/transcode", s.transcodeSessions)

	mux.Handle("/", s.web)
	return logRequests(s.log, s.requireAuth(mux))
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

// ------------------------------------------------------------------ helpers

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeError emits the single error shape documented in docs/api.md. Raw SQL
// errors never reach a client.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func (s *Server) writeInternal(w http.ResponseWriter, err error, op string) {
	s.log.Error(op, "error", err)
	writeError(w, http.StatusInternalServerError, "internal", "unexpected server error")
}

// notFoundOr maps a store error to the right response, returning true if it
// handled one.
func (s *Server) notFoundOr(w http.ResponseWriter, err error, op, msg string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound), errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "not_found", msg)
	default:
		s.writeInternal(w, err, op)
	}
	return true
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func queryInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	return n
}
