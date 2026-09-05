package api

import (
	"context"
	"net/http"
	"testing"
)

/*
 * The semantic-search endpoints on a server with nothing installed — which is
 * every server until somebody presses the button, and the case most likely to
 * be got wrong.
 *
 * The risk is the same one the face routes have already paid for, one feature
 * along: a client that cannot tell "nothing in your photographs matched" from
 * "nothing has ever looked at your photographs" shows the first when the second
 * is true, and teaches people the feature is broken. Every test here is about
 * that distinction surviving the wire.
 *
 * The second risk is subtler and has no symptom at all: answering a semantic
 * question with the *face* feature's readiness. They share a binary, so it is a
 * one-word slip, and it reports a working search as unavailable on every server
 * that never wanted face grouping.
 */

func pictureLibrary(t *testing.T, h *harness) int64 {
	t.Helper()
	lib, err := h.st.CreateLibrary(context.Background(), "Photographs", "picture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return lib.ID
}

// Capabilities answers even with nothing installed, and says why not.
func TestSemanticCapabilitiesAlwaysAnswers(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "GET", "/api/photos/semantic/capabilities", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a missing model is not an error",
			resp.StatusCode)
	}
	m := decodeMap(t, resp)
	if ready, _ := m["semantic_ready"].(bool); ready {
		t.Error("reported ready with nothing installed")
	}
	if reason, _ := m["semantic_reason"].(string); reason == "" {
		t.Error("reported not ready without saying why; a client cannot " +
			"explain a blank search page from that")
	}
}

/*
 * And it answers about *search*, not about face grouping.
 *
 * The two features share a worker binary, so returning the whole capabilities
 * struct here is a one-word slip with no symptom: a client would eventually
 * branch on `ready`, which is a different feature's answer and false on a
 * server whose search works perfectly well.
 */
func TestSemanticCapabilitiesDoesNotAnswerAboutFaces(t *testing.T) {
	h := newHarness(t)

	m := decodeMap(t, h.do(t, "GET", "/api/photos/semantic/capabilities", nil))
	if _, present := m["ready"]; present {
		t.Error("the face feature's `ready` is on the semantic route; a client " +
			"will branch on it and report a working search as unavailable")
	}
	if _, present := m["semantic_ready"]; !present {
		t.Error("no semantic_ready on the semantic capabilities route")
	}
}

/*
 * A search with no models is refused with a reason, rather than answered with
 * an empty list.
 *
 * An empty 200 here is the exact failure this file exists for: it is
 * indistinguishable from a library where nothing matched, and it is the answer
 * somebody would show a person who has not installed the feature at all.
 */
func TestSearchingWithoutTheModelsIsRefusedWithAReason(t *testing.T) {
	h := newHarness(t)
	id := pictureLibrary(t, h)

	resp := h.do(t, "GET", "/api/libraries/"+itoa(id)+"/photos/search?q=a+dog", nil)
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a search answered 200 with no models installed; that is " +
			"indistinguishable from a library where nothing matched")
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	m := decodeMap(t, resp)
	if m["error"] == nil && m["message"] == nil {
		t.Error("refused without an error envelope")
	}
}

// A search with no query is the caller's mistake and says so, which is the one
// case here that genuinely is a bad request.
func TestSearchingWithNoQueryIsABadRequest(t *testing.T) {
	h := newHarness(t)
	id := pictureLibrary(t, h)

	for _, q := range []string{"", "?q=", "?q=%20%20"} {
		resp := h.do(t, "GET", "/api/libraries/"+itoa(id)+"/photos/search"+q, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", q, resp.StatusCode)
		}
	}
}

// Searching a library that holds no photographs is a 400 naming the reason,
// not an empty result: a film library has nothing to search and never will.
func TestSearchingAFilmLibraryIsRefused(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "GET", "/api/libraries/"+itoa(h.lib.ID)+"/photos/search?q=a+dog", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 wrong_kind", resp.StatusCode)
	}
}

// Starting a pass with nothing installed is refused rather than accepted and
// quietly dropped, which would leave somebody waiting for a progress bar that
// never appears.
func TestStartingASemanticPassWithoutTheModelsIsRefused(t *testing.T) {
	h := newHarness(t)
	id := pictureLibrary(t, h)

	resp := h.do(t, "POST", "/api/libraries/"+itoa(id)+"/photos/index", nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatal("an indexing pass was accepted with no models installed")
	}
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

/*
 * The model list is readable and honest on a platform with no pinned build.
 *
 * `supported: false` rather than an empty asset list: an empty list reads as
 * "nothing to download", which is what a finished install looks like.
 */
func TestSemanticModelsAlwaysAnswers(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "GET", "/api/photos/semantic/models", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	m := decodeMap(t, resp)
	if _, present := m["supported"]; !present {
		t.Fatal("no `supported`; a client cannot tell an unsupported platform " +
			"from a finished install")
	}
	if m["job"] == nil {
		t.Error("no job snapshot, so a client has nothing to poll")
	}
	if supported, _ := m["supported"].(bool); !supported {
		if reason, _ := m["reason"].(string); reason == "" {
			t.Error("unsupported without saying so in words")
		}
		return
	}
	/*
	 * Every asset is identifiable before a byte moves. A download somebody
	 * cannot identify is not consent, and 600MB is not a rounding error on
	 * somebody's connection.
	 */
	assets, _ := m["assets"].([]any)
	if len(assets) == 0 {
		t.Fatal("supported, but nothing listed to download")
	}
	for _, a := range assets {
		row, _ := a.(map[string]any)
		for _, field := range []string{"name", "size_bytes", "licence", "licence_url", "url", "present"} {
			if _, ok := row[field]; !ok {
				t.Errorf("asset %v has no %s", row["name"], field)
			}
		}
	}
}
