package plugin

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCache is an in-memory ResponseCache that records what it was asked for,
// so a test can assert on the keys as well as the hits.
type fakeCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	writes  []string // "provider|key"
	reads   []string
}

func newFakeCache() *fakeCache { return &fakeCache{entries: map[string][]byte{}} }

// runtimeFor builds a Runtime with no module, for the policy pieces that do not
// need one.
func runtimeFor(t *testing.T, opts ...Option) *Runtime {
	t.Helper()
	ctx := context.Background()
	rt, err := NewRuntime(ctx, quietLog(), opts...)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(func() { rt.Close(ctx) })
	return rt
}

func (c *fakeCache) CachedResponse(_ context.Context, provider, key string, _ time.Duration) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads = append(c.reads, provider+"|"+key)
	b, ok := c.entries[provider+"|"+key]
	return b, ok, nil
}

func (c *fakeCache) CacheResponse(_ context.Context, provider, key string, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, provider+"|"+key)
	c.entries[provider+"|"+key] = payload
	return nil
}

// The second identical fetch is served from the cache, and never reaches the
// wire. This is the property the native providers have and a plugin could not
// give itself: a rescan of an already-enriched library costs no API calls.
func TestPluginFetchIsCached(t *testing.T) {
	cache := newFakeCache()
	var calls int
	p := loadFixture(t, WithResponseCache(cache), WithHTTPGetter(func(ctx context.Context, url string) ([]byte, error) {
		calls++
		return []byte("payload"), nil
	}))
	ctx := context.Background()

	for i := range 3 {
		out, err := p.Call(ctx, "httpget", []byte("https://example.test/data?api_key=hunter2"))
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(out) != "payload" {
			t.Fatalf("call %d returned %q, want payload", i, out)
		}
	}
	if calls != 1 {
		t.Errorf("reached the wire %d times, want once", calls)
	}
}

/*
 * The cache key must not be the URL, because the URL is the secret.
 *
 * The native clients build their key before adding the api_key parameter, so it
 * never reaches the column. The host only ever sees a plugin's URL fully formed
 * — key included — so it hashes it. Without this, enabling the cache would
 * write every user's TMDB and OMDb keys into the database in plain text, in a
 * table nobody thinks of as holding credentials.
 */
func TestPluginCacheKeyDoesNotCarryTheSecret(t *testing.T) {
	cache := newFakeCache()
	p := loadFixture(t, WithResponseCache(cache), WithHTTPGetter(func(ctx context.Context, url string) ([]byte, error) {
		return []byte("payload"), nil
	}))
	_, err := p.Call(context.Background(), "httpget", []byte("https://example.test/data?api_key=hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.writes) != 1 {
		t.Fatalf("writes = %v, want one", cache.writes)
	}
	got := cache.writes[0]
	if strings.Contains(got, "hunter2") {
		t.Errorf("cache key %q carries the API key", got)
	}
	if strings.Contains(got, "example.test") {
		t.Errorf("cache key %q carries the URL", got)
	}
	// Namespaced by plugin: two plugins granted the same host must not read
	// each other's authenticated answers.
	if !strings.HasPrefix(got, "plugin:fixture|") {
		t.Errorf("cache key %q is not namespaced by plugin", got)
	}
}

// Two plugins fetching the same URL get separate cache entries, because the
// response is what each one's own key bought.
func TestPluginCacheIsPerPlugin(t *testing.T) {
	cache := newFakeCache()
	ctx := context.Background()
	var calls int
	getter := WithHTTPGetter(func(ctx context.Context, url string) ([]byte, error) {
		calls++
		return []byte("payload"), nil
	})

	one := loadFixture(t, WithResponseCache(cache), getter)
	two := loadFixture(t, WithResponseCache(cache), getter)
	two.Manifest.Name = "other"

	const url = "https://example.test/data"
	if _, err := one.Call(ctx, "httpget", []byte(url)); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Call(ctx, "httpget", []byte(url)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("reached the wire %d times, want twice — one plugin read another's entry", calls)
	}
}

// A URL a plugin's manifest does not grant never reaches the cache or the
// limiter, let alone the wire. The manifest check stays first.
func TestDeniedFetchIsNotCached(t *testing.T) {
	cache := newFakeCache()
	p := loadFixture(t, WithResponseCache(cache), WithHTTPGetter(func(ctx context.Context, url string) ([]byte, error) {
		t.Error("an ungranted host reached the wire")
		return nil, nil
	}))
	out, err := p.Call(context.Background(), "httpget", []byte("https://evil.test/steal"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("denied fetch returned %q, want nothing", out)
	}
	if len(cache.reads)+len(cache.writes) != 0 {
		t.Errorf("cache was touched for a denied URL: reads=%v writes=%v", cache.reads, cache.writes)
	}
}

/*
 * The limiter is keyed on the host, not the plugin.
 *
 * What is being protected is somebody else's API, and its budget is spent
 * against this server's IP address whichever plugin spends it. If this ever
 * becomes per-plugin, n plugins each politely taking 5 requests a second means
 * the remote sees 5n and bans the user.
 */
func TestRateLimitIsSharedAcrossPluginsPerHost(t *testing.T) {
	ctx := context.Background()
	// One token a second, burst of two: the third request must wait.
	rt := runtimeFor(t, WithRateLimit(1))

	same := rt.limiterFor("https://example.test/a")
	deep := rt.limiterFor("https://example.test/b?x=1")
	other := rt.limiterFor("https://other.test/a")
	if same != deep {
		t.Error("two URLs on one host got different limiters — a plugin could mint budget by varying the path")
	}
	if same == other {
		t.Error("two hosts share a limiter")
	}

	// Drain the burst, then time one more.
	for range 2 {
		if err := same.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	if err := same.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Errorf("third request waited %v at 1/s, want it to have been held", elapsed)
	}
}

// A cache hit must not spend a token. Making an answer that came off disk wait
// behind a rate limit is how a rescan of an already-enriched library becomes
// slow for no reason — the exact thing the cache was for.
func TestCacheHitDoesNotSpendAToken(t *testing.T) {
	cache := newFakeCache()
	p := loadFixture(t,
		WithResponseCache(cache),
		WithRateLimit(1),
		WithHTTPGetter(func(ctx context.Context, url string) ([]byte, error) { return []byte("payload"), nil }),
	)
	ctx := context.Background()
	const url = "https://example.test/data"

	if _, err := p.Call(ctx, "httpget", []byte(url)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for range 5 {
		if _, err := p.Call(ctx, "httpget", []byte(url)); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("five cache hits took %v at 1 request/sec — they are queueing behind the limiter", elapsed)
	}
}

/*
 * A plugin's query string is where its API key lives, and these URLs are
 * logged. Every "denied" and "failed" line was writing the user's OMDb or TMDB
 * key into lancastd.log — a file people paste into bug reports.
 */
func TestRedactURLDropsTheQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.omdbapi.com/?i=tt1&apikey=hunter2", "https://www.omdbapi.com/?…"},
		{"https://api.themoviedb.org/3/movie/1?api_key=hunter2&language=en-US", "https://api.themoviedb.org/3/movie/1?…"},
		{"https://example.test/plain", "https://example.test/plain"},
		{"https://user:pw@example.test/x", "https://example.test/x"},
		{"://nonsense", "(unparseable url)"},
	}
	for _, tc := range cases {
		if got := redactURL(tc.in); got != tc.want {
			t.Errorf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.Contains(redactURL(tc.in), "hunter2") || strings.Contains(redactURL(tc.in), ":pw@") {
			t.Errorf("redactURL(%q) leaked a credential", tc.in)
		}
	}
}
