package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * DLNA at the boundary (ADR 0086): a TV browsing as the chosen account sees
 * what that account sees and nothing more, and nobody off the LAN sees
 * anything. The control for each is the same request for an account with no
 * restriction.
 */

func (a accessHarness) dlna(t *testing.T, account string) http.Handler {
	t.Helper()
	return a.srvAPI.DLNAHandler(func() DLNAConfig {
		return DLNAConfig{UUID: "11111111-2222-5333-8444-555555555555", Name: "Living room", Account: account}
	})
}

func dlnaCall(h http.Handler, method, path, body, from string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = from
	req.Host = "192.168.1.20:8200"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func browseBody(objectID string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><ObjectID>` + objectID +
		`</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex>` +
		`<RequestedCount>0</RequestedCount></u:Browse></s:Body></s:Envelope>`
}

const tv = "192.168.1.50:51000"

func TestATVSeesWhatItsAccountSees(t *testing.T) {
	a := newAccessHarness(t)
	if code := status(t, a.hide(t, a.private)); code != http.StatusOK {
		t.Fatal("hide failed")
	}
	member := a.dlna(t, a.memberID(t))

	root := dlnaCall(member, "POST", "/dlna/control/cds", browseBody("0"), tv).Body.String()
	if strings.Contains(root, "Private") || !strings.Contains(root, "lib-") {
		t.Errorf("root as the member lists the hidden library or nothing:\n%s", root)
	}
	privateLib := fmt.Sprintf("lib-%d", a.private)
	if got := dlnaCall(member, "POST", "/dlna/control/cds", browseBody(privateLib), tv); got.Code != http.StatusInternalServerError ||
		!strings.Contains(got.Body.String(), "<errorCode>701</errorCode>") {
		t.Errorf("browsing the hidden library = %d %s, want a 701 fault", got.Code, got.Body.String())
	}
	if code := dlnaCall(member, "GET", fmt.Sprintf("/dlna/media/%d", a.privateFilm), "", tv).Code; code != http.StatusNotFound {
		t.Errorf("the hidden film's file = %d, want 404", code)
	}

	// The open film plays, with the headers a TV checks before seeking.
	got := dlnaCall(member, "GET", fmt.Sprintf("/dlna/media/%d", a.openFilm), "", tv)
	if got.Code != http.StatusOK || got.Header().Get("transferMode.dlna.org") != "Streaming" {
		t.Errorf("the open film = %d %v", got.Code, got.Header())
	}
	if b, _ := io.ReadAll(got.Body); string(b) != "x" {
		t.Errorf("the open film's bytes = %q", b)
	}

	// Control: a TV browsing as the administrator sees both libraries.
	var adminID string
	users, _ := a.st.ListUsers(t.Context())
	for _, u := range users {
		if u.Role == "admin" {
			adminID = u.ID
		}
	}
	all := dlnaCall(a.dlna(t, adminID), "POST", "/dlna/control/cds", browseBody("0"), tv).Body.String()
	if !strings.Contains(all, "Private") {
		t.Errorf("control: the administrator's root lacks the private library:\n%s", all)
	}
	// And the library's own listing names the film with a playable resource.
	lib := dlnaCall(a.dlna(t, adminID), "POST", "/dlna/control/cds", browseBody(privateLib), tv).Body.String()
	if !strings.Contains(lib, "Home Video") || !strings.Contains(lib, "http://192.168.1.20:8200/dlna/media/") {
		t.Errorf("the private library's listing:\n%s", lib)
	}
}

func TestNothingOffTheLANIsAnswered(t *testing.T) {
	a := newAccessHarness(t)
	h := a.dlna(t, a.memberID(t))
	for _, from := range []string{"8.8.8.8:4000", "[2001:db8::99]:4000"} {
		if code := dlnaCall(h, "GET", "/dlna/device.xml", "", from).Code; code != http.StatusForbidden {
			t.Errorf("device.xml from %s = %d, want 403", from, code)
		}
	}
	for _, from := range []string{tv, "[fe80::1]:4000", "127.0.0.1:4000"} {
		if code := dlnaCall(h, "GET", "/dlna/device.xml", "", from).Code; code != http.StatusOK {
			t.Errorf("device.xml from %s = %d, want 200", from, code)
		}
	}
	// It serves DLNA and nothing else: the LANcast API is not behind it.
	if code := dlnaCall(h, "GET", "/api/libraries", "", tv).Code; code != http.StatusNotFound {
		t.Errorf("/api/libraries through the DLNA listener = %d, want 404", code)
	}
}
