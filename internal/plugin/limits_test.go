package plugin

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
)

/*
 * The limits a plugin runs under (R10-4): a deadline it cannot ignore, a memory
 * ceiling it cannot raise, and inputs from outside it that are bounded before
 * they are held.
 *
 * The modules here are assembled by hand, a few dozen bytes each, so the test
 * shows exactly what the guest does and needs no toolchain to rebuild.
 */

// wasmSection frames one section: its id, then its contents' length. Every
// length here is under 128, so a LEB128 size is one byte.
func wasmSection(id byte, body ...byte) []byte {
	return append([]byte{id, byte(len(body))}, body...)
}

// wasmName is a length-prefixed export name.
func wasmName(s string) []byte { return append([]byte{byte(len(s))}, s...) }

/*
 * limitsModule exports memory, `alloc` (returns 0), `ratings` (which the
 * rating-source kind requires, and which is `body`) and `spin`, the same
 * function under the name the test calls it by. The memory's minimum is
 * minPages.
 *
 * body is the code of a function typed (i32, i32) -> i64, without its locals
 * declaration or end.
 */
func limitsModule(minPages uint16, body []byte) []byte {
	m := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	m = append(m, wasmSection(1, // types
		0x02,
		0x60, 0x01, 0x7f, 0x01, 0x7f, // (i32) -> i32
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, // (i32, i32) -> i64
	)...)
	m = append(m, wasmSection(3, 0x02, 0x00, 0x01)...) // two functions
	// One memory, minimum minPages as a two-byte LEB128 (it may be over 127).
	m = append(m, wasmSection(5, 0x01, 0x00, byte(minPages&0x7f)|0x80, byte(minPages>>7))...)
	var exp []byte
	exp = append(exp, 0x04)
	exp = append(append(exp, wasmName("memory")...), 0x02, 0x00)
	exp = append(append(exp, wasmName("alloc")...), 0x00, 0x00)
	exp = append(append(exp, wasmName("ratings")...), 0x00, 0x01)
	exp = append(append(exp, wasmName("spin")...), 0x00, 0x01)
	m = append(m, wasmSection(7, exp...)...)
	alloc := []byte{0x00, 0x41, 0x00, 0x0b} // no locals; i32.const 0; end
	fn := append(append([]byte{0x00}, body...), 0x0b)
	code := []byte{0x02, byte(len(alloc))}
	code = append(code, alloc...)
	code = append(append(code, byte(len(fn))), fn...)
	return append(m, wasmSection(10, code...)...)
}

// spinBody loops for ever: loop; br 0; end — then an i64 that is never reached.
var spinBody = []byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x42, 0x00}

func loadLimitsModule(t *testing.T, wasm []byte) (*Plugin, error) {
	t.Helper()
	ctx := context.Background()
	rt, err := NewRuntime(ctx, quietLog())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(func() { rt.Close(ctx) })
	m := Manifest{Name: "limits", Version: "0.0.1", ABI: ABIVersion, Kind: KindRatingSource}
	return rt.Load(ctx, m, wasm)
}

