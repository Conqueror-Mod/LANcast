package plugin

import (
	"context"
	"errors"

	"github.com/tetratelabs/wazero/api"
)

// The ABI is length-prefixed bytes over the module's linear memory. A value
// crossing the boundary is a (pointer, length) pair; a single return value packs
// both into one u64 as (ptr<<32)|len, since WASM functions return scalars. JSON
// rides inside these byte spans — legible and debuggable for the first ABI; a
// tighter encoding is a later change behind the ABI version.

// packPtrLen encodes a pointer and length into the single u64 a WASM export
// returns.
func packPtrLen(ptr, length uint32) uint64 {
	return uint64(ptr)<<32 | uint64(length)
}

// unpackPtrLen splits the u64 an export returned.
func unpackPtrLen(v uint64) (ptr, length uint32) {
	return uint32(v >> 32), uint32(v)
}

// guestAlloc reserves length bytes inside the module and copies data in, using
// the module's own exported allocator so the guest owns (and pins) the memory.
// Returns the guest pointer.
func guestAlloc(ctx context.Context, mod api.Module, data []byte) (uint32, error) {
	alloc := mod.ExportedFunction("alloc")
	if alloc == nil {
		return 0, errors.New("plugin exports no alloc")
	}
	res, err := alloc.Call(ctx, uint64(len(data)))
	if err != nil {
		return 0, err
	}
	if len(res) == 0 {
		return 0, errors.New("alloc returned nothing")
	}
	ptr := api.DecodeU32(res[0])
	if !mod.Memory().Write(ptr, data) {
		return 0, errors.New("write to guest memory failed")
	}
	return ptr, nil
}

// readPacked reads the byte span a packed (ptr,len) return points at. A zero
// length is a valid empty result.
func readPacked(mod api.Module, packed uint64) ([]byte, bool) {
	ptr, length := unpackPtrLen(packed)
	if length == 0 {
		return nil, true
	}
	return mod.Memory().Read(ptr, length)
}

// writeToGuest allocates and copies host data into the module and returns the
// packed (ptr,len) a host function hands back to the guest. Zero on failure or
// empty, which the guest reads as "no data".
func writeToGuest(ctx context.Context, mod api.Module, data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}
	ptr, err := guestAlloc(ctx, mod, data)
	if err != nil {
		return 0
	}
	return packPtrLen(ptr, uint32(len(data)))
}
