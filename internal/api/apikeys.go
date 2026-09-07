package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lancast/internal/auth"
	"lancast/internal/store"
)

/*
 * API keys over HTTP (ADR 0061).
 *
 * A key is how something that is not a browser authenticates: a script, a CLI,
 * a third-party client built against `docs/openapi.json`. The alternative was
 * storing somebody's password and keeping a cookie alive against a TTL chosen
 * for a person watching a film.
 *
 * THE ROUTES THAT MANAGE KEYS ARE SESSION-ONLY
 *
 * Not because they are administration — they are not, a key belongs to whoever
 * made it — but because a key that can mint keys is a key that cannot be
 * revoked by revoking it. Somebody who takes a stolen key can make a second one
 * and keep it after the first is deleted, which turns a bounded mistake into a
 * permanent one. Minting requires the password-backed credential.
 */

// maxKeyName keeps a name to something a list can display. Nothing depends on
// the limit; it exists so a key cannot be given a novel as a label.
const maxKeyName = 64

// listAPIKeys returns the caller's own keys. Never anybody else's, and never
// the secrets — those exist once, in the response that created them.
func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	if !s.keysManageable(w, r) {
		return
	}
	keys, err := s.st.ListAPIKeys(r.Context(), s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "list api keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

/*
 * createAPIKey mints a key and returns it exactly once.
 *
 * The plaintext is in this response and nowhere else — the database holds only
 * its hash, the same treatment sessions get. The client has to say so plainly,
 * because somebody who closes the dialog needs a new key, and that is a much
 * better outcome than a database full of retrievable credentials.
 */
func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	if !s.keysManageable(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		// Required rather than defaulted. A list of keys called "key" is a list
		// nobody can revoke confidently, which is the one thing the list is for.
		writeError(w, http.StatusBadRequest, "bad_request",
			"a name is required, so this key can be told apart later")
		return
	}
	if len(name) > maxKeyName {
		name = name[:maxKeyName]
	}

	token, hash, err := auth.NewToken()
	if err != nil {
		s.writeInternal(w, err, "generate api key")
		return
	}
	key, err := s.st.CreateAPIKey(r.Context(), hash, s.userID(r), name)
	if err != nil {
		s.writeInternal(w, err, "create api key")
		return
	}

	s.audit(r, "apikey.create", "user", name, "created an API key", nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"key": key,
		// The only time this value exists outside the caller's own storage.
		"secret": token,
	})
}

// deleteAPIKey revokes one of the caller's keys. Scoped in the query rather
// than checked here, so the database refuses somebody else's key even if this
// handler is later rewritten by someone who forgets to.
func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if !s.keysManageable(w, r) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which key?")
		return
	}
	err := s.st.DeleteAPIKey(r.Context(), id, s.userID(r))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such key")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "delete api key")
		return
	}
	s.audit(r, "apikey.revoke", "user", id, "revoked an API key", nil)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

/*
 * keysManageable refuses a caller that is itself using a key.
 *
 * A key that can mint keys cannot be revoked by revoking it: whoever has it
 * makes a second one and keeps that after the first is deleted. Revocation has
 * to be the end of the story, so managing keys needs the credential a person
 * types.
 */
func (s *Server) keysManageable(w http.ResponseWriter, r *http.Request) bool {
	if authedByKey(r) {
		writeError(w, http.StatusForbidden, "forbidden",
			"an API key cannot manage API keys; sign in instead")
		return false
	}
	if _, ok := sessionFromContext(r); !ok {
		// The unconfigured loopback state, where no account exists yet. A key
		// belongs to an account, so there is nothing to own one.
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return false
	}
	return true
}
