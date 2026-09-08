// Package sdk is the guest-side SDK for LANcast WebAssembly plugins (ADR 0020).
//
// It hides the ABI — the alloc/pin dance and the packed (ptr,len) returns — and
// wraps the host functions behind ordinary Go calls, so a plugin author writes
// against types, not linear memory. The host side of these shapes lives in
// internal/plugin; the two must agree on the JSON and the function names.
package sdk

import (
	"encoding/json"
	"unsafe"
)

// pinned keeps buffers that cross the boundary alive so Go's GC does not reclaim
// memory the host holds a pointer into. A fresh module instance per call means
// this never accumulates across calls.
var pinned = map[uintptr][]byte{}

// Alloc reserves size bytes for the host to write into. A plugin re-exports this
// as //go:wasmexport alloc.
func Alloc(size uint32) uint32 {
	b := make([]byte, size)
	p := uintptr(unsafe.Pointer(&b[0]))
	pinned[p] = b
	return uint32(p)
}

// Input views the bytes the host passed at (ptr, length).
func Input(ptr, length uint32) []byte {
	if length == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

// Return pins a copy of b and packs its (ptr,len) for a wasm export to return.
func Return(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	out := make([]byte, len(b))
	copy(out, b)
	p := uintptr(unsafe.Pointer(&out[0]))
	pinned[p] = out
	return uint64(p)<<32 | uint64(len(out))
}

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_http_get
func hostHTTPGet(ptr, length uint32) uint64

//go:wasmimport env host_secret
func hostSecret(ptr, length uint32) uint64

// send pins b in guest memory and returns its (ptr,len) for a host call.
func send(b []byte) (uint32, uint32) {
	if len(b) == 0 {
		return 0, 0
	}
	p := uintptr(unsafe.Pointer(&b[0]))
	pinned[p] = b
	return uint32(p), uint32(len(b))
}

// recv copies the bytes a host function returned into a fresh slice.
func recv(packed uint64) []byte {
	ptr, length := uint32(packed>>32), uint32(packed)
	src := Input(ptr, length)
	if len(src) == 0 {
		return nil
	}
	out := make([]byte, len(src))
	copy(out, src)
	return out
}

// Log emits a diagnostic line, attributed to this plugin by the host.
func Log(msg string) {
	p, l := send([]byte(msg))
	if l == 0 {
		return
	}
	hostLog(1, p, l)
}

// HTTPGet fetches a URL through the host. It succeeds only for hosts the
// plugin's manifest declared; a denied or failed fetch returns nil.
func HTTPGet(url string) []byte {
	p, l := send([]byte(url))
	return recv(hostHTTPGet(p, l))
}

// Secret returns a configured secret the manifest granted by name, or "".
func Secret(name string) string {
	p, l := send([]byte(name))
	return string(recv(hostSecret(p, l)))
}

// Rating is one score a rating_source plugin returns. It mirrors the host's
// meta.Rating; the host normalizes nothing further.
type Rating struct {
	Source  string  `json:"source"`
	Score   float64 `json:"score"`
	Display string  `json:"display"`
	Votes   int     `json:"votes"`
}

type ratingsRequest struct {
	IMDbID string `json:"imdb_id"`
}

/*
 * envelope is the ABI 2 response shape (ADR 0063).
 *
 * Before it, a guest had no way to say a call went wrong: returning nothing was
 * how you said "no results", so an upstream that was down looked exactly like
 * one that had nothing. For a rating source that meant no scores until
 * tomorrow. For a provider it would mean the host writing down "unmatched",
 * which it does not re-ask, from a failure that was temporary.
 */
type envelope struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Ok packs a successful result.
func Ok(v any) uint64 {
	b, err := json.Marshal(envelope{Result: v})
	if err != nil {
		return Fail("could not encode the result")
	}
	return Return(b)
}

/*
 * Fail packs a failure, in words.
 *
 * The message reaches the host's log with this plugin's name against it, so it
 * is worth writing for somebody reading that log at the time — "omdb rejected
 * the key" rather than "error 3".
 */
func Fail(msg string) uint64 {
	b, err := json.Marshal(envelope{Error: msg})
	if err != nil {
		return 0
	}
	return Return(b)
}

/*
 * HandleRatings is the boilerplate for a rating_source entrypoint: it decodes
 * the {"imdb_id":...} request, calls fn, and packs whichever of the two answers
 * fn gave.
 *
 * fn returns an error as well as ratings, which is the ABI 2 change an author
 * sees. Returning no ratings and no error means "looked, found nothing"; an
 * error means "could not look" — and the host treats those differently.
 */
func HandleRatings(input []byte, fn func(imdbID string) ([]Rating, error)) uint64 {
	var req ratingsRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return Fail("could not read the request")
	}
	out, err := fn(req.IMDbID)
	if err != nil {
		return Fail(err.Error())
	}
	return Ok(out)
}

/*
 * ---------------------------------------------------------------------------
 * provider (ADR 0063)
 *
 * The second plugin shape: two exports, search and fetch, mirroring the host's
 * meta.Provider. These types mirror the wire shapes in internal/plugin's
 * provider.go, and the two must agree on the JSON.
 *
 * Two fields an author might expect are deliberately absent.
 *
 * A Candidate has no score. Ranking is the host's, and a plugin that could
 * score its own candidates could promote itself over the built-in providers —
 * match confidence is the one number the library's identity rests on. Give the
 * host good candidates and let it choose.
 *
 * A Record has no source. The host fills that with this plugin's manifest name,
 * so a plugin cannot attribute its answers to somebody else.
 */

