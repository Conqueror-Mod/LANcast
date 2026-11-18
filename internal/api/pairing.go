package api

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"lancast/internal/auth"
	"lancast/internal/devicepair"
)

/*
 * A TV pairs by a code it shows and a person types (ADR 0125 §1–2).
 *
 *   POST /api/devices/pair              the TV, no session: name in, token and nonce out
 *   GET  /api/devices/pair/certificate  the TV, no session: the leaf it was served
 *   POST /api/devices/pair/poll         the TV, no session: 202, then its key once
 *   GET  /api/devices/pair/{code}       a person, signed in: what they would approve
 *   POST /api/devices/pair/approve      a person, signed in: yes (with the password) or no
 *
 * The key is the approver's and is an ordinary API key (ADR 0061): hashed at
 * rest, listed and revoked in Settings, never an admin route. It is made when
 * the TV collects it, not when the person approves, so an approval nobody
 * collects leaves no key behind.
 *
 * THE ORDER THE TV DOES THIS IN IS PART OF THE DEFENCE
 *
 * The TV reads the certificate first, over a connection it cannot verify, and
 * then makes every pairing request, the poll included, pinned to the
 * certificate it read. If it did not pin, a machine that simply relayed
 * /certificate from the real server would let the TV compute the right code
 * and then read the key out of the unpinned poll. Pinned, the relay has to
 * present the real certificate, which it cannot without the real key; and an
 * impostor that serves its own gets a code the server never filed.
 */

func newPairThrottle() *auth.Throttle {
	t := auth.NewThrottle()
	t.Window = 10 * time.Minute
	return t
}

// SetCertificateChooser hands the server the function its TLS listener picks
// a certificate with, so the pairing routes can name the leaf a connection
// was served. Nil (a loopback-only server, which speaks no TLS) leaves them
// refusing to pair: there is no network for a TV to be on.
func (s *Server) SetCertificateChooser(choose func(*tls.ClientHelloInfo) (*tls.Certificate, error)) {
	if choose == nil {
		s.servedLeaf = nil
		return
	}
	s.servedLeaf = func(r *http.Request) ([]byte, error) { return leafFor(r, choose) }
}

/*
 * leafFor asks the chooser again, with what decided the first answer: the name
 * the client asked for (the public certificate is chosen by name) and the
 * address it arrived on (loopback gets the generated one). Asked again rather
 * than recorded at the handshake because those are the only inputs the
 * listener's choice reads; the one difference is a renewal landing between
 * the handshake and this request, and that costs the TV a code that does not
 * match, so it starts again.
 */
func leafFor(r *http.Request, choose func(*tls.ClientHelloInfo) (*tls.Certificate, error)) ([]byte, error) {
	if r.TLS == nil {
		return nil, errors.New("not a TLS connection")
	}
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	c, err := choose(&tls.ClientHelloInfo{ServerName: r.TLS.ServerName, Conn: localConn{local: local}})
	if err != nil {
		return nil, err
	}
	if c == nil || len(c.Certificate) == 0 {
		return nil, errors.New("no certificate")
	}
	return c.Certificate[0], nil
}

// localConn is a connection that can say which address it arrived on, which
// is all a certificate chooser asks of one. Anything else would panic, and
// nothing calls anything else.
type localConn struct {
	net.Conn
	local net.Addr
}

func (c localConn) LocalAddr() net.Addr { return c.local }

// leaf is the certificate this request's connection was served, or a refusal
// written for the caller.
func (s *Server) leaf(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if s.servedLeaf == nil {
		writeError(w, http.StatusConflict, "conflict", "pairing needs the server to be reachable over HTTPS")
		return nil, false
	}
	der, err := s.servedLeaf(r)
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", "pairing needs an HTTPS connection")
		return nil, false
	}
	return der, true
}

