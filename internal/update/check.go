// Package update reports whether a newer LANcast release exists.
//
// This is the one part of LANcast that contacts a server the operator did not
// choose. It is worth being exact about why that is consistent with the
// no-phone-home principle rather than an exception to it: the principle says
// nothing is *required* to reach the internet for the server to work. An update
// check is not required, changes nothing if it never succeeds, and can be
// switched off. Metadata providers are already on those terms.
//
// What is sent is a plain GET to the releases API. No install identifier, no
// library statistics, no version history — the request carries an address and a
// user agent because HTTP does, and nothing is added to it.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// releasesURL is the project's own releases endpoint. A constant rather than a
// setting: an updater pointed at an arbitrary host by configuration is a way to
// hand someone a malicious update, and the signature check is what makes the
// destination matter less rather than not at all.
const releasesURL = "https://api.github.com/repos/Conqueror-Mod/LANcast/releases/latest"

// checkInterval is how often a background check may run. Deliberately long: a
// media server on a home LAN does not need to know about a release the hour it
// lands, and anything shorter is traffic with no reader.
const checkInterval = 24 * time.Hour

// State is what the UI shows. It is a snapshot, safe to serialize.
type State struct {
	// Current is the running build. "dev" for an unstamped build, which is why
	// Available can be false while Latest is set.
	Current string `json:"current"`
	// Latest is the newest published release, empty until a check succeeds.
	Latest string `json:"latest,omitempty"`
	// Available is true only when Latest is genuinely newer than Current. A
	// build that cannot compare its own version never claims an update.
	Available bool `json:"available"`
	// URL is where a human can read about it and download by hand.
	URL string `json:"url,omitempty"`
	// CheckedAt is when the last attempt finished, successful or not.
	CheckedAt int64 `json:"checked_at,omitempty"`
	// Error is the last failure, cleared by a success. Surfaced rather than
	// swallowed: an update check that has been silently failing for months is
	// indistinguishable from one that has nothing to report.
	Error string `json:"error,omitempty"`
	// Checking is true while a check is in flight.
	Checking bool `json:"checking"`
}

// Checker holds the last result and does the asking.
type Checker struct {
	current string
	client  *http.Client

	mu    sync.Mutex
	state State
}

func New(currentVersion string) *Checker {
	return &Checker{
		current: currentVersion,
		// A short timeout on purpose. This is a background courtesy; it must
		// never be the reason something else waits.
		client: &http.Client{Timeout: 10 * time.Second},
		state:  State{Current: currentVersion},
	}
}

// State returns the last known result.
func (c *Checker) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Due reports whether a background check should run now.
func (c *Checker) Due() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Checking {
		return false
	}
	return c.state.CheckedAt == 0 ||
		time.Since(time.Unix(c.state.CheckedAt, 0)) >= checkInterval
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Check asks once. Errors are recorded in the state rather than returned as
// something a caller must handle: the only reasonable response to "the update
// check failed" is to say so and carry on.
func (c *Checker) Check(ctx context.Context) State {
	return c.checkAt(ctx, releasesURL)
}

// checkAt is Check against a given endpoint, so tests can serve their own
// releases API. The exported path always uses the constant — a URL a caller
// could choose would be a way to hand someone a malicious update.
func (c *Checker) checkAt(ctx context.Context, url string) State {
	c.mu.Lock()
	if c.state.Checking {
		s := c.state
		c.mu.Unlock()
		return s
	}
	c.state.Checking = true
	c.mu.Unlock()

	latest, htmlURL, err := c.fetch(ctx, url)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Checking = false
	c.state.CheckedAt = time.Now().Unix()
	if err != nil {
		c.state.Error = err.Error()
		return c.state
	}
	c.state.Error = ""
	c.state.Latest = latest
	c.state.URL = htmlURL
	c.state.Available = Newer(c.current, latest)
	return c.state
}

func (c *Checker) fetch(ctx context.Context, endpoint string) (tag, url string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// 404 from a repository that plainly exists means it is private: the API
	// does not distinguish "no such repository" from "not yours to see", by
	// design. Said plainly here because the alternative is an operator staring
	// at a bare 404 and concluding the updater is broken, when it is working and
	// the repository simply is not public yet.
	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("releases: not found — the project repository is not public, so update checks cannot work yet")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("releases: %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}
	if rel.Draft || rel.Prerelease {
		// /latest already excludes both; this is belt and braces, because
		// offering someone a draft of their own unpublished release would be a
		// confusing way to find out the filter changed.
		return "", "", fmt.Errorf("releases: latest is a draft or prerelease")
	}
	return rel.TagName, rel.HTMLURL, nil
}

// Newer reports whether tag is a later version than current.
//
// Conservative by construction: anything it cannot parse is not newer. A build
// that cannot compare its own version must not offer an update, because the
// failure mode is telling someone to install something they already have — or
// worse, to downgrade.
func Newer(current, tag string) bool {
	c, okC := parseVersion(current)
	t, okT := parseVersion(tag)
	if !okC || !okT {
		return false
	}
	for i := 0; i < 3; i++ {
		if t[i] != c[i] {
			return t[i] > c[i]
		}
	}
	return false
}

// parseVersion reads "v1.2.3" or "1.2.3". A pre-release suffix is rejected
// rather than ignored: "1.2.3-rc1" is not 1.2.3, and treating it as such would
// offer a release candidate as a stable update.
func parseVersion(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if s == "" || s == "dev" {
		return out, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
