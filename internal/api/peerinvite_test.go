package api

import (
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"lancast/internal/config"
	"lancast/internal/peer"
	"lancast/internal/publiccert"
	"lancast/internal/tlscert"
)

type inviteBody struct {
	Invite string   `json:"invite"`
	Addrs  []string `json:"addrs"`
}

// withPublicName turns the public certificate on in settings and hands the
// server a manager reporting cert, so publicName answers as it would live.
func withPublicName(t *testing.T, h *harness, cert publiccert.Status) {
	t.Helper()
	st := h.settings.Get()
	st.PublicCert = config.PublicCert{Enabled: true, Hostname: pkRP, CA: "letsencrypt"}
	if err := h.settings.Set(st); err != nil {
		t.Fatal(err)
	}
	h.srvAPI.publicCert = certStatus{cert}
}

/*
 * The fault this exists for: a server behind a home router holds only a
 * private IPv4, so an invite built from its interfaces gave a friend on an
 * IPv4-only network nothing to dial. With a public name the invite leads with
 * it, on the port the CA validated it on — and does so even on a machine with
 * no other address, because the name is reachable whatever the interfaces say.
 */
func TestOurInviteLeadsWithThePublicName(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	withPublicName(t, h, publiccert.Status{State: publiccert.StateActive, NotAfter: time.Now().Add(24 * time.Hour)})

	var got inviteBody
	decode(t, h.authed(t, "GET", "/api/peers/invite", nil), &got)

	want := pkRP + ":443"
	if len(got.Addrs) == 0 || got.Addrs[0] != want {
		t.Fatalf("addrs = %v, want %s first", got.Addrs, want)
	}
	parsed, err := peer.Parse(got.Invite)
	if err != nil {
		t.Fatalf("the invite does not parse: %v", err)
	}
	if len(parsed.Addrs) == 0 || parsed.Addrs[0] != want {
		t.Errorf("the invite itself carries %v, want %s first", parsed.Addrs, want)
	}
	if !reflect.DeepEqual(parsed.Addrs, got.Addrs) {
		t.Errorf("the route reports %v and the invite carries %v", got.Addrs, parsed.Addrs)
	}
}

// No valid public certificate, no name: the invite is the interfaces and
// nothing else, exactly as before. An expired certificate is the case that
// matters — the setting is still on, and the name may no longer answer.
func TestOurInviteWithoutAPublicNameIsUnchanged(t *testing.T) {
	for name, cert := range map[string]PublicCertManager{
		"no manager":         nil,
		"expired":            certStatus{publiccert.Status{State: publiccert.StateFailing, NotAfter: time.Now().Add(-time.Hour)}},
		"never issued (yet)": certStatus{publiccert.Status{State: publiccert.StatePending}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.secure(t, "a good long password")
			withPublicName(t, h, publiccert.Status{})
			h.srvAPI.publicCert = cert

			_, port, err := net.SplitHostPort(h.srvAPI.listenAddr)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, ip := range tlscert.StableIPs() {
				want = append(want, net.JoinHostPort(ip, port))
			}

			resp := h.authed(t, "GET", "/api/peers/invite", nil)
			if len(want) == 0 {
				resp.Body.Close()
				if resp.StatusCode != http.StatusConflict {
					t.Fatalf("status %d with no address and no name, want 409", resp.StatusCode)
				}
				return
			}
			var got inviteBody
			decode(t, resp, &got)
			if !reflect.DeepEqual(got.Addrs, want) {
				t.Errorf("addrs = %v, want the interfaces alone: %v", got.Addrs, want)
			}
		})
	}
}
