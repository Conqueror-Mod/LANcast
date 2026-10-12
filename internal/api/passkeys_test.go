package api

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"lancast/internal/auth"
	"lancast/internal/config"
	"lancast/internal/publiccert"
)

const (
	pkRP     = "example.dynv6.net"
	pkOrigin = "https://example.dynv6.net"
)

// enablePasskeys stands in for "this request arrived at the public name over
// its trusted certificate", which the plain-HTTP test server cannot be.
func enablePasskeys(h *harness) {
	h.srvAPI.passkeyRPFor = func(*http.Request) (string, bool) { return pkRP, true }
}

/*
 * fakeKey is a browser and a platform authenticator in one: it answers the
 * options the server hands out the way navigator.credentials does, and signs
 * with a real P-256 key. The server under test verifies with no idea it is
 * not talking to a phone.
 */
type fakeKey struct {
	key    *ecdsa.PrivateKey
	credID []byte
	count  uint32
	origin string
}

func newFakeKey(t *testing.T) *fakeKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &fakeKey{key: k, credID: id, origin: pkOrigin}
}

func (f *fakeKey) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": f.origin})
	return b
}

func (f *fakeKey) authData(attested []byte) []byte {
	h := sha256.Sum256([]byte(pkRP))
	flags := byte(0x01 | 0x04) // present, verified
	if attested != nil {
		flags |= 0x40
	}
	b := append(append([]byte{}, h[:]...), flags)
	b = binary.BigEndian.AppendUint32(b, f.count)
	return append(b, attested...)
}

// register answers registration options.
func (f *fakeKey) register(t *testing.T, options map[string]any, name string) map[string]any {
	t.Helper()
	pk := options["public_key"].(map[string]any)
	pub, _ := f.key.PublicKey.Bytes()
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pub[1:33], -3: pub[33:]})
	attested := make([]byte, 16)
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(f.credID)))
	attested = append(append(attested, f.credID...), cose...)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": f.authData(attested)})
	return map[string]any{
		"challenge_id":       options["challenge_id"],
		"name":               name,
		"client_data_json":   b64.EncodeToString(f.clientData("webauthn.create", pk["challenge"].(string))),
		"attestation_object": b64.EncodeToString(att),
	}
}

// signIn answers sign-in options as the passkey belonging to userID.
func (f *fakeKey) signIn(t *testing.T, options map[string]any, userID string) map[string]any {
	t.Helper()
	f.count++
	pk := options["public_key"].(map[string]any)
	cd := f.clientData("webauthn.get", pk["challenge"].(string))
	ad := f.authData(nil)
	h := sha256.Sum256(cd)
	d := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := f.key.Sign(rand.Reader, d[:], crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"challenge_id":       options["challenge_id"],
		"id":                 b64.EncodeToString(f.credID),
		"client_data_json":   b64.EncodeToString(cd),
		"authenticator_data": b64.EncodeToString(ad),
		"signature":          b64.EncodeToString(sig),
		"user_handle":        b64.EncodeToString([]byte(userID)),
	}
}

const pkPassword = "a long enough password"

// addPasskey runs the whole registration as the harness's signed-in admin.
func addPasskey(t *testing.T, h *harness, f *fakeKey, name string) map[string]any {
	t.Helper()
	var opts map[string]any
	resp := h.authed(t, "POST", "/api/profile/passkeys/options", map[string]any{"current_password": pkPassword})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registration options: %d", resp.StatusCode)
	}
	decode(t, resp, &opts)
	var created map[string]any
	resp = h.authed(t, "POST", "/api/profile/passkeys", f.register(t, opts, name))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("registration: %d", resp.StatusCode)
	}
	decode(t, resp, &created)
	return created
}

func loginOptions(t *testing.T, h *harness) map[string]any {
	t.Helper()
	var opts map[string]any
	resp := h.do(t, "POST", "/api/auth/passkey/login/options", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login options: %d", resp.StatusCode)
	}
	decode(t, resp, &opts)
	return opts
}

func adminID(t *testing.T, h *harness) string {
	t.Helper()
	var st struct {
		User struct{ ID string } `json:"user"`
	}
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	return st.User.ID
}

