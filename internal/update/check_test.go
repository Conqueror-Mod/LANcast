package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The comparison decides whether a person is told to install something. Getting
// it wrong in the permissive direction means offering a downgrade, or offering
// an update to someone already running it.
func TestNewer(t *testing.T) {
	cases := []struct {
		current, tag string
		want         bool
	}{
		{"0.6.1", "v0.6.2", true},
		{"v0.6.1", "0.7.0", true},
		{"0.6.1", "1.0.0", true},
		{"0.6.1", "v0.6.1", false},
		{"0.6.1", "v0.6.0", false},
		{"1.0.0", "v0.9.9", false},
		// Two-digit components must compare numerically, not as text: the
		// classic way "0.10.0" reads as older than "0.9.0".
		{"0.9.0", "v0.10.0", true},
		{"0.10.0", "v0.9.0", false},
		// A development build cannot compare itself, so it is never told to
		// update. Claiming otherwise would nag every developer on every run.
		{"dev", "v9.9.9", false},
		{"", "v1.0.0", false},
		// A pre-release is not the stable version it resembles.
		{"0.6.1", "v0.6.2-rc1", false},
		{"0.6.1", "not-a-version", false},
	}
	for _, tc := range cases {
		if got := Newer(tc.current, tc.tag); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.tag, got, tc.want)
		}
	}
}

func TestCheckReportsAnAvailableRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://example.invalid/r"}`))
	}))
	defer srv.Close()

	c := New("0.6.1")
	c.client = srv.Client()
	st := c.checkAt(context.Background(), srv.URL)

	if !st.Available || st.Latest != "v9.9.9" {
		t.Errorf("state = %+v, want an available v9.9.9", st)
	}
	if st.Error != "" {
		t.Errorf("unexpected error: %s", st.Error)
	}
	if st.CheckedAt == 0 {
		t.Error("CheckedAt was not stamped")
	}
}

// A failure has to be visible. An update check that has been quietly failing
// for months is indistinguishable from one with nothing to report, which is the
// shape of bug this project keeps finding.
func TestCheckRecordsFailureRatherThanHidingIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("0.6.1")
	c.client = srv.Client()
	st := c.checkAt(context.Background(), srv.URL)

	if st.Error == "" {
		t.Error("a failed check reported no error")
	}
	if st.Available {
		t.Error("a failed check claimed an update was available")
	}
	if st.CheckedAt == 0 {
		t.Error("a failed check must still record that it happened, or it retries forever")
	}
}

// Draft and prerelease are excluded by the endpoint, but a change there must not
// silently start offering unpublished builds.
func TestDraftIsNotOffered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","draft":true}`))
	}))
	defer srv.Close()

	c := New("0.6.1")
	c.client = srv.Client()
	st := c.checkAt(context.Background(), srv.URL)
	if st.Available {
		t.Error("a draft release was offered as an update")
	}
}

// Due is what stops the check becoming traffic with no reader.
func TestDueRespectsTheInterval(t *testing.T) {
	c := New("0.6.1")
	if !c.Due() {
		t.Error("a checker that has never run should be due")
	}
	c.mu.Lock()
	c.state.CheckedAt = time.Now().Unix()
	c.mu.Unlock()
	if c.Due() {
		t.Error("a checker that just ran should not be due")
	}
	c.mu.Lock()
	c.state.CheckedAt = time.Now().Add(-2 * checkInterval).Unix()
	c.mu.Unlock()
	if !c.Due() {
		t.Error("a checker past the interval should be due")
	}
}

// The private-repository case, which is where this actually stands today: the
// API returns 404 for a repository that exists but is not yours to see, and a
// bare 404 would read as "the updater is broken" rather than "not public yet".
func TestPrivateRepositoryExplainsItself(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New("0.6.1")
	c.client = srv.Client()
	st := c.checkAt(context.Background(), srv.URL)

	if st.Error == "" {
		t.Fatal("no error recorded")
	}
	if !strings.Contains(st.Error, "not public") {
		t.Errorf("error = %q; it must explain the repository is not public", st.Error)
	}
}

// The whole path against a releases API that behaves like a public repository,
// which is what this becomes the day the repo is published.
func TestEndToEndAgainstAPublicRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.7.0",` +
			`"html_url":"https://github.com/Conqueror-Mod/LANcast/releases/tag/v0.7.0"}`))
	}))
	defer srv.Close()

	c := New("0.6.1")
	c.client = srv.Client()
	st := c.checkAt(context.Background(), srv.URL)

	if !st.Available {
		t.Fatalf("v0.7.0 over 0.6.1 was not offered: %+v", st)
	}
	if st.URL == "" {
		t.Error("no release URL, so there is nowhere to send the reader")
	}
	if st.Error != "" {
		t.Errorf("unexpected error: %s", st.Error)
	}
}
