package api

import (
	"net/http"
	"testing"

	"lancast/internal/publiccert"
)

type fakePublicCert struct {
	configured []publiccert.Config
}

func (f *fakePublicCert) Configure(c publiccert.Config) { f.configured = append(f.configured, c) }
func (f *fakePublicCert) Status() publiccert.Status {
	return publiccert.Status{State: publiccert.StatePending}
}

func turnOn(extra map[string]any) map[string]any {
	body := map[string]any{"enabled": true, "hostname": "Example.DuckDNS.org",
		"ca": "letsencrypt-staging", "agree_terms": true}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// A loopback-only server has no HTTPS, so the feature says it is unavailable
// rather than accepting a choice it cannot act on.
func TestPublicCertificateUnavailableWithoutTLS(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/settings/public-certificate", nil), &got)
	if got["available"] != false {
		t.Fatalf("available = %v on a server with no TLS", got["available"])
	}
	resp := h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(nil))
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409", resp.StatusCode)
	}
}

func TestTurningThePublicCertificateOn(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	fake := &fakePublicCert{}
	h.srvAPI.SetPublicCert(fake)

	var got map[string]any
	decode(t, h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(nil)), &got)
	if got["enabled"] != true || got["hostname"] != "example.duckdns.org" {
		t.Fatalf("response %v; the name must be stored normalized", got)
	}
	if got["agreed_at"] == nil {
		t.Fatal("the agreement was not recorded")
	}
	if len(fake.configured) != 1 || !fake.configured[0].Enabled ||
		fake.configured[0].Hostname != "example.duckdns.org" {
		t.Fatalf("manager was configured with %+v", fake.configured)
	}
	st := h.settings.Get().PublicCert
	if !st.Enabled || st.CA != "letsencrypt-staging" || st.Email != "" {
		t.Fatalf("stored %+v; no email unless one is given", st)
	}
}

// LANcast does not agree to a CA's terms on anybody's behalf.
func TestThePublicCertificateNeedsTheTermsAgreed(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	fake := &fakePublicCert{}
	h.srvAPI.SetPublicCert(fake)

	resp := h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(map[string]any{"agree_terms": false}))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || len(fake.configured) != 0 {
		t.Fatalf("status %d, configured %d times; turning it on without agreement must be refused",
			resp.StatusCode, len(fake.configured))
	}

	// Once agreed for this CA, saving again ("try now") needs no new tick...
	h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(nil)).Body.Close()
	resp = h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(map[string]any{"agree_terms": false}))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d; saving again for the same CA must not ask again", resp.StatusCode)
	}
	// ...but a different CA is a different agreement.
	resp = h.authed(t, "PUT", "/api/settings/public-certificate",
		turnOn(map[string]any{"agree_terms": false, "ca": "letsencrypt"}))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d; switching CA must ask for its terms", resp.StatusCode)
	}
}

func TestThePublicCertificateRefusesBadChoices(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	h.srvAPI.SetPublicCert(&fakePublicCert{})

	for name, extra := range map[string]map[string]any{
		"an IP address":     {"hostname": "203.0.113.5"},
		"a URL":             {"hostname": "https://example.duckdns.org"},
		"an unknown CA":     {"ca": "somebody-else"},
		"a directory URL":   {"ca": "https://evil.example/directory"},
		"a malformed email": {"email": "not an email"},
	} {
		resp := h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(extra))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, resp.StatusCode)
		}
	}
}

// Off keeps the name for next time but not the agreement.
func TestTurningThePublicCertificateOff(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	fake := &fakePublicCert{}
	h.srvAPI.SetPublicCert(fake)

	h.authed(t, "PUT", "/api/settings/public-certificate", turnOn(nil)).Body.Close()
	h.authed(t, "PUT", "/api/settings/public-certificate", map[string]any{"enabled": false}).Body.Close()

	st := h.settings.Get().PublicCert
	if st.Enabled || st.AgreedAt != 0 || st.Hostname != "example.duckdns.org" {
		t.Fatalf("stored %+v after turning off", st)
	}
	if last := fake.configured[len(fake.configured)-1]; last.Enabled {
		t.Fatal("the manager was not told to turn off")
	}
}

// The directory URL is file-only (ADR 0081 §1): nothing the API accepts can
// set it, and it is never reported.
func TestTheACMEDirectoryIsNotReachableFromTheAPI(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	fake := &fakePublicCert{}
	h.srvAPI.SetPublicCert(fake)

	var got map[string]any
	decode(t, h.authed(t, "PUT", "/api/settings/public-certificate",
		turnOn(map[string]any{"directory_url": "https://evil.example/directory",
			"acme_directory_url": "https://evil.example/directory"})), &got)
	if h.settings.Get().ACMEDirectoryURL != "" || fake.configured[0].DirectoryURL != "" {
		t.Fatal("a directory URL was accepted through the API")
	}
	if _, ok := got["acme_directory_url"]; ok {
		t.Fatal("the directory URL is reported by the API")
	}
}

// Members cannot see or change it: it decides what the server contacts.
func TestThePublicCertificateIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	h.srvAPI.SetPublicCert(&fakePublicCert{})
	member := h.member(t, "kid", "another long password")
	for _, m := range []string{"GET", "PUT"} {
		resp := h.asUser(t, member, m, "/api/settings/public-certificate", turnOn(nil))
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s by a member: status %d, want 403", m, resp.StatusCode)
		}
	}
}
