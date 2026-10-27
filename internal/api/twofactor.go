package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"lancast/internal/auth"
	"lancast/internal/secret"
	"lancast/internal/store"
)

/*
 * Two-factor sign-in (ADR 0100).
 *
 * Each person turns it on for themselves; nobody can turn it on for somebody
 * else, and an administrator can only take it off (a lost phone). With it on,
 * the right password no longer signs anybody in. It earns a challenge: an
 * unguessable token, good for five minutes and five tries, which a second
 * request trades — with a six-digit code from the app, or one of the recovery
 * codes — for the session.
 *
 * Passkey sign-in skips it. A passkey is already two factors (the device, and
 * what unlocks it), and asking for a code after one would be ceremony.
 *
 * The per-address throttle and the per-account wait are not cleared by the
 * password alone. They are cleared when the whole sign-in succeeds, and a
 * wrong code counts against both, so knowing the password does not buy a
 * fresh budget for guessing codes.
 */

const (
	twoFactorChallengeFor   = 5 * time.Minute
	twoFactorChallengeTries = 5
	twoFactorSetupFor       = 10 * time.Minute
	twoFactorRecoveryCodes  = 10
	twoFactorIssuer         = "LANcast"
)

// SetTwoFactorSealer lets accounts turn two-factor on. Without it the setup
// route says it is unavailable; an account already using it can still sign in
// with a recovery code.
func (s *Server) SetTwoFactorSealer(sl *secret.Sealer) { s.tfSealer = sl }

type tfChallenge struct {
	userID string
	until  time.Time
	tries  int
}

type tfPending struct {
	secret []byte
	until  time.Time
}

// twoFactorBook holds the challenges sign-ins are waiting on, and the secrets
// people are part-way through adding to an app. In memory: a restart costs a
// sign-in in progress, nothing more.
type twoFactorBook struct {
	mu         sync.Mutex
	challenges map[string]*tfChallenge
	pending    map[string]tfPending
}

func newTwoFactorBook() *twoFactorBook {
	return &twoFactorBook{challenges: map[string]*tfChallenge{}, pending: map[string]tfPending{}}
}

func (b *twoFactorBook) issue(userID string, now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, c := range b.challenges {
		if now.After(c.until) {
			delete(b.challenges, k)
		}
	}
	b.challenges[tok] = &tfChallenge{userID: userID, until: now.Add(twoFactorChallengeFor)}
	return tok, nil
}

// attempt spends one try on a challenge and returns whose it is. A challenge
// that has run out of time or tries is gone.
func (b *twoFactorBook) attempt(tok string, now time.Time) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.challenges[tok]
	if !ok {
		return "", false
	}
	if now.After(c.until) || c.tries >= twoFactorChallengeTries {
		delete(b.challenges, tok)
		return "", false
	}
	c.tries++
	return c.userID, true
}

func (b *twoFactorBook) done(tok string) {
	b.mu.Lock()
	delete(b.challenges, tok)
	b.mu.Unlock()
}

func (b *twoFactorBook) setPending(userID string, sec []byte, now time.Time) {
	b.mu.Lock()
	b.pending[userID] = tfPending{secret: sec, until: now.Add(twoFactorSetupFor)}
	b.mu.Unlock()
}

func (b *twoFactorBook) takePending(userID string, now time.Time) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pending[userID]
	if !ok || now.After(p.until) {
		delete(b.pending, userID)
		return nil, false
	}
	return p.secret, true
}

func (b *twoFactorBook) dropPending(userID string) {
	b.mu.Lock()
	delete(b.pending, userID)
	b.mu.Unlock()
}

// twoFactorChallenge answers a correct password for an account with
// two-factor on. Reports false when the account has none and the caller should
// sign them in as before.
func (s *Server) twoFactorChallenge(w http.ResponseWriter, r *http.Request, u *store.User) (bool, error) {
	tf, err := s.st.TwoFactorFor(r.Context(), u.ID)
	if err != nil {
		return false, err
	}
	if !tf.Enabled {
		return false, nil
	}
	tok, err := s.tfBook.issue(u.ID, time.Now())
	if err != nil {
		return false, err
	}
	s.log.Info("sign-in waiting for a code", "account", u.Name, "client", auth.ClientKey(r))
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
		"two_factor":    true,
		"challenge":     tok,
	})
	return true, nil
}