// Off the public name, passkeys say so and every ceremony route refuses.
func TestPasskeysUnavailableOffThePublicName(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)

	var st map[string]any
	decode(t, h.do(t, "GET", "/api/auth/status", nil), &st)
	if pk := st["passkeys"].(map[string]any); pk["available"] != false {
		t.Fatalf("passkeys = %v on a server with no public name", pk)
	}
	for _, path := range []string{"/api/auth/passkey/login/options", "/api/auth/passkey/login"} {
		resp := h.do(t, "POST", path, map[string]any{})
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("%s: %d, want 409", path, resp.StatusCode)
		}
	}
	resp := h.authed(t, "POST", "/api/profile/passkeys/options", map[string]any{"current_password": pkPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("registration options: %d, want 409", resp.StatusCode)
	}
}

// The whole thing, end to end: add a passkey, sign in with it, get a session
// that works.
func TestAPasskeySignsIn(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)
	enablePasskeys(h)

	var st map[string]any
	decode(t, h.do(t, "GET", "/api/auth/status", nil), &st)
	if pk := st["passkeys"].(map[string]any); pk["available"] != true || pk["rp_id"] != pkRP {
		t.Fatalf("passkeys = %v", pk)
	}

	f := newFakeKey(t)
	created := addPasskey(t, h, f, "  Phone  ")
	// The name is trimmed, and a passkey just made at this name is usable here.
	if created["name"] != "Phone" || created["usable"] != true || created["rp_id"] != pkRP {
		t.Fatalf("created: %v", created)
	}
	uid := adminID(t, h)

	resp := h.do(t, "POST", "/api/auth/passkey/login", f.signIn(t, loginOptions(t, h), uid))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("passkey sign-in: %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("passkey sign-in issued no session")
	}
	saved := h.cookie
	h.cookie = cookie
	if got := adminID(t, h); got != uid {
		t.Fatalf("the new session belongs to %q, want %q", got, uid)
	}
	h.cookie = saved
}

