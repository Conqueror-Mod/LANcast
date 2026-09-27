package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * Browsing a share, asked for by the friend's own server
 * (ADR 0071's amendment).
 *
 * These call the handlers directly with a crafted TLS state rather than
 * standing up a mutual-TLS listener. FingerprintFromState reads one thing —
 * the public key on the first peer certificate — so a certificate carrying
 * that key is the whole of what the auth path sees, and the rest of the
 * machinery would be scenery.
 *
 * It is the first HTTP-level test of a federation endpoint; presence has none,
 * for the reason that made this look hard.
 */

// peerRequest is a request as it arrives on the peer channel: a client
// certificate carrying that server's identity key, and nothing else.
func peerRequest(t *testing.T, id identity.Identity, target string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{PublicKey: id.Public()}},
	}
	return r
}

// anonymousRequest is the same request with no certificate — an ordinary
// browser reaching a federation route.
func anonymousRequest(target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.TLS = &tls.ConnectionState{}
	return r
}

type fedFixture struct {
	h        *harness
	georgia  identity.Identity
	stranger identity.Identity
	peerFP   string
}

func newFedFixture(t *testing.T) fedFixture {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	return fedFixture{
		h: h, georgia: georgia, stranger: anotherServer(t),
		peerFP: identity.Normalize(georgia.Fingerprint()),
	}
}

