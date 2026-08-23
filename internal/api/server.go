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
	"sync"
	"time"

	"lancast/internal/artwork"
	"lancast/internal/auth"
	"lancast/internal/config"
	"lancast/internal/coverart"
	"lancast/internal/crashlog"
	"lancast/internal/enrich"
	"lancast/internal/identity"
	"lancast/internal/meta"
	"lancast/internal/photo"
	"lancast/internal/presence"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/subtitle"
	"lancast/internal/together"
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
	Photos   *photo.Worker
	// ServiceManaged reports whether this process is running under a service
	// manager. It decides what "finish the update" can do: a service can be
	// restarted for the user, a foreground server can only be told to close and
	// reopen — and guessing wrong means killing a process nothing will restart.
	ServiceManaged bool

	// Relaunch finishes a staged update when this server is *not* a service:
	// it spawns a detached helper that waits for this process to exit and then
	// starts it again with the arguments it had, and then triggers a graceful
	// shutdown. Nil where that cannot work (an unsupported host), in which case
	// the API says so rather than pretending.
	//
	// It exists because "close LANcast and open it again" was the whole
	// instruction a non-service install got, delivered once, with nothing
	// afterwards to say whether the swap had happened — so the only way to find
	// out was to start the server and read the version. The application knows
	// exactly when the update completes; it should say so.
	Relaunch func() error

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

	// Identity is this server's own keypair (ADR 0044). Held so GET
	// /api/identity can report the fingerprint, and so the peer work in Phase 2
	// has one place to get it from rather than re-reading the key file.
	Identity identity.Identity

	// ListenAddr is the address actually bound, so an invite can name a port
	// somebody could really reach. The port is the part that matters — the host
	// half is usually a wildcard, and the addresses in an invite come from
	// enumerating interfaces rather than from this.
	ListenAddr string

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
	ident          identity.Identity
	listenAddr     string
	st             *store.Store
	scanner        *scan.Scanner
	reg            *meta.Registry
	art            *artwork.Cache
	worker         *enrich.Worker
	probes         *probe.Worker
	covers         *coverart.Worker
	photos         *photo.Worker
	serviceManaged bool
	relaunch       func() error
	trans          *transcode.Manager
	updates        *update.Checker
	subs           *subtitle.Extractor
	settings       *config.SettingsStore
	// tools is the one media-tools install that may be running (ADR 0043).
	tools         toolsJob
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
	// presence is who is watching what, right now. In memory and never
	// persisted, which is ADR 0045 §4 and not an optimisation.
	presence *presence.Tracker
	// rosterAt is when each peer's roster was last fetched. In memory because
	// it describes this process, not the pairing.
	rosterMu sync.Mutex
	rosterAt map[string]time.Time
	// crashes records recovered panics as reports beside the database. Created
	// here rather than injected: it needs only the data directory, and a
	// dependency the caller may forget to wire is a crash reporter that is
	// absent in exactly the builds nobody tested.
	crashes *crashlog.Recorder
	// together holds live watch-together rooms. In memory and created here for
	// the same reason crashes is: it needs nothing from the caller, and a
	// dependency the caller may forget to wire is a feature that is absent in
	// exactly the builds nobody tested.
	together *together.Manager
	/*
	 * prober inspects a single source on demand — a live channel — as opposed
	 * to `probes`, which is the worker that walks the library.
	 *
	 * Separate because the two have different lifetimes and different failure
	 * meanings: the worker's job is to eventually probe everything and it may
	 * retry for hours, while this one has six seconds and a viewer waiting.
	 */
	prober *probe.Prober
}

