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

// httpget treats its input as a URL and returns the host's response bytes.
//
// Named httpget rather than fetch because `fetch` is the provider entrypoint in
// the ABI, and the fixture plays both kinds.
//
//go:wasmexport httpget
func httpget(ptr, length uint32) uint64 {
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

	/*
	 * "fail" asks the fixture to report a failure, so the host side can be
	 * tested against the answer ABI 1 could not give. Everything else is a
	 * success, wrapped in the ABI 2 envelope.
	 */
	if req.IMDbID == "fail" {
		out, err := json.Marshal(map[string]any{"error": "the fixture was asked to fail"})
		if err != nil {
			return 0
		}
		return ret(out)
	}

	resp := []map[string]any{
		{"source": "imdb", "score": 7.9, "display": req.IMDbID, "votes": 42},
	}
	out, err := json.Marshal(map[string]any{"result": resp})
	if err != nil {
		return 0
	}
	return ret(out)
}

/*
 * The provider entrypoints (ADR 0063).
 *
 * Between them they prove what the host adapter needs proving: that a second
 * export needs nothing new from the ABI, that a candidate crosses the boundary
 * with its fields intact, that a nil Fields pointer stays nil, and that an
 * image URL outside the manifest grant is dropped by the host rather than by
 * the guest choosing to behave.
 */

// search returns two candidates, one of which carries a poster on a host the
// fixture manifest does not grant. "fail" asks for a reported failure.
//
//go:wasmexport search
func search(ptr, length uint32) uint64 {
	var q struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Year  int    `json:"year"`
	}
	_ = json.Unmarshal(bytesAt(ptr, length), &q)
	if q.Title == "fail" {
		return envelope(map[string]any{"error": "the fixture was asked to fail"})
	}
	if q.Title == "nothing" {
		return envelope(map[string]any{"result": []any{}})
	}
	return envelope(map[string]any{"result": []map[string]any{
		{
			"external_id": "ext-1",
			"kind":        q.Kind,
			"title":       q.Title,
			"year":        q.Year,
			"popularity":  3.5,
			"poster_url":  "https://example.test/poster.jpg",
		},
		{
			"external_id": "ext-2",
			"kind":        q.Kind,
			"title":       q.Title + " (again)",
			"poster_url":  "https://evil.test/beacon.jpg",
		},
		// No external_id: the host has no way to fetch this one, so it drops it.
		{"kind": q.Kind, "title": "unfetchable"},
	}})
}

// fetch returns one record. Overview is deliberately absent rather than empty,
// so the host side can prove a nil pointer survives the crossing.
//
//go:wasmexport fetch
func fetch(ptr, length uint32) uint64 {
	var ref struct {
		Kind       string `json:"kind"`
		ExternalID string `json:"external_id"`
	}
	_ = json.Unmarshal(bytesAt(ptr, length), &ref)
	if ref.ExternalID == "fail" {
		return envelope(map[string]any{"error": "the fixture was asked to fail"})
	}
	if ref.ExternalID == "missing" {
		return envelope(map[string]any{})
	}
	return envelope(map[string]any{"result": map[string]any{
		"external_id": ref.ExternalID,
		"kind":        ref.Kind,
		"imdb_id":     "tt0000001",
		"fields": map[string]any{
			"title": "A Fixture Film",
			"year":  1999,
		},
		"genres": []string{"Drama"},
		"credits": []map[string]any{
			{"name": "A Person", "role": "actor", "image": "https://example.test/face.jpg"},
			{"name": "B Person", "role": "director", "image": "https://evil.test/face.jpg"},
			{"name": "", "role": "actor"},
		},
		"artwork": []map[string]any{
			{"kind": "poster", "url": "https://example.test/poster.jpg"},
			{"kind": "fanart", "url": "https://evil.test/fanart.jpg"},
		},
		"collection": map[string]any{
			"external_id": "col-1",
			"name":        "A Fixture Collection",
			"artwork":     []map[string]any{{"kind": "poster", "url": "https://evil.test/col.jpg"}},
		},
		"keywords": []map[string]any{{"id": 7, "name": "fixture"}},
	}})
}

// envelope marshals an ABI 2 response and packs it.
func envelope(v map[string]any) uint64 {
	out, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return ret(out)
}
