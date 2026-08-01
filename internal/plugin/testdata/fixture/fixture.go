// Command fixture is a minimal guest plugin used only by the plugin runtime
// tests. It exercises the ABI (alloc + packed returns) and each host function
// (log, http_get, secret) so the host side can be tested without a real plugin.
//
// It has its own module (go.mod) so the main build never compiles it, and it is
// built to ../fixture.wasm by build.sh. The committed .wasm is what the tests
// load — CI needs no wasm toolchain — and this source plus build.sh keep it
// reproducible.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../fixture.wasm .
package main

import (
	"encoding/json"
	"unsafe"
)

func main() {}

// pinned keeps allocated buffers alive across the call boundary so Go's GC does
// not reclaim memory the host still holds a pointer into.
var pinned = map[uintptr][]byte{}

//go:wasmexport alloc
func alloc(size uint32) uint32 {
	b := make([]byte, size)
	p := uintptr(unsafe.Pointer(&b[0]))
	pinned[p] = b
	return uint32(p)
}

func bytesAt(ptr, length uint32) []byte {
	if length == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

// ret copies b into a pinned buffer and packs its (ptr,len) for return.
func ret(b []byte) uint64 {
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

func fromHost(packed uint64) []byte {
	ptr, length := uint32(packed>>32), uint32(packed)
	return bytesAt(ptr, length)
}

// echo returns its input unchanged — the ABI round-trip.
//
//go:wasmexport echo
func echo(ptr, length uint32) uint64 {
	return ret(bytesAt(ptr, length))
}

// fetch treats its input as a URL and returns the host's response bytes.
//
//go:wasmexport fetch
func fetch(ptr, length uint32) uint64 {
	return ret(fromHost(hostHTTPGet(ptr, length)))
}

// getsecret treats its input as a secret name and returns the host's value.
//
//go:wasmexport getsecret
func getsecret(ptr, length uint32) uint64 {
	return ret(fromHost(hostSecret(ptr, length)))
}

// logit logs its input and returns it, so a test can confirm logging does not
// disturb the return path.
//
//go:wasmexport logit
func logit(ptr, length uint32) uint64 {
	hostLog(1, ptr, length)
	return ret(bytesAt(ptr, length))
}

// ratings is the rating_source entrypoint. It unmarshals {"imdb_id":...} and
// returns one rating whose display echoes the id — enough for the host adapter
// test to prove the request reached the guest and the response marshalled back.
//
//go:wasmexport ratings
func ratings(ptr, length uint32) uint64 {
	var req struct {
		IMDbID string `json:"imdb_id"`
	}
	_ = json.Unmarshal(bytesAt(ptr, length), &req)
	resp := []map[string]any{
		{"source": "imdb", "score": 7.9, "display": req.IMDbID, "votes": 42},
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return 0
	}
	return ret(out)
}