func New(d Deps) *Server {
	web := d.Web
	if web == nil {
		web = http.NotFoundHandler()
	}
	return &Server{
		st: d.Store, scanner: d.Scanner, reg: d.Registry, art: d.Artwork,
		worker: d.Worker, probes: d.Probes, covers: d.Covers, photos: d.Photos, serviceManaged: d.ServiceManaged, relaunch: d.Relaunch, trans: d.Trans, subs: d.Subs,
		updates:  d.Updates,
		settings: d.Settings, dataDir: d.DataDir, log: d.Log, web: web,
		ident:      d.Identity,
		listenAddr: d.ListenAddr,
		presence:   presence.New(),
		rosterAt:   map[string]time.Time{},
		rebuild:    d.Rebuild, reloadPlugins: d.ReloadPlugins, enrich: d.Enrich,
		probe: d.Probe, coversSoon: d.Cover,
		lanBound: d.LANBound, restartWidens: d.RestartWidens,
		throttle: auth.NewThrottle(),
		crashes:  crashlog.New(d.DataDir, Version),
		together: together.New(),
		prober:   probe.New(),
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
	mux.HandleFunc("PATCH /api/libraries/{id}", s.adminOnly(s.patchLibrary))
	mux.HandleFunc("DELETE /api/libraries/{id}", s.adminOnly(s.deleteLibrary))
	// A library's locations (ADR 0034). Adding one is filesystem access at a
	// caller-chosen path, so it is gated exactly as library creation is;
	// listing is not, because the same paths are already in the library listing.
	mux.HandleFunc("GET /api/libraries/{id}/roots", s.listRoots)
	mux.HandleFunc("POST /api/libraries/{id}/roots", s.adminOnly(s.addRoot))
	mux.HandleFunc("PATCH /api/libraries/{id}/roots/{rootID}", s.adminOnly(s.patchRoot))
	mux.HandleFunc("DELETE /api/libraries/{id}/roots/{rootID}", s.adminOnly(s.removeRoot))

	// Before the {id} form only for readability; the patterns do not overlap.
	mux.HandleFunc("POST /api/libraries/scan", s.adminOnly(s.scanAll))
	mux.HandleFunc("POST /api/libraries/{id}/scan", s.adminOnly(s.startScan))
	mux.HandleFunc("GET /api/libraries/{id}/scan", s.scanStatus)
	mux.HandleFunc("GET /api/libraries/{id}/facets", s.libraryFacets)
	mux.HandleFunc("GET /api/libraries/{id}/cast", s.libraryCast)

	// Media tools. Admin-only: this makes the server download a binary and then
	// execute it (ADR 0043). The URL is pinned in mediatools, never a parameter.
	mux.HandleFunc("GET /api/media-tools", s.adminOnly(s.mediaToolsStatus))
	mux.HandleFunc("POST /api/media-tools/install", s.adminOnly(s.installMediaTools))
	mux.HandleFunc("POST /api/media-tools/install/cancel", s.adminOnly(s.cancelMediaToolsInstall))
	mux.HandleFunc("GET /api/libraries/{id}/trending", s.libraryTrending)
	mux.HandleFunc("POST /api/libraries/{id}/refresh", s.adminOnly(s.refreshLibrary))
	mux.HandleFunc("POST /api/libraries/{id}/reparse", s.adminOnly(s.reparseLibrary))

	mux.HandleFunc("GET /api/items", s.listItems)
	mux.HandleFunc("GET /api/continue", s.continueWatching)
	mux.HandleFunc("GET /api/profile", s.profile)
	mux.HandleFunc("PATCH /api/profile", s.patchProfile)
	mux.HandleFunc("GET /api/profile/ratings", s.listMyRatings)
	mux.HandleFunc("PUT /api/profile/sharing", s.putSharing)

	// Who this server is (ADR 0044). Reports an identity; grants nothing.
	mux.HandleFunc("GET /api/identity", s.identity)

	/*
	 * Peers (ADR 0044). Admin, because pairing opens a network relationship for
	 * the whole server — the same class of operational power as adding a
	 * library, and gated on the server rather than hidden in the client.
	 *
	 * Granting a named person something is not here and is not admin: that is
	 * one account's own decision, it lives on the People page, and it reads a
	 * different table. Settings pairs servers; People grants and joins.
	 */
	mux.HandleFunc("GET /api/peers", s.adminOnly(s.listPeers))
	mux.HandleFunc("POST /api/peers", s.adminOnly(s.addPeer))
	mux.HandleFunc("GET /api/peers/invite", s.adminOnly(s.ourInvite))
	mux.HandleFunc("DELETE /api/peers/{fingerprint}", s.adminOnly(s.removePeer))

	// Personal, not administrative: whether this account appears in the roster
	// handed to peers. Beside the other thing an account decides about itself.
	mux.HandleFunc("PUT /api/profile/peer-visibility", s.putPeerVisibility)

	mux.HandleFunc("GET /api/people", s.listPeople)
	// Peers and presence are read by any member, because granting somebody
	// presence is one account's decision about its own viewing and never
	// touches a peer route (ADR 0045 §6). Pairing stays admin-gated above.
	mux.HandleFunc("GET /api/people/peers", s.peerPresence)
	mux.HandleFunc("PUT /api/people/peers/{fingerprint}/{person}/presence", s.putPresenceGrant)
	mux.HandleFunc("DELETE /api/presence", s.deletePresence)
	// Answered over a pinned mutual-TLS connection rather than a session: the
	// caller is a server, not a browser. See federationPresence.
	mux.HandleFunc("GET /api/federation/presence", s.federationPresence)
	mux.HandleFunc("GET /api/federation/roster", s.federationRoster)
	mux.HandleFunc("GET /api/people/{id}/activity", s.personActivity)

	mux.HandleFunc("GET /api/channels", s.listChannels)
	mux.HandleFunc("GET /api/channels/{id}/stream", s.channelStream)
	mux.HandleFunc("GET /api/channels/{id}/live", s.channelLive)
	// The guide is readable by the household, like the channels it describes.
	mux.HandleFunc("GET /api/guide", s.listGuide)
	mux.HandleFunc("GET /api/channels/{id}/guide", s.channelGuide)
	mux.HandleFunc("GET /api/channel-sources", s.adminOnly(s.listChannelSources))
	mux.HandleFunc("POST /api/channel-sources", s.adminOnly(s.createChannelSource))
	mux.HandleFunc("POST /api/channel-sources/{id}/refresh", s.adminOnly(s.refreshChannelSource))
	mux.HandleFunc("PATCH /api/channel-sources/{id}", s.adminOnly(s.patchChannelSource))
	mux.HandleFunc("DELETE /api/channel-sources/{id}", s.adminOnly(s.deleteChannelSource))
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

	mux.HandleFunc("GET /api/items/{id}/download", s.download)
	mux.HandleFunc("GET /api/items/{id}/rating", s.getRating)
	mux.HandleFunc("PUT /api/items/{id}/rating", s.putRating)
	mux.HandleFunc("DELETE /api/items/{id}/rating", s.deleteRating)
	mux.HandleFunc("GET /api/items/{id}/playback", s.playback)
	mux.HandleFunc("GET /api/items/{id}/trailer", s.trailer)
	// A show's play actions. Not admin-gated: they read what the caller may
	// already browse, and Continue is per-user by construction.
	mux.HandleFunc("GET /api/items/{id}/continue", s.continueShow)
	mux.HandleFunc("GET /api/items/{id}/episodes", s.showEpisodes)
	mux.HandleFunc("GET /api/items/{id}/subtitles", s.listSubtitles)
	mux.HandleFunc("GET /api/items/{id}/subtitles/search", s.searchSubtitles)
	mux.HandleFunc("POST /api/items/{id}/subtitles/download", s.downloadSubtitle)
	mux.HandleFunc("GET /api/items/{id}/subtitles/{key}", s.serveSubtitle)
	mux.HandleFunc("DELETE /api/items/{id}/subtitles/{key}", s.deleteSubtitle)

	// Playlist editing (ADR 0030). Not adminOnly, and playlists.go says why.
	mux.HandleFunc("POST /api/playlists", s.createPlaylist)
	mux.HandleFunc("DELETE /api/playlists/{id}", s.deletePlaylist)
	mux.HandleFunc("PUT /api/playlists/{id}/entries", s.setPlaylistEntries)
	mux.HandleFunc("POST /api/playlists/{id}/entries", s.addPlaylistEntries)
	mux.HandleFunc("DELETE /api/playlists/{id}/entries/{pos}", s.removePlaylistEntry)

	mux.HandleFunc("GET /api/together", s.listTogether)
	mux.HandleFunc("POST /api/together", s.createTogether)
	mux.HandleFunc("POST /api/together/{id}/join", s.joinTogether)
	mux.HandleFunc("GET /api/together/{id}", s.pollTogether)
	mux.HandleFunc("PUT /api/together/{id}", s.reportTogether)
	mux.HandleFunc("DELETE /api/together/{id}", s.leaveTogether)

	mux.HandleFunc("GET /api/review", s.reviewQueue)
	mux.HandleFunc("GET /api/enrich", s.enrichStatus)
	mux.HandleFunc("GET /api/probe", s.probeStatus)
	mux.HandleFunc("GET /api/activity", s.activity)
	mux.HandleFunc("GET /api/logs", s.adminOnly(s.serverLog))
	mux.HandleFunc("GET /api/crashes", s.adminOnly(s.listCrashes))
	mux.HandleFunc("DELETE /api/crashes", s.adminOnly(s.clearCrashes))
	mux.HandleFunc("GET /api/audit", s.adminOnly(s.listAudit))
	mux.HandleFunc("GET /api/update", s.adminOnly(s.updateStatus))
	mux.HandleFunc("POST /api/update/check", s.adminOnly(s.checkForUpdate))
	mux.HandleFunc("POST /api/update/download", s.adminOnly(s.downloadUpdate))
	mux.HandleFunc("POST /api/update/restart", s.adminOnly(s.restartForUpdate))
	mux.HandleFunc("POST /api/probe/refresh", s.adminOnly(s.reprobe))
	mux.HandleFunc("GET /api/coverart", s.coverArtStatus)
	mux.HandleFunc("POST /api/coverart/refresh", s.adminOnly(s.recoverArt))
	mux.HandleFunc("GET /api/artwork/{hash}", s.serveArtwork)

	mux.HandleFunc("GET /api/settings", s.adminOnly(s.getSettings))
	mux.HandleFunc("PUT /api/settings", s.adminOnly(s.putSettings))
	mux.HandleFunc("POST /api/settings/reset", s.adminOnly(s.resetSettings))
	mux.HandleFunc("POST /api/cache/clear", s.adminOnly(s.clearCache))

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
	mux.HandleFunc("PATCH /api/users/{id}", s.adminOnly(s.patchUser))
	mux.HandleFunc("POST /api/users/{id}/password", s.adminOnly(s.resetUserPassword))

	// A picture is served by item id like a stream, and for the same reason:
	// the client knows items, not paths, and paths never leave the server.
	mux.HandleFunc("GET /api/items/{id}/photo", s.photo)
	mux.HandleFunc("GET /api/stream/{id}", s.stream)
	mux.HandleFunc("GET /api/stream/{id}/transcode", s.transcodeStream)
	mux.HandleFunc("GET /api/stream/{id}/hls/index.m3u8", s.hlsPlaylist)
	mux.HandleFunc("GET /api/stream/{id}/hls/{session}/{name}", s.hlsSegment)
	mux.HandleFunc("GET /api/transcode", s.transcodeSessions)

	/*
	 * Anything under /api that matched no route is a 404 in the documented
	 * error shape — not the client.
	 *
	 * Without this, an unknown API path fell through to the SPA fallback below
	 * and answered **200 with an HTML page**. A browser never notices, because
	 * a browser never asks for a route that does not exist; a third-party
	 * client asking for a mistyped or newer endpoint got a success and a
	 * document, which is the least debuggable answer available.
	 *
	 * It also quietly contradicted the version contract: ADR 0018 promises that
	 * a client can tell what this server supports, and a 200 for every path is
	 * a server that claims to support everything. Found by trying an endpoint
	 * that was never built.
	 *
	 * Go 1.22 routing prefers the more specific pattern, so every registered
	 * route still wins over this.
	 */
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found",
			"no such endpoint: "+r.Method+" "+r.URL.Path)
	})

	mux.Handle("/", s.web)
	// Outermost after logging: a panic anywhere inside — including in the auth
	// middleware — is recovered and recorded, and the request that caused it is
	// already in the log above.
	// Version negotiation sits outside auth: a client asking for a contract this
	// build cannot serve should be told so, not told to sign in first.
	return logRequests(s.log,
		s.recoverPanics(negotiateAPIVersion(s.requireAuth(mux))))
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "version": Version, "api_version": APIVersion,
		// Every contract revision this build can serve. A future v2 server is
		// expected to keep answering v1 for a release (ADR 0018), so "which
		// versions" is a different question from "which version am I getting".
		"api_versions": SupportedAPIVersions,
	})
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
