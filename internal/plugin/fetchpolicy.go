package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"time"

	"lancast/internal/meta"
)

/*
 * Caching and rate limiting for plugin fetches, and why they live here.
 *
 * The TMDB plugin has neither, and building it is what made the gap obvious.
 * The native client caches every response for a day and takes a token from a
 * limiter before each request; the plugin does neither, because a guest cannot.
 * Both are properties of whoever holds the socket, and the whole point of the
 * capability model is that the guest does not.
 *
 * There was a second, worse reason not to leave it to guests. A rate limit
 * implemented in a plugin is a rate limit the plugin can choose not to have —
 * and the request goes out over the server's IP address, so an impolite or
 * simply buggy plugin gets *the user* rate-limited or banned by somebody else's
 * API, for something they cannot see and did not do. It has to be somewhere a
 * plugin cannot opt out of, and host_http_get is the only such place.
 *
 * So it sits between the manifest check and the fetch: every plugin gets it,
 * no plugin asks for it, and no plugin can decline it.
 */

// ResponseCache persists raw plugin fetch payloads. The store satisfies it;
// tests supply their own. It is the same interface and the same table the
// native providers use, so a plugin provider gets the property that makes a
// rescan free rather than expensive.
type ResponseCache interface {
	CachedResponse(ctx context.Context, provider, key string, maxAge time.Duration) ([]byte, bool, error)
	CacheResponse(ctx context.Context, provider, key string, payload []byte) error
}

const (
	// defaultCacheTTL matches the native providers'. Search results and detail
	// records change rarely, and a day keeps rescans free.
	defaultCacheTTL = 24 * time.Hour

	// defaultRatePerSec matches the native providers' default.
	defaultRatePerSec = 5
)

// WithResponseCache gives plugin fetches the same persistent response cache the
// native providers use.
func WithResponseCache(c ResponseCache) Option { return func(r *Runtime) { r.cache = c } }

// WithCacheTTL overrides how long a cached plugin response stays fresh.
func WithCacheTTL(d time.Duration) Option {
	return func(r *Runtime) {
		if d > 0 {
			r.cacheTTL = d
		}
	}
}

// WithRateLimit sets the requests per second any one remote host will be asked
// for. Zero or less leaves the default.
func WithRateLimit(perSec float64) Option {
	return func(r *Runtime) {
		if perSec > 0 {
			r.ratePerSec = perSec
		}
	}
}

/*
 * fetch is host_http_get's body once the manifest has approved the URL.
 *
 * Cache first, then the limiter, then the wire. A cache hit must not spend a
 * token: making a served-from-disk answer wait behind a rate limit is how a
 * rescan of an already-enriched library becomes slow for no reason, which is
 * the exact thing caching was for.
 */
func (rt *Runtime) fetch(ctx context.Context, p *Plugin, raw string) ([]byte, error) {
	provider, key := cacheKey(p.Manifest.Name, raw)

	if rt.cache != nil {
		if payload, ok, err := rt.cache.CachedResponse(ctx, provider, key, rt.ttl()); err == nil && ok {
			return payload, nil
		}
	}

	if err := rt.limiterFor(raw).Wait(ctx); err != nil {
		return nil, err
	}

	body, err := rt.httpc(ctx, raw)
	if err != nil {
		return nil, err
	}
	if rt.cache != nil && len(body) > 0 {
		// A cache write failure must not fail the request, the same rule the
		// native clients follow: the answer is already in hand.
		_ = rt.cache.CacheResponse(ctx, provider, key, body)
	}
	return body, nil
}

func (rt *Runtime) ttl() time.Duration {
	if rt.cacheTTL > 0 {
		return rt.cacheTTL
	}
	return defaultCacheTTL
}

/*
 * SetRateLimit changes the per-host allowance and discards the current
 * limiters.
 *
 * The rate is a user setting, and the native TMDB client picks up a change
 * immediately because its whole client is rebuilt when settings change. The
 * plugin runtime is built once at startup, so without this a person lowering
 * the rate would find the native provider obeyed them and the plugins did not
 * — the sort of half-applied setting that reads as the setting being broken.
 *
 * Discarding the limiters loses whatever tokens had accumulated, which is the
 * conservative direction: a rate change starts the new allowance from empty
 * rather than handing out a burst under the old one.
 */
func (rt *Runtime) SetRateLimit(perSec float64) {
	if perSec <= 0 {
		return
	}
	rt.limMu.Lock()
	defer rt.limMu.Unlock()
	rt.ratePerSec = perSec
	rt.limiters = nil
}

/*
 * limiterFor returns the limiter for a URL's *host*, not for the plugin.
 *
 * The thing being protected is somebody else's API, and its budget is spent
 * against this server's IP address regardless of which plugin spends it. Two
 * plugins granted api.example.com share one allowance, because api.example.com
 * sees one caller.
 *
 * The cost is that a busy plugin can make a quiet one wait on a host they
 * share. That is the right way round: the alternative is n plugins each
 * politely taking 5 requests a second and the remote seeing 5n.
 *
 * Keyed on the hostname alone. A port or a path would let a plugin mint fresh
 * budgets by varying either, which is a rate limit in name only.
 */
func (rt *Runtime) limiterFor(raw string) *meta.Limiter {
	host := hostOf(raw)
	rt.limMu.Lock()
	defer rt.limMu.Unlock()
	if rt.limiters == nil {
		rt.limiters = map[string]*meta.Limiter{}
	}
	l, ok := rt.limiters[host]
	if !ok {
		rate := rt.ratePerSec
		if rate <= 0 {
			rate = defaultRatePerSec
		}
		l = meta.NewLimiter(rate, int(rate)+1)
		rt.limiters[host] = l
	}
	return l
}

/*
 * cacheKey namespaces a cached response by plugin, and hashes the URL.
 *
 * Two separate reasons, and both matter.
 *
 * Namespaced by plugin, because a plugin's URL usually carries its own API key
 * and the response is what that key bought. Two plugins granted the same host
 * must not read each other's authenticated answers, and a shared key space
 * would mean the first one to ask decides what the second one sees.
 *
 * Hashed, because the URL *is* the secret. The native clients build their cache
 * key before adding the api_key parameter, so it never reaches the column; the
 * host only ever sees a plugin's URL fully formed, key included. Storing that
 * verbatim would write the user's TMDB key into the database in plain text, in
 * a table nobody thinks of as holding credentials.
 */
func cacheKey(pluginName, raw string) (provider, key string) {
	sum := sha256.Sum256([]byte(raw))
	return "plugin:" + pluginName, hex.EncodeToString(sum[:])
}

/*
 * redactURL keeps the scheme, host and path and drops the query.
 *
 * Plugin URLs are logged when a fetch is denied or fails, and a plugin's query
 * string is where its API key lives — `?apikey=…` for OMDb, `?api_key=…` for
 * TMDB. Every one of those log lines was writing the user's key into
 * lancastd.log, which is a file people paste into bug reports.
 *
 * Nothing in the query helps diagnose either message. "Denied" is answered by
 * the host, and "failed" by the path and the error.
 */
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable url)"
	}
	hadQuery := u.RawQuery != ""
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	if hadQuery {
		// Say a query was there and withheld, rather than silently showing a
		// URL that is not the one that was fetched.
		return u.String() + "?…"
	}
	return u.String()
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
