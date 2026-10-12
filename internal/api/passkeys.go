package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"lancast/internal/auth"
	"lancast/internal/auth/passkey"
	"lancast/internal/store"
)

/*
 * Passkeys (ADR 0082).
 *
 * A passkey is bound to a name, never an address, and browsers refuse WebAuthn
 * on a certificate they do not trust. So passkeys exist only on a request that
 * arrived over TLS for the public certificate's own name (ADR 0081), while
 * that certificate is valid. Everywhere else — by IP, on localhost, in the
 * desktop window — they are reported unavailable and the routes refuse,
 * rather than offering a button that cannot work.
 */

const passkeyTimeoutMS = 300000 // matches the challenge lifetime

// passkeyRP returns the RP ID for this request, or false when passkeys
// cannot work on it.
func (s *Server) passkeyRP(r *http.Request) (string, bool) {
	if s.passkeyRPFor != nil {
		return s.passkeyRPFor(r)
	}
	if s.publicCert == nil || r.TLS == nil {
		return "", false
	}
	pc := s.settings.Get().PublicCert
	if !pc.Enabled || pc.Hostname == "" {
		return "", false
	}
	st := s.publicCert.Status()
	if st.NotAfter.IsZero() || !time.Now().Before(st.NotAfter) {
		return "", false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	if !strings.EqualFold(strings.TrimSuffix(host, "."), pc.Hostname) {
		return "", false
	}
	return pc.Hostname, true
}

func (s *Server) passkeyUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "conflict",
		"passkeys work only at the server's public name over HTTPS")
}

var b64 = base64.RawURLEncoding

// unb64 accepts base64url with or without padding, the two forms browsers
// and libraries produce.
func unb64(s string) ([]byte, error) {
	return b64.DecodeString(strings.TrimRight(s, "="))
}

func (s *Server) passkeyChallengeFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, passkey.ErrTooManyChallenges) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many sign-ins in progress; try again shortly")
		return
	}
	s.writeInternal(w, err, "issue passkey challenge")
}

// passkeyLoginOptions starts a sign-in. Public: whoever is signing in is not
// signed in yet. Nothing about any account is returned — the credential list
// is empty, because discoverable passkeys say whose they are.
func (s *Server) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	rp, ok := s.passkeyRP(r)
	if !ok {
		s.passkeyUnavailable(w)
		return
	}
	id, ch, err := s.passkeyChallenges.Issue(passkey.PurposeLogin, "")
	if err != nil {
		s.passkeyChallengeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": id,
		"public_key": map[string]any{
			"challenge":        b64.EncodeToString(ch),
			"rpId":             rp,
			"timeout":          passkeyTimeoutMS,
			"userVerification": "required",
		},
	})
}