// authLoginTwoFactor is the second half of a sign-in: a challenge and a code,
// or a challenge and a recovery code, for a session.
func (s *Server) authLoginTwoFactor(w http.ResponseWriter, r *http.Request) {
	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		s.log.Warn("login refused", "reason", "throttled", "client", key)
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many attempts; wait a few minutes")
		return
	}
	var req struct {
		Challenge    string `json:"challenge"`
		Code         string `json:"code"`
		RecoveryCode string `json:"recovery_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if (req.Code == "") == (req.RecoveryCode == "") {
		writeError(w, http.StatusBadRequest, "bad_request", "send a code or a recovery code")
		return
	}
	userID, ok := s.tfBook.attempt(req.Challenge, time.Now())
	if !ok {
		s.log.Warn("login refused", "reason", "two-factor challenge expired or unknown", "client", key)
		writeError(w, http.StatusUnauthorized, "unauthorized",
			"this sign-in has expired; enter your password again")
		return
	}
	u, err := s.st.UserByID(r.Context(), userID)
	if err != nil {
		s.notFoundOr(w, err, "lookup user", "this sign-in has expired; enter your password again")
		return
	}
	inside := auth.InsideNetwork(r)
	if !inside {
		if ok, left := s.accountWait.Allow(u.Name); !ok {
			s.log.Warn("login refused", "reason", "account waiting", "wait", left.Round(time.Second), "client", key)
			writeError(w, http.StatusTooManyRequests, "too_many_requests",
				"too many attempts; wait a few minutes")
			return
		}
	}

	good, how, err := s.checkSecondFactor(r, userID, req.Code, req.RecoveryCode)
	if err != nil {
		s.writeInternal(w, err, "check two-factor code")
		return
	}
	if !good {
		s.log.Warn("login refused", "reason", "wrong "+how, "account", u.Name, "client", key)
		if !inside {
			s.accountWait.Fail(u.Name)
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "that code is not right")
		return
	}

	s.tfBook.done(req.Challenge)
	s.throttle.Reset(key)
	s.accountWait.Reset(u.Name)
	if err := s.issueSession(w, r, u.ID); err != nil {
		return
	}
	if how == "recovery code" {
		s.audit(r, "user.two_factor_recovery", "user", u.ID,
			fmt.Sprintf("%q signed in with a recovery code", u.Name), nil)
	}
	s.log.Info("signed in", "account", u.Name, "with", how, "client", key)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          userJSON(u.ID, u.Name, u.Role),
	})
}

/*
 * checkSecondFactor checks a code or a recovery code, spending it if right.
 * `how` names which, for the log.
 *
 * A code needs the sealed secret opened. On a database restored to another
 * machine it cannot be — the sealing key stays beside the database it was made
 * for — and then only a recovery code works, which is what they are for.
 */
func (s *Server) checkSecondFactor(r *http.Request, userID, code, recovery string) (bool, string, error) {
	if recovery != "" {
		norm := auth.NormalizeRecoveryCode(recovery)
		if norm == "" {
			return false, "recovery code", nil
		}
		ok, err := s.st.UseRecoveryCode(r.Context(), userID, auth.HashToken(norm))
		return ok, "recovery code", err
	}
	tf, err := s.st.TwoFactorFor(r.Context(), userID)
	if err != nil {
		return false, "code", err
	}
	if !tf.Enabled || s.tfSealer == nil {
		return false, "code", nil
	}
	sec, err := s.tfSealer.Open(tf.Sealed)
	if errors.Is(err, secret.ErrCannotOpen) {
		s.log.Warn("two-factor secret cannot be opened on this machine; only recovery codes will work", "account", userID)
		return false, "code", nil
	}
	if err != nil {
		return false, "code", err
	}
	step, ok := auth.VerifyTOTP(sec, code, time.Now(), tf.LastStep)
	if !ok {
		return false, "code", nil
	}
	// Spent in one statement, so the same code racing in twice wins once.
	spent, err := s.st.UseTwoFactorStep(r.Context(), userID, step)
	return spent, "code", err
}

// getTwoFactor is the caller's own state, for the Security pane.
func (s *Server) getTwoFactor(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	tf, err := s.st.TwoFactorFor(r.Context(), sess.UserID)
	if s.notFoundOr(w, err, "read two-factor", "no such user") {
		return
	}
	out := map[string]any{
		"enabled":             tf.Enabled,
		"available":           s.tfSealer != nil,
		"recovery_codes_left": len(tf.Recovery),
	}
	if tf.Enabled {
		out["enabled_at"] = tf.EnabledAt
	}
	writeJSON(w, http.StatusOK, out)
}

// startTwoFactor makes a secret for the caller to add to an app. Nothing is
// stored until a code from that app comes back.
func (s *Server) startTwoFactor(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	if s.tfSealer == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"two-factor sign-in is unavailable: the server could not prepare its sealing key")
		return
	}
	sec, err := auth.NewTOTPSecret()
	if err != nil {
		s.writeInternal(w, err, "make two-factor secret")
		return
	}
	s.tfBook.setPending(sess.UserID, sec, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{
		"key": auth.TOTPKey(sec),
		"url": auth.TOTPURL(twoFactorIssuer, sess.Name, sec),
	})
}

// enableTwoFactor turns it on, once a code shows the app has the secret. The
// recovery codes are in this answer and never again.
func (s *Server) enableTwoFactor(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	if s.tfSealer == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"two-factor sign-in is unavailable: the server could not prepare its sealing key")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	now := time.Now()
	sec, ok := s.tfBook.takePending(sess.UserID, now)
	if !ok {
		writeError(w, http.StatusConflict, "conflict", "start again: the setup has expired")
		return
	}
	step, ok := auth.VerifyTOTP(sec, req.Code, now, 0)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"that code is not right; check the app's clock and try the next one")
		return
	}
	sealed, err := s.tfSealer.Seal(sec)
	if err != nil {
		s.writeInternal(w, err, "seal two-factor secret")
		return
	}
	codes, hashes, err := newRecoverySet()
	if err != nil {
		s.writeInternal(w, err, "make recovery codes")
		return
	}
	if err := s.st.EnableTwoFactor(r.Context(), sess.UserID, sealed, hashes, now.Unix()); s.notFoundOr(w, err, "enable two-factor", "no such user") {
		return
	}
	// The code just typed is spent too.
	_, _ = s.st.UseTwoFactorStep(r.Context(), sess.UserID, step)
	s.tfBook.dropPending(sess.UserID)
	s.audit(r, "profile.two_factor_on", "user", sess.UserID,
		fmt.Sprintf("%q turned on two-factor sign-in", sess.Name), nil)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// disableTwoFactor turns the caller's off, and newRecoveryCodes replaces
// theirs. Both ask for the password: a session left open on somebody else's
// screen should not be enough to weaken the account or read fresh codes.
func (s *Server) disableTwoFactor(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.passwordConfirmed(w, r)
	if !ok {
		return
	}
	if err := s.st.DisableTwoFactor(r.Context(), sess.UserID); s.notFoundOr(w, err, "disable two-factor", "no such user") {
		return
	}
	s.audit(r, "profile.two_factor_off", "user", sess.UserID,
		fmt.Sprintf("%q turned off two-factor sign-in", sess.Name), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.passwordConfirmed(w, r)
	if !ok {
		return
	}
	tf, err := s.st.TwoFactorFor(r.Context(), sess.UserID)
	if s.notFoundOr(w, err, "read two-factor", "no such user") {
		return
	}
	if !tf.Enabled {
		writeError(w, http.StatusConflict, "conflict", "two-factor sign-in is off")
		return
	}
	codes, hashes, err := newRecoverySet()
	if err != nil {
		s.writeInternal(w, err, "make recovery codes")
		return
	}
	if err := s.st.EnableTwoFactor(r.Context(), sess.UserID, tf.Sealed, hashes, tf.EnabledAt); err != nil {
		s.writeInternal(w, err, "replace recovery codes")
		return
	}
	// EnableTwoFactor resets the spent step; put it back.
	if tf.LastStep > 0 {
		_, _ = s.st.UseTwoFactorStep(r.Context(), sess.UserID, tf.LastStep)
	}
	s.audit(r, "profile.two_factor_codes", "user", sess.UserID,
		fmt.Sprintf("%q made new recovery codes", sess.Name), nil)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// removeUserTwoFactor is the administrator's way back in for somebody who lost
// their phone and their codes. It can only take two-factor off, never put it
// on, and it is recorded.
func (s *Server) removeUserTwoFactor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := s.st.UserByID(r.Context(), id)
	if s.notFoundOr(w, err, "lookup user", "no such user") {
		return
	}
	if err := s.st.DisableTwoFactor(r.Context(), id); s.notFoundOr(w, err, "remove two-factor", "no such user") {
		return
	}
	s.audit(r, "user.two_factor_removed", "user", id,
		fmt.Sprintf("Removed two-factor sign-in from %q", u.Name), nil)
	w.WriteHeader(http.StatusNoContent)
}

// passwordConfirmed reads {"current_password"} and checks it against the caller's
// account, on the same throttle as sign-in (see authChangePassword).
func (s *Server) passwordConfirmed(w http.ResponseWriter, r *http.Request) (*store.Session, bool) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return sess, false
	}
	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many attempts; wait a few minutes")
		return sess, false
	}
	var req struct {
		Password string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return sess, false
	}
	u, err := s.st.UserByID(r.Context(), sess.UserID)
	if s.notFoundOr(w, err, "lookup user", "no such user") {
		return sess, false
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusForbidden, "forbidden", "that password is not right")
		return sess, false
	}
	s.throttle.Reset(key)
	return sess, true
}

func newRecoverySet() (codes, hashes []string, err error) {
	codes, err = auth.NewRecoveryCodes(twoFactorRecoveryCodes)
	if err != nil {
		return nil, nil, err
	}
	hashes = make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = auth.HashToken(c)
	}
	return codes, hashes, nil
}
