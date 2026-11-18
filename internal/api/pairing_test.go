package api

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lancast/internal/devicepair"
)

/*
 * Pairing a TV by code (ADR 0125 §1–2).
 *
 * The harness speaks plain HTTP, so the certificate a connection "was served"
 * is set directly; leafFor, which finds it on a real TLS connection, has a test
 * of its own below.
 */

var (
	servedDER   = []byte("the certificate this server serves")
	impostorDER = []byte("a certificate somebody else serves")
)

type pairClock struct{ t time.Time }

func (c *pairClock) now() time.Time { return c.t }

func pairingHarness(t *testing.T) (*harness, *pairClock) {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a long enough password")
	h.srvAPI.servedLeaf = func(*http.Request) ([]byte, error) { return servedDER, nil }
	c := &pairClock{t: time.Now()}
	h.srvAPI.pairing.SetClock(c.now)
	return h, c
}

type started struct {
	Pairing   string `json:"pairing"`
	Nonce     string `json:"nonce"`
	ExpiresIn int    `json:"expires_in"`
	Interval  int    `json:"interval"`
}

// startPairing is the TV asking to pair, and the code it would then show
// having been served cert.
func startPairing(t *testing.T, h *harness, cert []byte) (started, string) {
	t.Helper()
	resp := h.request(t, "POST", "/api/devices/pair", "", "", false,
		map[string]any{"name": "Living room", "device": "Roku"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("start: %d %s", resp.StatusCode, b)
	}
	var s started
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	nonce, err := base64.StdEncoding.DecodeString(s.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	return s, devicepair.Format(devicepair.Code(nonce, cert))
}

func poll(t *testing.T, h *harness, c *pairClock, token string) *http.Response {
	t.Helper()
	c.t = c.t.Add(devicepair.Interval)
	return h.request(t, "POST", "/api/devices/pair/poll", "", "", false, map[string]any{"pairing": token})
}

func approve(t *testing.T, h *harness, code, password string) *http.Response {
	t.Helper()
	return h.authed(t, "POST", "/api/devices/pair/approve",
		map[string]any{"code": code, "approve": true, "current_password": password})
}

// The whole flow, and the key it ends in is an ordinary key of the approver's.
func TestATVPairsAndCollectsItsKeyOnce(t *testing.T) {
	h, c := pairingHarness(t)
	s, code := startPairing(t, h, servedDER)
	if s.ExpiresIn != 600 || s.Interval != 5 {
		t.Errorf("expires_in, interval = %d, %d; want 600, 5", s.ExpiresIn, s.Interval)
	}

	resp := poll(t, h, c, s.Pairing)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("poll before approval = %d, want 202", resp.StatusCode)
	}

	resp = h.authed(t, "GET", "/api/devices/pair/"+code, nil)
	var view struct {
		Name, Device, Address string
	}
	_ = json.NewDecoder(resp.Body).Decode(&view)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || view.Name != "Living room" || view.Device != "Roku" || view.Address == "" {
		t.Fatalf("lookup = %d %+v", resp.StatusCode, view)
	}

	resp = approve(t, h, code, h.password)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d, want 200", resp.StatusCode)
	}

	resp = poll(t, h, c, s.Pairing)
	var got struct {
		Key   string `json:"key"`
		KeyID string `json:"key_id"`
		User  struct{ Name string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || got.Key == "" || got.User.Name == "" {
		t.Fatalf("poll after approval = %d %+v", resp.StatusCode, got)
	}

	// The key works, and is listed under the device's name.
	resp = h.doKey(t, "GET", "/api/libraries", got.Key, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the paired key did not authenticate: %d", resp.StatusCode)
	}
	u, err := h.st.UserByName(context.Background(), got.User.Name)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := h.st.ListAPIKeys(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Name != "Roku: Living room" {
		t.Fatalf("keys = %+v, want one named \"Roku: Living room\"", keys)
	}

	// Exactly once.
	resp = poll(t, h, c, s.Pairing)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("second collection = %d, want 410", resp.StatusCode)
	}
}

/*
 * The defence in §2: a TV served somebody else's certificate shows a code the
 * server never filed, so the person typing it gets "no such device" and
 * nothing is approved.
 */
func TestACodeFromAnImpostorsCertificateMatchesNothing(t *testing.T) {
	h, c := pairingHarness(t)
	s, code := startPairing(t, h, impostorDER)

	resp := approve(t, h, code, h.password)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("approving the impostor's code = %d, want 404", resp.StatusCode)
	}
	resp = poll(t, h, c, s.Pairing)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("the real pairing = %d, want still waiting", resp.StatusCode)
	}
}

// Approving mints a key, and a key outlives every session (ADR 0061 as
// amended): an open session alone must not be enough.
func TestApprovingNeedsThePassword(t *testing.T) {
	h, c := pairingHarness(t)
	s, code := startPairing(t, h, servedDER)

	for _, pw := range []string{"", "not the password"} {
		resp := approve(t, h, code, pw)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("approve with %q = %d, want 403", pw, resp.StatusCode)
		}
	}
	resp := poll(t, h, c, s.Pairing)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("after wrong passwords the pairing = %d, want still waiting", resp.StatusCode)
	}
}

