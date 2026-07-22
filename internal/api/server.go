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
	"strconv"

	"lancast/internal/scan"
	"lancast/internal/store"
)

// Version is reported by GET /api/health.
const Version = "0.1.0"

// localUser is the single-user identity for M1. The schema is already keyed by
// user (ADR 0006), so multi-user arrives without a migration.
const localUser = "local"

// Server holds the API dependencies.
type Server struct {
	st      *store.Store
	scanner *scan.Scanner
	log     *slog.Logger
	web     http.Handler
}

func New(st *store.Store, sc *scan.Scanner, log *slog.Logger, web http.Handler) *Server {
	return &Server{st: st, scanner: sc, log: log, web: web}
}

// Handler builds the router. Go 1.22 method-and-pattern routing covers this
// without a third-party dependency.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.health)

	mux.HandleFunc("GET /api/libraries", s.listLibraries)
	mux.HandleFunc("POST /api/libraries", s.createLibrary)
	mux.HandleFunc("POST /api/libraries/{id}/scan", s.startScan)
	mux.HandleFunc("GET /api/libraries/{id}/scan", s.scanStatus)

	mux.HandleFunc("GET /api/items", s.listItems)
	mux.HandleFunc("GET /api/items/{id}", s.getItem)
	mux.HandleFunc("PUT /api/items/{id}/progress", s.putProgress)

	mux.HandleFunc("GET /api/stream/{id}", s.stream)

	mux.Handle("/", s.web)
	return logRequests(s.log, mux)
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
	case errors.Is(err, store.ErrNotFound):
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