// passkeyLogin finishes a sign-in and issues an ordinary session.
func (s *Server) passkeyLogin(w http.ResponseWriter, r *http.Request) {
	rp, ok := s.passkeyRP(r)
	if !ok {
		s.passkeyUnavailable(w)
		return
	}
	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		s.log.Warn("login refused", "reason", "throttled", "method", "passkey", "client", key)
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many attempts; wait a few minutes")
		return
	}
	var req struct {
		ChallengeID       string `json:"challenge_id"`
		ID                string `json:"id"`
		ClientDataJSON    string `json:"client_data_json"`
		AuthenticatorData string `json:"authenticator_data"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"user_handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	// One answer for every refusal, as password login gives one answer for a
	// wrong name and a wrong password. The reason goes to the log.
	refuse := func(reason string, err error) {
		s.log.Warn("login refused", "reason", reason, "method", "passkey", "client", key, "error", err)
		writeError(w, http.StatusUnauthorized, "unauthorized", "that passkey could not sign you in")
	}

	challenge, _, ok := s.passkeyChallenges.Take(req.ChallengeID, passkey.PurposeLogin)
	if !ok {
		refuse("unknown or expired challenge", nil)
		return
	}
	credID, err1 := unb64(req.ID)
	cd, err2 := unb64(req.ClientDataJSON)
	ad, err3 := unb64(req.AuthenticatorData)
	sig, err4 := unb64(req.Signature)
	uh, err5 := unb64(req.UserHandle)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil || len(credID) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "passkey response fields must be base64url")
		return
	}
	pk, err := s.st.PasskeyByCredentialID(r.Context(), credID)
	if errors.Is(err, store.ErrNotFound) {
		refuse("no such passkey", nil)
		return
	} else if err != nil {
		s.writeInternal(w, err, "look up passkey")
		return
	}
	// A discoverable passkey names its account; it must name the one it was
	// registered to.
	if string(uh) != pk.UserID {
		refuse("user handle does not match", nil)
		return
	}
	got, err := passkey.VerifyAssertion(passkey.Expect{
		Challenge: challenge, RPID: rp, Origin: passkey.OriginForRPID(rp),
	}, pk.PublicKey, cd, ad, sig)
	if err != nil {
		refuse("verification failed", err)
		return
	}
	/*
	 * The signature counter (ADR 0082 §4). Synced passkeys report zero for
	 * ever, which says nothing. A counter that has been used must go up: one
	 * that does not is the specification's sign of a cloned authenticator,
	 * and the sign-in is refused and said out loud.
	 */
	if (got.SignCount != 0 || pk.SignCount != 0) && got.SignCount <= pk.SignCount {
		s.log.Warn("passkey counter did not increase; possible cloned authenticator",
			"passkey", pk.ID, "stored", pk.SignCount, "presented", got.SignCount, "client", key)
		refuse("counter did not increase", nil)
		return
	}
	if err := s.st.RecordPasskeyUse(r.Context(), pk.ID, got.SignCount, got.BackupState); err != nil {
		s.writeInternal(w, err, "record passkey use")
		return
	}
	u, err := s.st.UserByID(r.Context(), pk.UserID)
	if err != nil {
		s.writeInternal(w, err, "load user")
		return
	}
	s.throttle.Reset(key)
	if err := s.issueSession(w, r, u.ID); err != nil {
		return
	}
	s.log.Info("signed in", "account", u.Name, "method", "passkey", "client", key)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          userJSON(u.ID, u.Name, u.Role),
	})
}

// passkeyRegisterOptions starts adding a passkey to the caller's account.
//
// It asks for the current password, for the reason changing the password
// does: a passkey is a new way into the account, and a session somebody left
// open must not be enough to add one. Throttled with the same counters as
// login and password change, because it asks the same question.
func (s *Server) passkeyRegisterOptions(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	rp, ok := s.passkeyRP(r)
	if !ok {
		s.passkeyUnavailable(w)
		return
	}
	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many attempts; wait a few minutes")
		return
	}
	var req struct {
		Current string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	u, err := s.st.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "load user")
		return
	}
	inside := auth.InsideNetwork(r)
	if !inside {
		if ok, _ := s.accountWait.Allow(u.Name); !ok {
			writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many attempts; wait a few minutes")
			return
		}
	}
	if !auth.CheckPassword(u.PasswordHash, req.Current) {
		if !inside {
			s.accountWait.Fail(u.Name)
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "current password is incorrect")
		return
	}
	s.throttle.Reset(key)
	s.accountWait.Reset(u.Name)

	existing, err := s.st.PasskeysByUser(r.Context(), u.ID)
	if err != nil {
		s.writeInternal(w, err, "list passkeys")
		return
	}
	exclude := make([]map[string]any, 0, len(existing))
	for _, p := range existing {
		exclude = append(exclude, map[string]any{"type": "public-key", "id": b64.EncodeToString(p.CredentialID)})
	}
	params := make([]map[string]any, 0, len(passkey.Algorithms))
	for _, alg := range passkey.Algorithms {
		params = append(params, map[string]any{"type": "public-key", "alg": alg})
	}
	id, ch, err := s.passkeyChallenges.Issue(passkey.PurposeRegister, u.ID)
	if err != nil {
		s.passkeyChallengeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": id,
		"public_key": map[string]any{
			"challenge": b64.EncodeToString(ch),
			"rp":        map[string]any{"id": rp, "name": "LANcast"},
			"user": map[string]any{
				"id":          b64.EncodeToString([]byte(u.ID)),
				"name":        u.Name,
				"displayName": u.Name,
			},
			"pubKeyCredParams": params,
			"authenticatorSelection": map[string]any{
				"residentKey":        "required",
				"requireResidentKey": true,
				"userVerification":   "required",
			},
			"attestation":        "none",
			"excludeCredentials": exclude,
			"timeout":            passkeyTimeoutMS,
		},
	})
}

// cleanPasskeyName trims a name and bounds it, falling back to "Passkey".
func cleanPasskeyName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Passkey"
	}
	if utf8.RuneCountInString(name) > 64 {
		name = string([]rune(name)[:64])
	}
	return name
}

// passkeyRegister finishes adding a passkey.
func (s *Server) passkeyRegister(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	rp, ok := s.passkeyRP(r)
	if !ok {
		s.passkeyUnavailable(w)
		return
	}
	var req struct {
		ChallengeID       string `json:"challenge_id"`
		Name              string `json:"name"`
		ClientDataJSON    string `json:"client_data_json"`
		AttestationObject string `json:"attestation_object"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	challenge, owner, ok := s.passkeyChallenges.Take(req.ChallengeID, passkey.PurposeRegister)
	// The challenge was issued to one account, after its password; another
	// session cannot spend it.
	if !ok || owner != sess.UserID {
		writeError(w, http.StatusBadRequest, "bad_request", "that passkey request has expired; start again")
		return
	}
	cd, err1 := unb64(req.ClientDataJSON)
	att, err2 := unb64(req.AttestationObject)
	if errors.Join(err1, err2) != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "passkey response fields must be base64url")
		return
	}
	cred, err := passkey.VerifyRegistration(passkey.Expect{
		Challenge: challenge, RPID: rp, Origin: passkey.OriginForRPID(rp),
	}, cd, att)
	if err != nil {
		s.log.Warn("passkey registration refused", "account", sess.Name, "error", err)
		writeError(w, http.StatusBadRequest, "bad_request", "that passkey could not be added")
		return
	}
	name := cleanPasskeyName(req.Name)
	id, err := s.st.AddPasskey(r.Context(), store.Passkey{
		UserID: sess.UserID, CredentialID: cred.ID, PublicKey: cred.PublicKey,
		Algorithm: cred.Algorithm, SignCount: cred.SignCount, AAGUID: cred.AAGUID,
		BackupEligible: cred.BackupEligible, BackupState: cred.BackupState,
		Name: name, RPID: rp,
	})
	if errors.Is(err, store.ErrPasskeyExists) {
		writeError(w, http.StatusConflict, "conflict", "that passkey is already registered")
		return
	} else if err != nil {
		s.writeInternal(w, err, "add passkey")
		return
	}
	s.audit(r, "passkey.add", "user", sess.UserID, "Added passkey \""+name+"\"",
		map[string]any{"passkey": id})
	s.log.Info("passkey added", "account", sess.Name, "passkey", id)
	list, err := s.st.PasskeysByUser(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "list passkeys")
		return
	}
	for _, p := range list {
		if p.ID == id {
			writeJSON(w, http.StatusCreated, passkeyJSON(p, rp))
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func passkeyJSON(p store.Passkey, currentRP string) map[string]any {
	out := map[string]any{
		"id":         p.ID,
		"name":       p.Name,
		"created_at": time.Unix(p.CreatedAt, 0).UTC(),
		"synced":     p.BackupState,
		"rp_id":      p.RPID,
		// A passkey made for another name cannot be used at this one; listed
		// rather than hidden, so it can be recognised and removed.
		"usable": currentRP != "" && strings.EqualFold(p.RPID, currentRP),
	}
	if p.LastUsedAt != 0 {
		out["last_used_at"] = time.Unix(p.LastUsedAt, 0).UTC()
	}
	return out
}

// listPasskeys lists the caller's own passkeys. Available on any connection,
// so somebody signed in by IP can still see and remove them.
func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	list, err := s.st.PasskeysByUser(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "list passkeys")
		return
	}
	rp := s.settings.Get().PublicCert.Hostname
	if !s.settings.Get().PublicCert.Enabled {
		rp = ""
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, passkeyJSON(p, rp))
	}
	here, _ := s.passkeyRP(r)
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out, "available_here": here != ""})
}

func (s *Server) renamePasskey(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	name := cleanPasskeyName(req.Name)
	err := s.st.RenamePasskey(r.Context(), sess.UserID, r.PathValue("id"), name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such passkey")
		return
	} else if err != nil {
		s.writeInternal(w, err, "rename passkey")
		return
	}
	s.audit(r, "passkey.rename", "user", sess.UserID, "Renamed a passkey to \""+name+"\"",
		map[string]any{"passkey": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// deletePasskey removes one of the caller's passkeys. No password is asked:
// removing a way in only ever makes the account harder to enter.
func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	err := s.st.DeletePasskey(r.Context(), sess.UserID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such passkey")
		return
	} else if err != nil {
		s.writeInternal(w, err, "delete passkey")
		return
	}
	s.audit(r, "passkey.remove", "user", sess.UserID, "Removed a passkey",
		map[string]any{"passkey": r.PathValue("id")})
	s.log.Info("passkey removed", "account", sess.Name, "passkey", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}
