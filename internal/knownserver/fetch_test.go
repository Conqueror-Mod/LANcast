package knownserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/certpin"
)

/*
 * Fetch is tested against a real handshake rather than a fake.
 *
 * What it has to get right is not arithmetic, it is that the bytes it hashes
 * are the ones a TLS peer actually presents. A mock would assert that this
 * package can read a struct it was handed, which is not the thing that could
 * be wrong.
 */
func TestFetchReadsTheKeyAServerPresents(t *testing.T) {
	srv, spki := tlsServer(t)
	defer srv.Close()

	got, err := Fetch(context.Background(), hostPort(t, srv.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Pin != spki {
		t.Errorf("pin = %q, want %q — the pin must be over the key this server presented", got.Pin, spki)
	}
	if got.Subject == "" {
		t.Error("no subject; the prompt has nothing to show beside the fingerprint")
	}
	if got.NotAfter.IsZero() {
		t.Error("no expiry read off the certificate")
	}
}

// A self-signed certificate must be readable. That is the whole first-contact
// case: it fails verification by construction, and refusing it here would mean
// no LANcast server could ever be added.
func TestFetchDoesNotRequireAVerifiableCertificate(t *testing.T) {
	srv, _ := tlsServer(t)
	defer srv.Close()

	if _, err := Fetch(context.Background(), hostPort(t, srv.URL)); err != nil {
		t.Fatalf("Fetch refused a self-signed certificate: %v", err)
	}
}

// The same server, asked twice, gives the same pin — otherwise nothing stored
// could ever match.
func TestFetchIsStable(t *testing.T) {
	srv, _ := tlsServer(t)
	defer srv.Close()
	addr := hostPort(t, srv.URL)

	first, err := Fetch(context.Background(), addr)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	second, err := Fetch(context.Background(), addr)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if first.Pin != second.Pin {
		t.Errorf("two reads of one server gave %q and %q", first.Pin, second.Pin)
	}
}

// Two different servers must not collide, which is the property that makes a
// mismatch mean anything at all.
func TestTwoServersHaveDifferentPins(t *testing.T) {
	a, _ := tlsServer(t)
	defer a.Close()
	b, _ := tlsServer(t)
	defer b.Close()

	pa, err := Fetch(context.Background(), hostPort(t, a.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	pb, err := Fetch(context.Background(), hostPort(t, b.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if pa.Pin == pb.Pin {
		t.Error("two independently generated servers share a pin")
	}
}

// End to end, in the terms the client uses: fetch, accept, and the next
// connection to the same server connects without asking again.
func TestAcceptedServerIsTrustedNextTime(t *testing.T) {
	srv, _ := tlsServer(t)
	defer srv.Close()
	addr, err := ParseAddress(srv.URL)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", srv.URL, err)
	}

	offered, err := Fetch(context.Background(), addr)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := (List{}).Check(addr, offered.Pin); got != TrustUnknown {
		t.Fatalf("first contact = %v, want unknown", got)
	}

	l := List{}.Accept(Server{Address: addr, Name: "test", Pin: offered.Pin})

	again, err := Fetch(context.Background(), addr)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := l.Check(addr, again.Pin); got != TrustMatch {
		t.Errorf("second contact = %v, want match", got)
	}
}

// A server that is not there is reported, not waited on for ever.
func TestFetchReportsAnAddressThatIsNotThere(t *testing.T) {
	// Port 1 on loopback: nothing listens there and the refusal is immediate.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Fetch(ctx, "127.0.0.1:1"); err == nil {
		t.Fatal("Fetch succeeded against nothing")
	}
}

func TestFetchRefusesAnEmptyAddress(t *testing.T) {
	if _, err := Fetch(context.Background(), ""); err == nil {
		t.Fatal("Fetch succeeded with no address")
	}
}

/*
 * The pin read off the wire is the pin read off disk.
 *
 * This is the load-bearing property of the whole feature and it spans two
 * packages, so neither one can state it alone. The client trusts a *local*
 * server by reading its certificate off the filesystem, and a *remote* one by
 * reading it off a TLS connection. If those two routes ever produced different
 * strings for the same server, the symptom would not be an error: it would be
 * a server that is trusted when opened one way and refused when opened the
 * other, and the refusal would look exactly like the attack the pin exists to
 * catch.
 *
 * Proven here against a server whose certificate is on disk *and* being
 * served, which is the only arrangement in which the question can be asked.
 */
func TestTheWirePinAndTheDiskPinAgree(t *testing.T) {
	srv, wantPin := tlsServer(t)
	defer srv.Close()

	// Write the same certificate where a server would keep it.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tls"), 0o755); err != nil {
		t.Fatalf("make tls dir: %v", err)
	}
	leaf := srv.TLS.Certificates[0].Certificate[0]
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf})
	if err := os.WriteFile(certpin.CertPath(dir), pemBytes, 0o644); err != nil {
		t.Fatalf("write certificate: %v", err)
	}

	fromDisk, err := certpin.SPKI(dir)
	if err != nil {
		t.Fatalf("certpin.SPKI: %v", err)
	}
	fromWire, err := Fetch(context.Background(), hostPort(t, srv.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if fromDisk != fromWire.Pin {
		t.Fatalf("disk pin %q, wire pin %q — the two trust routes disagree about one server",
			fromDisk, fromWire.Pin)
	}
	if fromDisk != wantPin {
		t.Errorf("pin = %q, want %q", fromDisk, wantPin)
	}
}

// hostPort turns a test server's URL into the host:port Fetch takes.
func hostPort(t *testing.T, raw string) string {
	t.Helper()
	addr, err := ParseAddress(raw)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", raw, err)
	}
	return addr
}

/*
 * tlsServer is a TLS listener with its own freshly generated key, and the pin
 * that key should produce.
 *
 * Its own key rather than httptest's shared one: two servers in the same test
 * must differ, which is what TestTwoServersHaveDifferentPins is for.
 */
func tlsServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "LANcast", Organization: []string{"LANcast"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}},
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()

	return srv, certpin.SPKIFromDER(leaf.RawSubjectPublicKeyInfo)
}