// A paired TV holds a key, and a key must not pair another device: it could
// then outlive its own revocation.
func TestAKeyCannotApproveAPairing(t *testing.T) {
	h, _ := pairingHarness(t)
	secret, _ := mint(t, h, "script")
	_, code := startPairing(t, h, servedDER)

	resp := h.doKey(t, "POST", "/api/devices/pair/approve",
		secret, map[string]any{"code": code, "approve": true, "current_password": h.password})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("approve with a key = %d, want 403", resp.StatusCode)
	}
	resp = h.doKey(t, "GET", "/api/devices/pair/"+code, secret, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("lookup with a key = %d, want 403", resp.StatusCode)
	}
}

func TestApprovingNeedsASession(t *testing.T) {
	h, _ := pairingHarness(t)
	_, code := startPairing(t, h, servedDER)
	resp := h.request(t, "POST", "/api/devices/pair/approve", "", "", false,
		map[string]any{"code": code, "approve": true, "current_password": h.password})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("approve with no session = %d, want 401", resp.StatusCode)
	}
}

func TestADeniedTVIsToldSo(t *testing.T) {
	h, c := pairingHarness(t)
	s, code := startPairing(t, h, servedDER)
	resp := h.authed(t, "POST", "/api/devices/pair/approve", map[string]any{"code": code, "approve": false})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deny = %d, want 200 (no password needed to refuse)", resp.StatusCode)
	}
	resp = poll(t, h, c, s.Pairing)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("poll after denial = %d, want 410", resp.StatusCode)
	}
}

func TestPollingTooFastIsRefused(t *testing.T) {
	h, c := pairingHarness(t)
	s, _ := startPairing(t, h, servedDER)
	resp := poll(t, h, c, s.Pairing)
	resp.Body.Close()
	resp = h.request(t, "POST", "/api/devices/pair/poll", "", "", false, map[string]any{"pairing": s.Pairing})
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("an immediate second poll = %d, want 429", resp.StatusCode)
	}
}

// The route needs no session, so it is the cheapest thing to flood.
func TestStartingIsRateLimitedPerAddress(t *testing.T) {
	h, _ := pairingHarness(t)
	last := 0
	for i := 0; i < 11; i++ {
		resp := h.request(t, "POST", "/api/devices/pair", "", "", false, map[string]any{"name": "TV"})
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("the 11th start = %d, want 429", last)
	}
}

func TestCodesThatMatchNothingAreRateLimited(t *testing.T) {
	h, _ := pairingHarness(t)
	last := 0
	for i := 0; i < 11; i++ {
		resp := h.authed(t, "GET", "/api/devices/pair/ABCD-EFGH", nil)
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("the 11th unknown code = %d, want 429", last)
	}
}

// No TLS, no pairing: a loopback-only server has no network for a TV to be on.
func TestPairingNeedsHTTPS(t *testing.T) {
	h, _ := pairingHarness(t)
	h.srvAPI.servedLeaf = nil
	resp := h.request(t, "POST", "/api/devices/pair", "", "", false, map[string]any{"name": "TV"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("start without TLS = %d, want 409", resp.StatusCode)
	}
}

func TestTheCertificateRouteServesTheLeaf(t *testing.T) {
	h, _ := pairingHarness(t)
	resp := h.request(t, "GET", "/api/devices/pair/certificate", "", "", false, nil)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	blk, _ := pem.Decode(b)
	if resp.StatusCode != http.StatusOK || blk == nil || string(blk.Bytes) != string(servedDER) {
		t.Fatalf("certificate = %d %q", resp.StatusCode, b)
	}
}

// leafFor asks the listener's chooser with what the handshake gave it: the
// name asked for, and the address the connection arrived on.
func TestLeafForAsksTheChooserWithTheConnectionsInputs(t *testing.T) {
	var gotName string
	var gotLocal net.Addr
	choose := func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		gotName, gotLocal = hello.ServerName, hello.Conn.LocalAddr()
		return &tls.Certificate{Certificate: [][]byte{servedDER}}, nil
	}
	r := httptest.NewRequest("GET", "/api/devices/pair/certificate", nil)
	r.TLS = &tls.ConnectionState{ServerName: "media.example"}
	local := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8080}
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))

	der, err := leafFor(r, choose)
	if err != nil || string(der) != string(servedDER) {
		t.Fatalf("leafFor = %q, %v", der, err)
	}
	if gotName != "media.example" || gotLocal != local {
		t.Fatalf("chooser saw %q, %v", gotName, gotLocal)
	}

	r.TLS = nil
	if _, err := leafFor(r, choose); err == nil {
		t.Fatal("leafFor answered for a plain HTTP request")
	}
}