// pairCertificate is the leaf this connection was served, as PEM: what the
// TV hashes into its code and then pins.
func (s *Server) pairCertificate(w http.ResponseWriter, r *http.Request) {
	der, ok := s.leaf(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Cache-Control", "no-store")
	_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// pairStart files a pairing for a TV.
func (s *Server) pairStart(w http.ResponseWriter, r *http.Request) {
	if !s.pairStarts.Allow(auth.ClientKey(r)) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many pairings from this address; wait a few minutes")
		return
	}
	var body struct {
		Name   string `json:"name"`
		Device string `json:"device"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
		return
	}
	name := oneLine(body.Name, devicepair.MaxName)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"a name is required, so the person approving knows which device this is")
		return
	}
	device := oneLine(body.Device, 32)
	if device == "" {
		device = "TV"
	}
	der, ok := s.leaf(w, r)
	if !ok {
		return
	}
	st, err := s.pairing.Start(name, device, auth.ClientKey(r), der)
	if errors.Is(err, devicepair.ErrFull) {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"too many devices are waiting to pair; try again in a few minutes")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "start pairing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pairing":    st.Token,
		"nonce":      base64.StdEncoding.EncodeToString(st.Nonce),
		"expires_in": int(devicepair.Lifetime / time.Second),
		"interval":   int(devicepair.Interval / time.Second),
	})
}

// pairPoll answers a TV asking whether it has been approved: 202 until then,
// its key exactly once, and 410 for everything that has ended.
func (s *Server) pairPoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pairing string `json:"pairing"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.Pairing == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which pairing?")
		return
	}
	p, err := s.pairing.Poll(body.Pairing)
	switch {
	case errors.Is(err, devicepair.ErrTooSoon):
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "poll no faster than the interval")
		return
	case errors.Is(err, devicepair.ErrUnknown):
		writeError(w, http.StatusGone, "gone", "this pairing has ended; start again")
		return
	case err != nil:
		s.writeInternal(w, err, "poll pairing")
		return
	}
	if p.State == devicepair.Waiting {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "waiting"})
		return
	}

	u, err := s.st.UserByID(r.Context(), p.ApproverID)
	if err != nil {
		// The approver's account went between the approval and now.
		writeError(w, http.StatusGone, "gone", "this pairing has ended; start again")
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		s.pairing.Restore(body.Pairing, p)
		s.writeInternal(w, err, "generate api key")
		return
	}
	keyName := oneLine(p.Device+": "+p.Name, maxKeyName)
	key, err := s.st.CreateAPIKey(r.Context(), hash, u.ID, keyName)
	if err != nil {
		s.pairing.Restore(body.Pairing, p)
		s.writeInternal(w, err, "create api key")
		return
	}
	s.log.Info("device paired", "user", u.Name, "device", keyName, "addr", p.Addr)
	writeJSON(w, http.StatusOK, map[string]any{
		"key":    token,
		"key_id": key.ID,
		"user":   map[string]any{"id": u.ID, "name": u.Name},
	})
}

// pairPerson is the signed-in person on the approving side: a session, never
// a key, so a paired TV cannot pair another.
func (s *Server) pairPerson(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.keysManageable(w, r) {
		return "", false
	}
	sess, _ := sessionFromContext(r)
	return sess.UserID, true
}

// pairCode reads a typed code, counting the ones that match nothing.
func (s *Server) pairCode(w http.ResponseWriter, r *http.Request, typed string) (devicepair.Pairing, string, bool) {
	addr := auth.ClientKey(r)
	if s.pairGuesses.Blocked(addr) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many codes that match nothing; wait a few minutes")
		return devicepair.Pairing{}, "", false
	}
	code := devicepair.Normalize(typed)
	var p devicepair.Pairing
	err := devicepair.ErrUnknown
	if code != "" {
		p, err = s.pairing.Lookup(code)
	}
	if err != nil {
		s.pairGuesses.Allow(addr)
		writeError(w, http.StatusNotFound, "not_found",
			"no device is waiting with that code; check it against the TV")
		return devicepair.Pairing{}, "", false
	}
	return p, code, true
}

func pairingView(p devicepair.Pairing) map[string]any {
	return map[string]any{
		"code":       devicepair.Format(p.Code),
		"name":       p.Name,
		"device":     p.Device,
		"address":    p.Addr,
		"created_at": p.Created.Unix(),
		"expires_at": p.Expires.Unix(),
	}
}

// pairLookup shows the approver what they are about to approve.
func (s *Server) pairLookup(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.pairPerson(w, r); !ok {
		return
	}
	p, _, ok := s.pairCode(w, r, r.PathValue("code"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pairingView(p))
}

/*
 * pairApprove decides a code. Yes needs the account's password, because it
 * mints a key, and a key outlives every session (R01-4, ADR 0061 as amended
 * 2026-11-16): a session somebody left open must not be enough to give a TV
 * the account. No needs nothing; refusing a device is never the harm.
 */
func (s *Server) pairApprove(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pairPerson(w, r)
	if !ok {
		return
	}
	var body struct {
		Code     string `json:"code"`
		Approve  bool   `json:"approve"`
		Password string `json:"current_password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
		return
	}
	p, code, ok := s.pairCode(w, r, body.Code)
	if !ok {
		return
	}
	if body.Approve {
		if _, ok := s.confirmPassword(w, r, userID, body.Password,
			http.StatusForbidden, "forbidden", "that password is not right"); !ok {
			return
		}
	}
	p, err := s.pairing.Decide(code, body.Approve, userID)
	if err != nil {
		// Decided by somebody else between the lookup and now, or expired.
		writeError(w, http.StatusNotFound, "not_found", "that device is no longer waiting")
		return
	}
	if body.Approve {
		s.audit(r, "device.pair", "device", p.Name, "approved a device", pairingView(p))
	} else {
		s.audit(r, "device.deny", "device", p.Name, "refused a device", pairingView(p))
	}
	out := pairingView(p)
	out["approved"] = body.Approve
	writeJSON(w, http.StatusOK, out)
}

// oneLine trims a name, folds runs of whitespace (a newline in a key's name
// would make the list lie about where one name ends) and cuts it to max bytes
// without splitting a character.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
