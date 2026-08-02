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

// HandleRatings is the boilerplate for a rating_source entrypoint: it decodes
// the {"imdb_id":...} request, calls fn, and encodes the result. A plugin's
// exported `ratings` function is one line over this.
func HandleRatings(input []byte, fn func(imdbID string) []Rating) uint64 {
	var req ratingsRequest
	_ = json.Unmarshal(input, &req)
	out, err := json.Marshal(fn(req.IMDbID))
	if err != nil {
		return 0
	}
	return Return(out)
}
