package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// endless is a body that never ends and never says how long it is.
type endless struct{ read int64 }

func (e *endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	e.read += int64(len(p))
	return len(p), nil
}

/*
 * Sign-in answers before any account, so it is where a stranger would send a
 * body of any size. One that says it is too large is refused unread.
 */
func TestOversizedBodyRefusedBeforeSignIn(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/auth/login",
		"/api/auth/login/two-factor",
		"/api/auth/passkey/login",
		"/api/auth/setup",
	} {
		body := bytes.Repeat([]byte("a"), defaultBodyLimit+1)
		req, _ := http.NewRequest("POST", h.srv.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(path, func(t *testing.T) {
			wantError(t, resp, http.StatusRequestEntityTooLarge, "too_large")
		})
	}
}

/*
 * A body that does not say how long it is gets read only to the ceiling. The
 * handler here reads everything it is given, which is what a JSON decoder
 * does with one long string.
 */
func TestUnannouncedBodyIsCutOffAtTheCeiling(t *testing.T) {
	mux := http.NewServeMux()
	var got int64
	var readErr error
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		got, readErr = io.Copy(io.Discard, r.Body)
	})
	src := &endless{}
	req := httptest.NewRequest("POST", "/api/auth/login", io.LimitReader(src, 50<<20))
	req.ContentLength = -1
	limitBodies(mux, mux).ServeHTTP(httptest.NewRecorder(), req)

	var tooBig *http.MaxBytesError
	if !errors.As(readErr, &tooBig) {
		t.Fatalf("read error = %v, want a MaxBytesError", readErr)
	}
	if got > defaultBodyLimit {
		t.Errorf("handler read %d bytes, past the %d ceiling", got, defaultBodyLimit)
	}
	if src.read > defaultBodyLimit+64<<10 {
		t.Errorf("%d bytes were pulled from the client for a %d ceiling", src.read, defaultBodyLimit)
	}
}

// A request with no body is untouched: the GETs that stream films never meet
// the ceiling at all.
func TestNoBodyIsLeftAlone(t *testing.T) {
	mux := http.NewServeMux()
	var body io.ReadCloser
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) { body = r.Body })
	req := httptest.NewRequest("GET", "/x", nil)
	limitBodies(mux, mux).ServeHTTP(httptest.NewRecorder(), req)
	if body != http.NoBody {
		t.Errorf("body = %T, want http.NoBody untouched", body)
	}
}

/*
 * Every route on the larger list is a real route, reached by its pattern.
 *
 * A body over the default ceiling, but under the route's own, is sent to each.
 * If the pattern has drifted from the router's (a renamed path, a changed
 * method), the default applies and the answer is 413 before anything else
 * looks at it. Any other answer, a 401 included, means the route's own
 * ceiling let it through.
 */
func TestLargeBodiesAreRoutes(t *testing.T) {
	h := newHarness(t)
	for pattern, limit := range largeBodies {
		if limit <= defaultBodyLimit {
			t.Errorf("%s: limit %d is no larger than the default, so it does not belong on the list", pattern, limit)
			continue
		}
		method, path, _ := strings.Cut(pattern, " ")
		path = strings.NewReplacer("{id}", "1", "{slot}", "1").Replace(path)
		if strings.Contains(path, "{") {
			t.Fatalf("%s: a wildcard this test does not fill", pattern)
		}
		body := bytes.Repeat([]byte("a"), defaultBodyLimit+1)
		req, _ := http.NewRequest(method, h.srv.URL+path, bytes.NewReader(body))
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			t.Errorf("%s: refused at the default ceiling, so the pattern does not match its route", pattern)
		}
	}
}