// Adding a passkey asks for the password, as changing the password does: an
// unattended session is not enough to add a way in.
func TestAddingAPasskeyNeedsThePassword(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)
	enablePasskeys(h)
	resp := h.authed(t, "POST", "/api/profile/passkeys/options", map[string]any{"current_password": "wrong"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

// A registration challenge belongs to the account that asked for it.
func TestAnotherSessionCannotSpendARegistrationChallenge(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)
	enablePasskeys(h)
	m := h.member(t, "kid", "another long password")

	var opts map[string]any
	decode(t, h.authed(t, "POST", "/api/profile/passkeys/options", map[string]any{"current_password": pkPassword}), &opts)
	f := newFakeKey(t)
	resp := h.asUser(t, m, "POST", "/api/profile/passkeys", f.register(t, opts, "stolen"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d; a member completed the admin's registration", resp.StatusCode)
	}
}

func TestPasskeySignInRefusals(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)
	enablePasskeys(h)
	f := newFakeKey(t)
	addPasskey(t, h, f, "Phone")
	uid := adminID(t, h)

	try := func(why string, body map[string]any) {
		t.Helper()
		resp := h.do(t, "POST", "/api/auth/passkey/login", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", why, resp.StatusCode)
		}
	}

	body := f.signIn(t, loginOptions(t, h), uid)
	if resp := h.do(t, "POST", "/api/auth/passkey/login", body); resp.StatusCode != http.StatusOK {
		t.Fatalf("control sign-in: %d", resp.StatusCode)
	}
	try("a replayed response (its challenge is spent)", body)

	body = f.signIn(t, loginOptions(t, h), "u_someone_else")
	try("a user handle naming another account", body)

	phish := *f
	phish.origin = "https://lancast-login.example"
	try("a phishing origin", phish.signIn(t, loginOptions(t, h), uid))

	stranger := newFakeKey(t)
	try("a passkey this server never registered", stranger.signIn(t, loginOptions(t, h), uid))

	// The counter went backwards: the sign of a cloned authenticator.
	f.count = 0
	try("a counter that did not increase", f.signIn(t, loginOptions(t, h), uid))
}

// Rename and remove are the owner's alone, and a removed passkey signs in no
// more.
func TestManagingPasskeys(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)
	enablePasskeys(h)
	f := newFakeKey(t)
	created := addPasskey(t, h, f, "Phone")
	id := created["id"].(string)
	uid := adminID(t, h)

	m := h.member(t, "kid", "another long password")
	for _, method := range []string{"PATCH", "DELETE"} {
		resp := h.asUser(t, m, method, "/api/profile/passkeys/"+id, map[string]any{"name": "mine"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s by another account: %d, want 404", method, resp.StatusCode)
		}
	}

	resp := h.authed(t, "PATCH", "/api/profile/passkeys/"+id, map[string]any{"name": "Pixel"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("rename: %d", resp.StatusCode)
	}
	var list struct {
		Passkeys []map[string]any `json:"passkeys"`
	}
	decode(t, h.authed(t, "GET", "/api/profile/passkeys", nil), &list)
	if len(list.Passkeys) != 1 || list.Passkeys[0]["name"] != "Pixel" {
		t.Fatalf("list: %v", list.Passkeys)
	}

	resp = h.authed(t, "DELETE", "/api/profile/passkeys/"+id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	resp = h.do(t, "POST", "/api/auth/passkey/login", f.signIn(t, loginOptions(t, h), uid))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a removed passkey signed in: %d", resp.StatusCode)
	}
}

// The same credential cannot be registered twice.
func TestAPasskeyCannotBeAddedTwice(t *testing.T) {
	h := newHarness(t)
	h.secure(t, pkPassword)
	enablePasskeys(h)
	f := newFakeKey(t)
	addPasskey(t, h, f, "Phone")

	var opts map[string]any
	decode(t, h.authed(t, "POST", "/api/profile/passkeys/options", map[string]any{"current_password": pkPassword}), &opts)
	excl := opts["public_key"].(map[string]any)["excludeCredentials"].([]any)
	if len(excl) != 1 {
		t.Fatalf("excludeCredentials %v; the browser should be told which passkey exists", excl)
	}
	resp := h.authed(t, "POST", "/api/profile/passkeys", f.register(t, opts, "again"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409", resp.StatusCode)
	}
}

type certStatus struct{ st publiccert.Status }

func (c certStatus) Configure(publiccert.Config) {}
func (c certStatus) Status() publiccert.Status   { return c.st }

// passkeyRP is the gate everything above stands in for: only TLS, at the
// public name, while its certificate is valid.
func TestPasskeysExistOnlyAtTheNameWithAValidCertificate(t *testing.T) {
	h := newHarness(t)
	pc := h.settings.Get()
	pc.PublicCert = config.PublicCert{Enabled: true, Hostname: pkRP, CA: "letsencrypt"}
	if err := h.settings.Set(pc); err != nil {
		t.Fatal(err)
	}
	valid := certStatus{publiccert.Status{State: publiccert.StateActive, NotAfter: time.Now().Add(24 * time.Hour)}}
	expired := certStatus{publiccert.Status{State: publiccert.StateFailing, NotAfter: time.Now().Add(-time.Hour)}}
	renewalFailing := certStatus{publiccert.Status{State: publiccert.StateFailing, NotAfter: time.Now().Add(time.Hour)}}

	for _, c := range []struct {
		name string
		host string
		tls  bool
		cert PublicCertManager
		want bool
	}{
		{"the name over TLS", pkRP, true, valid, true},
		{"the name with a port", pkRP + ":8080", true, valid, true},
		{"the name in capitals", "EXAMPLE.dynv6.net", true, valid, true},
		{"a failing renewal while the certificate is still valid", pkRP, true, renewalFailing, true},
		{"an IP address", "192.168.1.160:8080", true, valid, false},
		{"localhost", "localhost:8080", true, valid, false},
		{"another name", "evil.example", true, valid, false},
		{"plain HTTP", pkRP, false, valid, false},
		{"an expired certificate", pkRP, true, expired, false},
		{"no public certificate at all", pkRP, true, nil, false},
	} {
		h.srvAPI.publicCert = c.cert
		r := httptest.NewRequest("GET", "/api/auth/status", nil)
		r.Host = c.host
		if c.tls {
			r.TLS = &tls.ConnectionState{}
		}
		if _, got := h.srvAPI.passkeyRP(r); got != c.want {
			t.Errorf("%s: available=%v, want %v", c.name, got, c.want)
		}
	}

	h.srvAPI.publicCert = valid
	pc.PublicCert.Enabled = false
	h.settings.Set(pc)
	r := httptest.NewRequest("GET", "/", nil)
	r.Host, r.TLS = pkRP, &tls.ConnectionState{}
	if _, ok := h.srvAPI.passkeyRP(r); ok {
		t.Error("available with the public certificate turned off")
	}
}