// Query is a search request, built from what the scanner guessed about a file.
// Year is 0 when it could not be read.
type Query struct {
	Kind    string `json:"kind"` // movie | show | season | episode
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Series  string `json:"series,omitempty"`
	Season  int    `json:"season,omitempty"`
	Episode int    `json:"episode,omitempty"`
}

// Candidate is one possible match. ExternalID is required — it is what the host
// passes back to Fetch, so a candidate without one is dropped.
type Candidate struct {
	ExternalID string  `json:"external_id"`
	Kind       string  `json:"kind"`
	Title      string  `json:"title"`
	Year       int     `json:"year,omitempty"`
	Overview   string  `json:"overview,omitempty"`
	Popularity float64 `json:"popularity,omitempty"`
	PosterURL  string  `json:"poster_url,omitempty"`
}

// Ref identifies one record to fetch.
type Ref struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
}

/*
 * Fields are the per-field values, and every one is a pointer on purpose.
 *
 * Nil means "this source has nothing to say about this field". A pointer to the
 * zero value means "this source says it is empty". The host merges field by
 * field across sources, so those are different answers and collapsing them
 * would let a silent plugin overwrite a better source's overview with nothing.
 *
 * Use Str, Int, Num and Int64 to build them.
 */
type Fields struct {
	Title         *string  `json:"title,omitempty"`
	SortTitle     *string  `json:"sort_title,omitempty"`
	Year          *int     `json:"year,omitempty"`
	Overview      *string  `json:"overview,omitempty"`
	Rating        *float64 `json:"rating,omitempty"`
	ContentRating *string  `json:"content_rating,omitempty"`
	ReleasedAt    *int64   `json:"released_at,omitempty"`
	DurationMS    *int64   `json:"duration_ms,omitempty"`
	Series        *string  `json:"series,omitempty"`
	Season        *int     `json:"season,omitempty"`
	Episode       *int     `json:"episode,omitempty"`
}

// Str, Int, Num and Int64 make the pointers Fields wants. They exist because
// &"text" is not legal Go, and a local variable per field turns a record into
// three times the code it should be.
func Str(v string) *string   { return &v }
func Int(v int) *int         { return &v }
func Num(v float64) *float64 { return &v }
func Int64(v int64) *int64   { return &v }

// Art is an image the plugin knows about but has not downloaded — the host
// fetches it. Kind is poster, fanart or thumb.
//
// The URL must be on a host this plugin's manifest granted, the same list
// HTTPGet is checked against. One outside it is dropped and logged: an artwork
// URL makes the *host* fetch something, so it is a capability like any other.
type Art struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// Credit is one person's involvement. Role is actor, director or writer.
type Credit struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Character string `json:"character,omitempty"`
	Order     int    `json:"order,omitempty"`
	Image     string `json:"image,omitempty"`
}

// Collection is a franchise or series this record belongs to. It is membership,
// not containment: the item stays a top-level work and this only names the set.
type Collection struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Artwork    []Art  `json:"artwork,omitempty"`
}

// Keyword is one of the source's free tags. The host uses them to find
// groupings a single franchise field cannot express, and does not store them.
type Keyword struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name"`
}

// Record is one item in full.
type Record struct {
	ExternalID string      `json:"external_id"`
	Kind       string      `json:"kind"`
	IMDbID     string      `json:"imdb_id,omitempty"`
	Fields     Fields      `json:"fields"`
	Genres     []string    `json:"genres,omitempty"`
	Credits    []Credit    `json:"credits,omitempty"`
	Artwork    []Art       `json:"artwork,omitempty"`
	Collection *Collection `json:"collection,omitempty"`
	Keywords   []Keyword   `json:"keywords,omitempty"`
}

/*
 * HandleSearch is the boilerplate for the search entrypoint.
 *
 * Returning no candidates and no error means "I looked and there is no such
 * title" — the host records the item as unmatched and stops asking. Returning
 * an error means "I could not look", and it asks again later. Getting that
 * round the wrong way is how a temporary outage becomes a permanent answer, and
 * it is the reason ABI 2 exists.
 */
func HandleSearch(input []byte, fn func(q Query) ([]Candidate, error)) uint64 {
	var q Query
	if err := json.Unmarshal(input, &q); err != nil {
		return Fail("could not read the search request")
	}
	out, err := fn(q)
	if err != nil {
		return Fail(err.Error())
	}
	return Ok(out)
}

// HandleFetch is the boilerplate for the fetch entrypoint. A nil record with no
// error means there is no such record; an error means it could not be fetched.
func HandleFetch(input []byte, fn func(ref Ref) (*Record, error)) uint64 {
	var ref Ref
	if err := json.Unmarshal(input, &ref); err != nil {
		return Fail("could not read the fetch request")
	}
	rec, err := fn(ref)
	if err != nil {
		return Fail(err.Error())
	}
	if rec == nil {
		return Ok(nil)
	}
	return Ok(rec)
}
