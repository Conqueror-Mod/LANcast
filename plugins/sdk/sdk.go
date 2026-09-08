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
