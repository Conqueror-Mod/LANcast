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
	"lancast/internal/coverart"
	"lancast/internal/enrich"
	"lancast/internal/meta"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/subtitle"
	"lancast/internal/transcode"
	"lancast/internal/update"
)

// Version is reported by GET /api/health. It is a var, not a const, so a release
// build can stamp the tag into it with `-ldflags -X lancast/internal/api.Version=vX.Y.Z`
// (ADR 0016). An unstamped build reports "dev", which is the honest label for a
// binary built straight from source.
var Version = "dev"

// APIVersion is the HTTP contract revision. It changes only when a new
// /api/vN prefix ships, independently of the application Version (ADR 0018).
// /api is permanently version 1.
const APIVersion = 1

// Deps are the Server's collaborators.
type Deps struct {
	Store    *store.Store
	Scanner  *scan.Scanner
	Registry *meta.Registry
	Artwork  *artwork.Cache
	Worker   *enrich.Worker
	Probes   *probe.Worker
	Covers   *coverart.Worker
	Trans    *transcode.Manager
	Updates  *update.Checker
	Subs     *subtitle.Extractor
	Settings *config.SettingsStore
	// DataDir is the server data directory: where downloaded subtitles are
	// written — never beside the media, which is the same rule NFO writing
	// follows — and where lancastd.log is read from for GET /api/logs.
	DataDir string
	Log     *slog.Logger
	Web     http.Handler

	// Rebuild reconfigures providers after a settings change, so a newly
	// entered API key takes effect without a restart.
	Rebuild func(config.Settings)
	// ReloadPlugins re-reads installed plugins and rebuilds the registry, so an
	// install, grant, enable/disable, or remove takes effect without a restart.
	ReloadPlugins func() error
	// Enrich triggers a background enrichment pass.
	Enrich func()
	// Probe triggers a background probe pass, so a re-probe an operator asked
	// for starts now rather than at the next scan.
	Probe func()
	// Cover triggers a background album-art pass, for the same reason.
	Cover func()
	// LANBound reports whether the server is actually listening beyond
	// loopback — the resolved address, not whether a password is set.
	LANBound bool
	// RestartWidens reports whether restarting would bind wider than the
	// server is bound right now. False when the operator configured a loopback
	// address deliberately: there, a restart changes nothing and telling them
	// otherwise sends them to do something that cannot work.
	RestartWidens bool
}

// Server holds the API dependencies.
type Server struct {
	st            *store.Store
	scanner       *scan.Scanner
	reg           *meta.Registry
	art           *artwork.Cache
	worker        *enrich.Worker
	probes        *probe.Worker
	covers        *coverart.Worker
	trans         *transcode.Manager
	updates       *update.Checker
	subs          *subtitle.Extractor
	settings      *config.SettingsStore
	dataDir       string
	log           *slog.Logger
	web           http.Handler
	rebuild       func(config.Settings)
	reloadPlugins func() error
	enrich        func()
	probe         func()
	coversSoon    func()
	lanBound      bool
	restartWidens bool
	throttle      *auth.Throttle
}