// The finding's proving test, kept: a plugin that never returns comes back
// within its deadline, as an error. Before the fix it was still running four
// seconds after a one-second deadline, and would have run for ever.
func TestLoopingPluginReturnsWithinDeadline(t *testing.T) {
	p, err := loadLimitsModule(t, limitsModule(1, spinBody))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := p.Call(ctx, "spin", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a call that never returns reported success")
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Errorf("returned after %v, want close to the 1 s deadline", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still running 4 s after its 1 s deadline expired")
	}
}

// A caller with no deadline of its own — the enricher passes a long-lived
// context — still gets one: Call sets its own.
func TestLoopingPluginStopsWithoutACallerDeadline(t *testing.T) {
	saved := callTimeout
	callTimeout = 500 * time.Millisecond
	t.Cleanup(func() { callTimeout = saved })

	p, err := loadLimitsModule(t, limitsModule(1, spinBody))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.Call(context.Background(), "spin", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a call that never returns reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Call set no deadline of its own: still running after 5 s")
	}
}

// A module that declares more memory than the ceiling is refused at load,
// rather than given 4 GiB per instance.
func TestPluginMemoryOverCeilingIsRefused(t *testing.T) {
	if _, err := loadLimitsModule(t, limitsModule(maxMemoryPages+1, spinBody)); err == nil {
		t.Fatalf("a module declaring %d pages loaded; the ceiling is %d",
			maxMemoryPages+1, maxMemoryPages)
	}
	if _, err := loadLimitsModule(t, limitsModule(maxMemoryPages, spinBody)); err != nil {
		t.Fatalf("a module at the ceiling was refused: %v", err)
	}
}

// A module that tries to grow past the ceiling sees the grow fail (-1), as a
// guest's allocator expects, instead of being handed the memory.
func TestPluginMemoryGrowStopsAtCeiling(t *testing.T) {
	// i32.const maxMemoryPages; memory.grow 0; i64.extend_i32_s
	// — returns the previous size (1) on success, -1 on refusal.
	n := uint16(maxMemoryPages)
	grow := []byte{0x41, byte(n&0x7f) | 0x80, byte(n >> 7), 0x40, 0x00, 0xac}
	p, err := loadLimitsModule(t, limitsModule(1, grow))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx := context.Background()
	mod, err := p.rt.wz.InstantiateModule(ctx, p.compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)
	res, err := mod.ExportedFunction("spin").Call(ctx, 0, 0)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := int64(res[0]); got != -1 {
		t.Fatalf("growing from 1 page by %d returned %d; want -1 (refused)", n, got)
	}
}

func TestReadCappedRefusesOverLimit(t *testing.T) {
	if got, err := readCapped(strings.NewReader("12345"), 5); err != nil || string(got) != "12345" {
		t.Fatalf("at the limit: %q, %v", got, err)
	}
	if _, err := readCapped(strings.NewReader("123456"), 5); err == nil {
		t.Fatal("one byte over the limit was accepted")
	}
}

// A bundle member that inflates past the cap is refused, whether its header
// says so or lies about it. The upload cap bounds the compressed bytes only,
// and this runs before any signature is checked. A header that understates the
// size is refused by archive/zip itself once the reader passes it; the cap is
// what holds a header that is honest about being huge.
func TestBundleMemberInflatingPastCapIsRefused(t *testing.T) {
	big := make([]byte, maxBundleEntryBytes+1) // zeros: compresses to almost nothing

	honest := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create(bundleManifest)
		w.Write(testManifestJSON())
		w, _ = zw.Create(bundleWasm)
		w.Write(big)
		zw.Close()
		return buf.Bytes()
	}()

	lying := func() []byte {
		var compressed bytes.Buffer
		fw, _ := flate.NewWriter(&compressed, flate.BestCompression)
		fw.Write(big)
		fw.Close()

		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create(bundleManifest)
		w.Write(testManifestJSON())
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name:               bundleWasm,
			Method:             zip.Deflate,
			CompressedSize64:   uint64(compressed.Len()),
			UncompressedSize64: 8, // the lie
		})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(compressed.Bytes())
		zw.Close()
		return buf.Bytes()
	}()

	for name, data := range map[string][]byte{"honest header": honest, "lying header": lying} {
		if len(data) > 1<<20 {
			t.Fatalf("%s: the bundle is %d bytes; the point is that it is small", name, len(data))
		}
		_, err := VerifyBundle(data, TrustedKeys{})
		if err == nil {
			t.Errorf("%s: a member inflating to %d bytes was accepted", name, len(big))
			continue
		}
		if name == "honest header" && !strings.Contains(err.Error(), "larger than") {
			t.Errorf("%s: VerifyBundle = %v, want a size refusal", name, err)
		}
	}
}