func (f fedFixture) share(t *testing.T, lib int64, ceiling string) {
	t.Helper()
	if err := f.h.st.ShareLibrary(context.Background(), f.peerFP, lib, ceiling, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func (f fedFixture) call(h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestAPairedServerSeesWhatItWasGranted(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")

	w := f.call(f.h.srvAPI.federationLibraries,
		peerRequest(t, f.georgia, "/api/federation/libraries"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got struct {
		Libraries []struct {
			ID int64 `json:"id"`
		} `json:"libraries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Libraries) != 1 || got.Libraries[0].ID != f.h.lib.ID {
		t.Errorf("libraries = %+v, want only the shared one", got.Libraries)
	}
}

// Pairing grants nothing: a peer with no shares gets an empty list.
func TestAPairedServerWithNoSharesSeesNothing(t *testing.T) {
	f := newFedFixture(t)

	w := f.call(f.h.srvAPI.federationLibraries,
		peerRequest(t, f.georgia, "/api/federation/libraries"))
	var got struct {
		Libraries []any `json:"libraries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Libraries) != 0 {
		t.Errorf("libraries = %+v, want none", got.Libraries)
	}
}

/*
 * A server this one has never paired with is refused, and refused at the
 * lookup rather than by anything it asked for — so unpairing stops answering
 * immediately, with nothing else to clean up.
 */
func TestAnUnpairedServerIsRefused(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")

	for _, c := range []struct {
		name string
		r    *http.Request
		want int
	}{
		{"a server we never paired with", peerRequest(t, f.stranger, "/api/federation/libraries"), http.StatusForbidden},
		{"no client certificate at all", anonymousRequest("/api/federation/libraries"), http.StatusUnauthorized},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := f.call(f.h.srvAPI.federationLibraries, c.r)
			if w.Code != c.want {
				t.Errorf("status = %d, want %d", w.Code, c.want)
			}
			if bodyMentionsLibrary(w.Body.Bytes()) {
				t.Error("the refusal disclosed a library")
			}
		})
	}
}

// Unpairing refuses the next request.
func TestUnpairingRefusesFederationBrowse(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")

	w := f.call(f.h.srvAPI.federationLibraries,
		peerRequest(t, f.georgia, "/api/federation/libraries"))
	if w.Code != http.StatusOK {
		t.Fatalf("fixture: status %d while paired", w.Code)
	}

	f.h.authed(t, "DELETE", "/api/peers/"+f.georgia.Fingerprint(), nil).Body.Close()

	w = f.call(f.h.srvAPI.federationLibraries,
		peerRequest(t, f.georgia, "/api/federation/libraries"))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d after unpairing, want 403", w.Code)
	}
}

/*
 * The same scoping the ticket path gets, because it is the same code. This is
 * the test that the two ways in did not drift: a library that was not shared
 * is a 404 here exactly as it is there.
 */
func TestFederationBrowseIsScopedToTheShare(t *testing.T) {
	f := newFedFixture(t)
	ctx := context.Background()
	shared := f.h.addFile(t, "shared.mkv", []byte("x"))

	private, err := f.h.st.CreateLibrary(ctx, "Private", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.share(t, f.h.lib.ID, "")

	w := f.call(f.h.srvAPI.federationItems,
		peerRequest(t, f.georgia, "/api/federation/items?library="+itoa(f.h.lib.ID)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d for the shared library, want 200", w.Code)
	}
	var got struct {
		Items []struct {
			ID        int64 `json:"id"`
			LibraryID int64 `json:"library_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var sawShared bool
	for _, it := range got.Items {
		if it.LibraryID == private.ID {
			t.Error("an item from an unshared library was listed")
		}
		if it.ID == shared {
			sawShared = true
		}
	}
	if !sawShared {
		t.Error("the shared library's own item was not listed")
	}

	w = f.call(f.h.srvAPI.federationItems,
		peerRequest(t, f.georgia, "/api/federation/items?library="+itoa(private.ID)))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d for an unshared library, want 404", w.Code)
	}
}

// The share's own limit applies here too, for the same reason: it is the same
// lookup, made in the same place.
func TestFederationBrowseAppliesTheShareCeiling(t *testing.T) {
	f := newFedFixture(t)
	ctx := context.Background()
	kids := f.h.addFile(t, "paddington.mkv", []byte("x"))
	grown := f.h.addFile(t, "scream.mkv", []byte("x"))
	g, r := "G", "R"
	if err := f.h.st.UpdateItemMetadata(ctx, kids, store.ItemMetadata{ContentRating: &g}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.UpdateItemMetadata(ctx, grown, store.ItemMetadata{ContentRating: &r}); err != nil {
		t.Fatal(err)
	}
	f.share(t, f.h.lib.ID, "PG")

	w := f.call(f.h.srvAPI.federationItems,
		peerRequest(t, f.georgia, "/api/federation/items?library="+itoa(f.h.lib.ID)))
	var got struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, it := range got.Items {
		if it.ID == grown {
			t.Error("an R film was listed under a PG share limit")
		}
	}
}

// bodyMentionsLibrary reports whether a response leaked a library listing.
func bodyMentionsLibrary(b []byte) bool {
	var v struct {
		Libraries []any `json:"libraries"`
		Items     []any `json:"items"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return false
	}
	return len(v.Libraries) > 0 || len(v.Items) > 0
}

// --- streaming to a paired server ------------------------------------------

/*
 * The permission question is answered by the same call the browse path uses,
 * so these are about it being asked at all — and about a refusal saying
 * nothing.
 */
func TestAPairedServerStreamsOnlySharedItems(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("the film itself"))

	// Not shared yet.
	w := f.call(f.h.srvAPI.federationStream,
		peerRequest(t, f.georgia, "/api/federation/stream?item="+itoa(item)))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d before sharing, want 404", w.Code)
	}
	if w.Body.Len() > 200 {
		t.Error("the refusal returned a body large enough to be the file")
	}

	f.share(t, f.h.lib.ID, "")
	w = f.call(f.h.srvAPI.federationStream,
		peerRequest(t, f.georgia, "/api/federation/stream?item="+itoa(item)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d after sharing, want 200", w.Code)
	}
	if got := w.Body.String(); got != "the film itself" {
		t.Errorf("body = %q, want the file's contents", got)
	}
}

// The share's limit gates playing as well as listing. A friend who may not
// see a film in a listing must not be able to fetch it by id either.
func TestAShareCeilingBlocksFederationStreaming(t *testing.T) {
	f := newFedFixture(t)
	ctx := context.Background()
	grown := f.h.addFile(t, "scream.mkv", []byte("x"))
	rr := "R"
	if err := f.h.st.UpdateItemMetadata(ctx, grown, store.ItemMetadata{ContentRating: &rr}); err != nil {
		t.Fatal(err)
	}
	f.share(t, f.h.lib.ID, "PG")

	w := f.call(f.h.srvAPI.federationStream,
		peerRequest(t, f.georgia, "/api/federation/stream?item="+itoa(grown)))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d for an R film under a PG limit, want 404", w.Code)
	}
}

// Unpairing stops the bytes on the next request.
func TestUnpairingStopsFederationStreaming(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	f.share(t, f.h.lib.ID, "")

	w := f.call(f.h.srvAPI.federationStream,
		peerRequest(t, f.georgia, "/api/federation/stream?item="+itoa(item)))
	if w.Code != http.StatusOK {
		t.Fatalf("fixture: status %d while paired", w.Code)
	}

	f.h.authed(t, "DELETE", "/api/peers/"+f.georgia.Fingerprint(), nil).Body.Close()
	w = f.call(f.h.srvAPI.federationStream,
		peerRequest(t, f.georgia, "/api/federation/stream?item="+itoa(item)))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d after unpairing, want 403", w.Code)
	}
}

/*
 * Range requests pass through, which is what makes seeking work at the far
 * end: the friend's server is a pipe rather than a buffer, and a viewer
 * dragging the scrubber produces the same partial requests here that a local
 * viewer would.
 */
func TestFederationStreamingSupportsRanges(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("0123456789"))
	f.share(t, f.h.lib.ID, "")

	r := peerRequest(t, f.georgia, "/api/federation/stream?item="+itoa(item))
	r.Header.Set("Range", "bytes=2-5")
	w := f.call(f.h.srvAPI.federationStream, r)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if got := w.Body.String(); got != "2345" {
		t.Errorf("body = %q, want the requested range", got)
	}
	if cr := w.Header().Get("Content-Range"); cr == "" {
		t.Error("no Content-Range header, so the far end cannot seek")
	}
}

// An unpaired server gets nothing, and learns nothing.
func TestAnUnpairedServerCannotStream(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	f.share(t, f.h.lib.ID, "")

	for _, c := range []struct {
		name string
		r    *http.Request
		want int
	}{
		{"never paired", peerRequest(t, f.stranger, "/api/federation/stream?item="+itoa(item)), http.StatusForbidden},
		{"no certificate", anonymousRequest("/api/federation/stream?item=" + itoa(item)), http.StatusUnauthorized},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := f.call(f.h.srvAPI.federationStream, c.r)
			if w.Code != c.want {
				t.Errorf("status = %d, want %d", w.Code, c.want)
			}
		})
	}
}

// An item id that is not one is refused before anything is opened.
func TestFederationStreamingNeedsAnItem(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")

	for _, q := range []string{"", "?item=", "?item=abc", "?item=0", "?item=-1"} {
		w := f.call(f.h.srvAPI.federationStream,
			peerRequest(t, f.georgia, "/api/federation/stream"+q))
		if w.Code == http.StatusOK {
			t.Errorf("%q was accepted", q)
		}
	}
}
