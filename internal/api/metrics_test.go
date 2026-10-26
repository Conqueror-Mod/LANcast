package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func scrape(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/metrics = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type = %q", ct)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func mintMetricsToken(t *testing.T, h *harness) string {
	t.Helper()
	resp := h.authed(t, "POST", "/api/metrics/token", nil)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("POST /api/metrics/token = %d", resp.StatusCode)
	}
	var body struct {
		Token     string `json:"token"`
		CreatedAt int64  `json:"created_at"`
	}
	decode(t, resp, &body)
	if !strings.HasPrefix(body.Token, metricsTokenPrefix) || body.CreatedAt == 0 {
		t.Fatalf("token response = %+v", body)
	}
	return body.Token
}

func TestAnAdministratorCanReadMetrics(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	h.addFile(t, "paddington.mkv", []byte("x"))

	text := scrape(t, h.authed(t, "GET", "/api/metrics", nil))
	for _, want := range []string{
		"# TYPE lancast_build_info gauge",
		`lancast_build_info{version="`,
		"# TYPE lancast_http_requests_total counter",
		`lancast_transcode_sessions{mode="encode"} 0`,
		`lancast_library_items{library_id="`,
		`kind="movie"} 1`,
		"lancast_accounts 1",
		"go_goroutines ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics lack %q\n%s", want, text)
		}
	}
	// Every family is introduced once, before its samples.
	if n := strings.Count(text, "# TYPE lancast_library_items gauge"); n != 1 {
		t.Errorf("lancast_library_items TYPE appears %d times", n)
	}
}

func TestMetricsAreRefusedToEveryoneElse(t *testing.T) {
	a := newAccessHarness(t)

	resp := a.do(t, "GET", "/api/metrics", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no session = %d, want 401", resp.StatusCode)
	}
	resp = a.doAs(t, a.member, "GET", "/api/metrics", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("member = %d, want 403", resp.StatusCode)
	}
	// An API key is never an administrator, whoever owns it (ADR 0061).
	secret, _ := mint(t, a.harness, "grafana")
	resp = a.doKey(t, "GET", "/api/metrics", secret, nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("an administrator's API key read the metrics")
	}
}

func TestTheMetricsTokenReadsMetricsAndNothingElse(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	token := mintMetricsToken(t, h)

	text := scrape(t, h.doKey(t, "GET", "/api/metrics", token, nil))
	if !strings.Contains(text, "lancast_build_info") {
		t.Fatal("the token's scrape is empty")
	}

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/libraries"},
		{"GET", "/api/settings"},
		{"POST", "/api/metrics/token"},
		{"DELETE", "/api/metrics/token"},
		{"GET", "/api/metrics/"},
	} {
		resp := h.doKey(t, c.method, c.path, token, nil)
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("the metrics token reached %s %s: %d", c.method, c.path, resp.StatusCode)
		}
	}

	// Never shown again: settings say only that one exists.
	var settings struct {
		MetricsToken struct {
			Configured bool  `json:"configured"`
			CreatedAt  int64 `json:"created_at"`
		} `json:"metrics_token"`
	}
	resp := h.authed(t, "GET", "/api/settings", nil)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), strings.TrimPrefix(token, metricsTokenPrefix)) {
		t.Fatal("settings returned the token")
	}
	decode(t, h.authed(t, "GET", "/api/settings", nil), &settings)
	if !settings.MetricsToken.Configured || settings.MetricsToken.CreatedAt == 0 {
		t.Fatalf("settings say %+v", settings.MetricsToken)
	}
}

func TestReplacingAndRevokingTheMetricsToken(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	first := mintMetricsToken(t, h)
	second := mintMetricsToken(t, h)

	resp := h.doKey(t, "GET", "/api/metrics", first, nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a replaced token still reads")
	}
	scrape(t, h.doKey(t, "GET", "/api/metrics", second, nil))

	del := h.authed(t, "DELETE", "/api/metrics/token", nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d", del.StatusCode)
	}
	resp = h.doKey(t, "GET", "/api/metrics", second, nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a revoked token still reads")
	}
	// Idempotent.
	del = h.authed(t, "DELETE", "/api/metrics/token", nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("second DELETE = %d", del.StatusCode)
	}
}

// Library names are typed by people, and a quote or a newline in one must not
// break the format for every other line.
func TestLabelValuesAreEscaped(t *testing.T) {
	var p promWriter
	p.family("x", "gauge", "help")
	p.sample("x", 1, "library", "Dad's \"Best\"\nFilms\\")
	want := `x{library="Dad's \"Best\"\nFilms\\"} 1`
	if !strings.Contains(p.String(), want) {
		t.Fatalf("got %q, want a line %q", p.String(), want)
	}
}

// Every sample of a family sits in one block under its TYPE line, however the
// samples were added. The per-library families were interleaved before.
func TestFamiliesAreContiguous(t *testing.T) {
	var p promWriter
	for _, lib := range []string{"a", "b"} {
		p.family("items", "gauge", "h")
		p.sample("items", 1, "library", lib)
		p.family("scan", "gauge", "h")
		p.sample("scan", 2, "library", lib)
	}
	want := "# HELP items h\n# TYPE items gauge\n" +
		"items{library=\"a\"} 1\nitems{library=\"b\"} 1\n" +
		"# HELP scan h\n# TYPE scan gauge\n" +
		"scan{library=\"a\"} 2\nscan{library=\"b\"} 2\n"
	if got := p.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRequestCounterCounts(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	before := h.srvAPI.counters.requests.Load()
	for i := 0; i < 3; i++ {
		resp := h.do(t, "GET", "/api/health", nil)
		resp.Body.Close()
	}
	if got := h.srvAPI.counters.requests.Load() - before; got != 3 {
		t.Fatalf("counted %d requests, want 3", got)
	}
}
