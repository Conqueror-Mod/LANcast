//go:build windows

package vsync

import (
	"fmt"
	"syscall"
	"unsafe"
)

/*
 * The kernel thunks DWM and Chromium themselves use: open the adapter that
 * drives a GDI display name, then block until that output's next vertical
 * blank. Exported by gdi32 since Windows 7, documented in d3dkmthk.h, and no
 * COM: an IDXGIOutput would do the same through three interfaces.
 */

var (
	gdi32                                   = syscall.NewLazyDLL("gdi32.dll")
	procD3DKMTOpenAdapterFromGdiDisplayName = gdi32.NewProc("D3DKMTOpenAdapterFromGdiDisplayName")
	procD3DKMTWaitForVerticalBlankEvent     = gdi32.NewProc("D3DKMTWaitForVerticalBlankEvent")
	procD3DKMTCloseAdapter                  = gdi32.NewProc("D3DKMTCloseAdapter")
)

// openAdapterFromGdiDisplayName is D3DKMT_OPENADAPTERFROMGDIDISPLAYNAME.
type openAdapterFromGdiDisplayName struct {
	DeviceName    [32]uint16
	Adapter       uint32
	AdapterLuidLo uint32
	AdapterLuidHi int32
	VidPnSourceID uint32
}

// waitForVerticalBlankEvent is D3DKMT_WAITFORVERTICALBLANKEVENT.
type waitForVerticalBlankEvent struct {
	Adapter       uint32
	Device        uint32
	VidPnSourceID uint32
}

// Waiter waits for the vertical blank of one monitor. Not safe for use from
// two goroutines at once; one waiter has one caller.
type Waiter struct {
	adapter uint32
	source  uint32
}

// Open finds the adapter output behind a GDI display name ("\\.\DISPLAY3").
func Open(gdiName string) (*Waiter, error) {
	if err := procD3DKMTOpenAdapterFromGdiDisplayName.Find(); err != nil {
		return nil, ErrUnsupported
	}
	name, err := syscall.UTF16FromString(gdiName)
	if err != nil || len(name) > 32 {
		return nil, fmt.Errorf("vsync: bad display name %q", gdiName)
	}
	var o openAdapterFromGdiDisplayName
	copy(o.DeviceName[:], name)
	if st, _, _ := procD3DKMTOpenAdapterFromGdiDisplayName.Call(uintptr(unsafe.Pointer(&o))); st != 0 {
		return nil, fmt.Errorf("vsync: open %s: NTSTATUS 0x%08x", gdiName, uint32(st))
	}
	return &Waiter{adapter: o.Adapter, source: o.VidPnSourceID}, nil
}

// Wait blocks until the monitor's next vertical blank.
func (w *Waiter) Wait() error {
	e := waitForVerticalBlankEvent{Adapter: w.adapter, VidPnSourceID: w.source}
	if st, _, _ := procD3DKMTWaitForVerticalBlankEvent.Call(uintptr(unsafe.Pointer(&e))); st != 0 {
		return fmt.Errorf("vsync: wait: NTSTATUS 0x%08x", uint32(st))
	}
	return nil
}

// Close releases the adapter. The waiter is unusable afterwards.
func (w *Waiter) Close() {
	if w == nil || w.adapter == 0 {
		return
	}
	a := w.adapter
	w.adapter = 0
	_, _, _ = procD3DKMTCloseAdapter.Call(uintptr(unsafe.Pointer(&a)))
}