func New(d Deps) *Server {
	web := d.Web
	if web == nil {
		web = http.NotFoundHandler()
	}
	return &Server{
		st: d.Store, scanner: d.Scanner, reg: d.Registry, art: d.Artwork,
		worker: d.Worker, probes: d.Probes, covers: d.Covers, trans: d.Trans, subs: d.Subs,
		updates:  d.Updates,
		settings: d.Settings, dataDir: d.DataDir, log: d.Log, web: web,
		rebuild: d.Rebuild, reloadPlugins: d.ReloadPlugins, enrich: d.Enrich,
		probe: d.Probe, coversSoon: d.Cover,
		lanBound: d.LANBound, restartWidens: d.RestartWidens,
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

	// Filesystem enumeration is reconnaissance for library creation, so it is an
	// admin-only power like library creation itself.
	mux.HandleFunc("GET /api/browse", s.adminOnly(s.browse))

	mux.HandleFunc("GET /api/libraries", s.listLibraries)
	mux.HandleFunc("POST /api/libraries", s.adminOnly(s.createLibrary))
	mux.HandleFunc("DELETE /api/libraries/{id}", s.adminOnly(s.deleteLibrary))
	mux.HandleFunc("POST /api/libraries/{id}/scan", s.adminOnly(s.startScan))
	mux.HandleFunc("GET /api/libraries/{id}/scan", s.scanStatus)
	mux.HandleFunc("GET /api/libraries/{id}/facets", s.libraryFacets)
	mux.HandleFunc("POST /api/libraries/{id}/refresh", s.adminOnly(s.refreshLibrary))

	mux.HandleFunc("GET /api/items", s.listItems)
	mux.HandleFunc("GET /api/continue", s.continueWatching)
	mux.HandleFunc("GET /api/items/{id}", s.getItem)
	// Editing shared metadata or identity re-litigates the library for everyone,
	// so it is an admin action. Watching and progress are not.
	mux.HandleFunc("PATCH /api/items/{id}", s.adminOnly(s.patchItem))
	mux.HandleFunc("DELETE /api/items/{id}", s.adminOnly(s.deleteItem))
	mux.HandleFunc("PUT /api/items/{id}/progress", s.putProgress)
	mux.HandleFunc("DELETE /api/items/{id}/locks/{field}", s.adminOnly(s.deleteLock))
	mux.HandleFunc("GET /api/items/{id}/candidates", s.candidates)
	mux.HandleFunc("POST /api/items/{id}/match", s.adminOnly(s.applyMatch))
	mux.HandleFunc("POST /api/items/{id}/refresh", s.adminOnly(s.refreshItem))

	mux.HandleFunc("GET /api/items/{id}/playback", s.playback)
	mux.HandleFunc("GET /api/items/{id}/trailer", s.trailer)
	mux.HandleFunc("GET /api/items/{id}/subtitles", s.listSubtitles)
	mux.HandleFunc("GET /api/items/{id}/subtitles/search", s.searchSubtitles)
	mux.HandleFunc("POST /api/items/{id}/subtitles/download", s.downloadSubtitle)
	mux.HandleFunc("GET /api/items/{id}/subtitles/{key}", s.serveSubtitle)
	mux.HandleFunc("DELETE /api/items/{id}/subtitles/{key}", s.deleteSubtitle)

	mux.HandleFunc("GET /api/review", s.reviewQueue)
	mux.HandleFunc("GET /api/enrich", s.enrichStatus)
	mux.HandleFunc("GET /api/probe", s.probeStatus)
	mux.HandleFunc("GET /api/activity", s.activity)
	mux.HandleFunc("GET /api/logs", s.adminOnly(s.serverLog))
	mux.HandleFunc("GET /api/audit", s.adminOnly(s.listAudit))
	mux.HandleFunc("GET /api/update", s.adminOnly(s.updateStatus))
	mux.HandleFunc("POST /api/update/check", s.adminOnly(s.checkForUpdate))
	mux.HandleFunc("POST /api/update/download", s.adminOnly(s.downloadUpdate))
	mux.HandleFunc("POST /api/probe/refresh", s.adminOnly(s.reprobe))
	mux.HandleFunc("GET /api/coverart", s.coverArtStatus)
	mux.HandleFunc("POST /api/coverart/refresh", s.adminOnly(s.recoverArt))
	mux.HandleFunc("GET /api/artwork/{hash}", s.serveArtwork)

	mux.HandleFunc("GET /api/settings", s.adminOnly(s.getSettings))
	mux.HandleFunc("PUT /api/settings", s.adminOnly(s.putSettings))

	// Plugins (ADR 0021). Install is two steps — upload/inspect, then grant — so
	// the capability approval is an explicit act. All admin-only.
	mux.HandleFunc("GET /api/plugins", s.adminOnly(s.listPlugins))
	mux.HandleFunc("POST /api/plugins", s.adminOnly(s.uploadPlugin))
	mux.HandleFunc("POST /api/plugins/{name}/grant", s.adminOnly(s.grantPlugin))
	mux.HandleFunc("POST /api/plugins/{name}/enable", s.adminOnly(s.enablePlugin))
	mux.HandleFunc("POST /api/plugins/{name}/disable", s.adminOnly(s.disablePlugin))
	mux.HandleFunc("DELETE /api/plugins/{name}", s.adminOnly(s.removePlugin))

	mux.HandleFunc("GET /api/users", s.adminOnly(s.listUsers))
	mux.HandleFunc("POST /api/users", s.adminOnly(s.createUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.adminOnly(s.deleteUser))
	mux.HandleFunc("POST /api/users/{id}/password", s.adminOnly(s.resetUserPassword))

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
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "api_version": APIVersion})
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
