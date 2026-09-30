//go:build windows

package clientwindow

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * Per-monitor DPI awareness.
 *
 * The client declared none, so Windows treated it as DPI-unaware: it drew
 * every window at 96 DPI and stretched the bitmap onto whatever monitor the
 * window was on. For one window that is only blur. For this client it is
 * wrong, because native video is not one window -- the picture and the page
 * overlay are owned popups beside the main window (webview2/overlay.go), and
 * Windows scales and places each top-level window separately. On a desk with
 * two monitors at different scaling, moving the app across put the docked
 * picture somewhere off to the side of the window, travelling with it at its
 * own offset, and a full-size picture came out a pixel wider than the screen
 * it was on, showing as a line on the next one. Both reported together.
 *
 * Per-monitor V2 is what Windows and WebView2 both expect of a modern desktop
 * app: every coordinate is a real pixel on the monitor it is on, the page's
 * devicePixelRatio is that monitor's scale, and the rectangle the page sends
 * for the docked picture (device pixels, nativeLayout.ts) means what the popup
 * is placed with. Windows also scales the title bar and frame itself under V2.
 *
 * It has to be set before the process creates any window at all -- the tray
 * included -- which is why cmd/lancast calls this from an init function rather
 * than from Open.
 */

var (
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")

	shcore                     = windows.NewLazySystemDLL("shcore.dll")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
)

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, a pseudo-handle of -4.
const dpiAwarenessPerMonitorV2 = ^uintptr(3)

// processPerMonitorDPIAware is PROCESS_PER_MONITOR_DPI_AWARE for shcore, the
// Windows 8.1 fallback.
const processPerMonitorDPIAware = 2

// SetDPIAware makes this process per-monitor DPI aware. Call it before any
// window exists. Failure is not fatal -- the app still works, stretched -- so
// the caller logs it and carries on.
func SetDPIAware() error {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		ok, _, err := procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
		switch {
		case ok != 0:
			return nil
		case errors.Is(err, windows.ERROR_ACCESS_DENIED):
			// Already set, by a manifest or an earlier call. Nothing to do.
			return nil
		case !errors.Is(err, windows.ERROR_INVALID_PARAMETER):
			return err
		}
		// Invalid parameter: a Windows that knows the call but not V2.
	}
	// Before Windows 10 1703 there is no V2; per-monitor V1 still gets real
	// pixels, without Windows scaling the frame for us.
	if procSetProcessDpiAwareness.Find() != nil {
		return errors.New("clientwindow: no DPI awareness API on this Windows")
	}
	if hr, _, _ := procSetProcessDpiAwareness.Call(processPerMonitorDPIAware); hr != 0 {
		return windows.Errno(hr)
	}
	return nil
}

// scaleForMonitor resizes a freshly created window from 96-DPI units to its
// monitor's scale and centres it there.
//
// Only for a window with no remembered placement. Being DPI aware, the window
// is created at exactly the pixels asked for, so the 1280x800 default would
// open at two-thirds size on a 150% monitor.
func scaleForMonitor(hwnd uintptr, width, height int) {
	if procGetDpiForWindow.Find() != nil {
		return
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 || dpi == 96 {
		return
	}
	w := width * int(dpi) / 96
	h := height * int(dpi) / 96

	const monitorDefaultToNearest = 2
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	var mi monitorInfoEx
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	work := mi.RcWork
	ww, wh := int(work.Right-work.Left), int(work.Bottom-work.Top)
	if w > ww {
		w = ww
	}
	if h > wh {
		h = wh
	}
	x := int(work.Left) + (ww-w)/2
	y := int(work.Top) + (wh-h)/2
	const swpNoZOrder = 0x0004
	_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoZOrder|swpNoActivate)
}
