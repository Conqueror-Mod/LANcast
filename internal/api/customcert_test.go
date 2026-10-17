package api

import (
	"crypto/tls"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/tlscert"
)

/*
 * A certificate of the operator's own at the boundary (ADR 0088). What is
 * served for which connection is proven in tlscert; here it is who may set
 * it, that a bad pair is refused before it is saved, that a good one is
 * served at once, and that the Network pane's fingerprint follows it.
 */

// certPair writes a certificate and key for name, as a CA or an operator's
// own tooling would leave them, and returns their paths.
func certPair(t *testing.T, name string) (certFile, keyFile string) {
	t.Helper()
	dir := t.TempDir()
	if _, err := tlscert.LoadOrGenerate(dir, []string{name}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

func servingHarness(t *testing.T) (*harness, *tlscert.Serving) {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a long enough password")
	gen, err := tlscert.LoadOrGenerate(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	serving := tlscert.NewServing(gen, nil)
	h.srvAPI.SetServingCert(serving)
	return h, serving
}

func TestACustomCertificateIsCheckedSavedAndServed(t *testing.T) {
	h, _ := servingHarness(t)

	var before customCertStatus
	decode(t, h.authed(t, "GET", "/api/settings/custom-certificate", nil), &before)
	if !before.Available || before.Serving != "generated" || !before.SelfSigned {
		t.Fatalf("before = %+v", before)
	}

	cf, kf := certPair(t, "media.home.example")
	// Pasted the way Windows' "Copy as path" leaves it.
	var got customCertStatus
	decode(t, h.authed(t, "PUT", "/api/settings/custom-certificate",
		map[string]any{"cert_file": `"` + cf + `"`, "key_file": kf}), &got)
	if got.Serving != "custom" || got.Error != "" || got.CertFile != cf {
		t.Fatalf("after = %+v", got)
	}
	found := false
	for _, n := range got.Names {
		found = found || n == "media.home.example"
	}
	if !found {
		t.Errorf("names = %v, want media.home.example", got.Names)
	}
	if st := h.settings.Get(); st.TLSCertFile != cf || st.TLSKeyFile != kf {
		t.Errorf("stored %q %q", st.TLSCertFile, st.TLSKeyFile)
	}

	// The Network pane answers about the certificate actually served.
	var net networkStatus
	decode(t, h.authed(t, "GET", "/api/network", nil), &net)
	if !net.CustomCertificate || net.CertificateFingerprint != got.Fingerprint {
		t.Errorf("network = custom %v, fingerprint %q; want custom, %q",
			net.CustomCertificate, net.CertificateFingerprint, got.Fingerprint)
	}

	// And back.
	decode(t, h.authed(t, "DELETE", "/api/settings/custom-certificate", nil), &got)
	if got.Serving != "generated" || got.CertFile != "" || h.settings.Get().CustomTLS() {
		t.Errorf("after DELETE = %+v", got)
	}
}

// A mistake is heard while the admin is looking at the form, and nothing is
// saved: a pair that does not load never becomes the configuration.
func TestABadPairIsRefusedBeforeItIsSaved(t *testing.T) {
	h, _ := servingHarness(t)
	certA, _ := certPair(t, "a.example")
	_, keyB := certPair(t, "b.example")
	cases := map[string]struct {
		body map[string]any
		want string
	}{
		"mismatched": {map[string]any{"cert_file": certA, "key_file": keyB}, "pair"},
		"missing":    {map[string]any{"cert_file": certA + ".gone", "key_file": keyB}, "can't be read"},
		"relative":   {map[string]any{"cert_file": "cert.pem", "key_file": keyB}, "full path"},
		"half":       {map[string]any{"cert_file": certA}, "both"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resp := h.authed(t, "PUT", "/api/settings/custom-certificate", c.body)
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			decode(t, resp, &e)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(e.Error.Message, c.want) {
				t.Errorf("%d %q, want 400 mentioning %q", resp.StatusCode, e.Error.Message, c.want)
			}
			if h.settings.Get().CustomTLS() {
				t.Error("a refused pair was saved")
			}
		})
	}
}

func TestOnlyAnAdministratorSetsTheCertificate(t *testing.T) {
	a := newAccessHarness(t)
	a.srvAPI.SetServingCert(tlscert.NewServing(mustGenerate(t), nil))
	cf, kf := certPair(t, "media.home.example")
	for _, m := range []string{"GET", "PUT", "DELETE"} {
		if code := status(t, a.doAs(t, a.member, m, "/api/settings/custom-certificate",
			map[string]any{"cert_file": cf, "key_file": kf})); code != http.StatusForbidden {
			t.Errorf("member %s = %d, want 403", m, code)
		}
	}
}

// A loopback-only server serves no HTTPS: the routes say so instead of
// accepting a choice nothing would act on.
func TestTheCustomCertificateIsUnavailableWithoutTLS(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	var got customCertStatus
	decode(t, h.authed(t, "GET", "/api/settings/custom-certificate", nil), &got)
	if got.Available {
		t.Fatalf("available on a server with no TLS: %+v", got)
	}
	cf, kf := certPair(t, "media.home.example")
	if code := status(t, h.authed(t, "PUT", "/api/settings/custom-certificate",
		map[string]any{"cert_file": cf, "key_file": kf})); code != http.StatusConflict {
		t.Errorf("PUT = %d, want 409", code)
	}
}

func mustGenerate(t *testing.T) tls.Certificate {
	t.Helper()
	c, err := tlscert.LoadOrGenerate(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
