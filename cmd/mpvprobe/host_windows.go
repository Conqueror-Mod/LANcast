//go:build windows

package main

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * -embed: LANcast's window arrangement, without LANcast.
 *
 * In the app, the main window is borderless and covers the monitor when a film
 * is fullscreen; the picture is an owned popup over it (webview2/overlay.go:
 * WS_POPUP, WS_EX_NOACTIVATE|WS_EX_TOOLWINDOW, a black class brush); and mpv,
 * given that popup as its wid, draws into a child window it makes inside it.
 * This builds the same three windows, so the probe differs from LANcast only
 * by what is not here: the page, its overlay and the stream over HTTP.
 */

var (
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetStockObject   = windows.NewLazySystemDLL("gdi32.dll").NewProc("GetStockObject")
)

const (
	wsPopup         = 0x80000000
	wsVisible       = 0x10000000
	wsExNoActivate  = 0x08000000
	wsExToolWindow  = 0x00000080
	swpNoActivate   = 0x0010
	swpShowWindow   = 0x0040
	pmRemove        = 0x0001
	stockBlackBrush = 4
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	private uint32
}

// createHost makes the main window and the picture window over rc, and
// returns the picture window: what LANcast hands mpv as its wid.
func createHost(rc rect) (uintptr, error) {
	var hinst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinst)
	name, _ := windows.UTF16PtrFromString("mpvprobe-host")
	brush, _, _ := procGetStockObject.Call(stockBlackBrush)
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   procDefWindowProcW.Addr(),
		hInstance:     hinst,
		hbrBackground: windows.Handle(brush),
		lpszClassName: name,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return 0, fmt.Errorf("registering the window class: %w", err)
	}
	x, y := uintptr(rc.Left), uintptr(rc.Top)
	w, h := uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top)

	// The main window, borderless over the monitor as LANcast's is fullscreen.
	owner, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), 0,
		wsPopup|wsVisible, x, y, w, h, 0, 0, uintptr(hinst), 0)
	if owner == 0 {
		return 0, fmt.Errorf("main window: %w", err)
	}
	// The picture: an owned popup, never activated, out of the taskbar.
	video, _, err := procCreateWindowExW.Call(wsExNoActivate|wsExToolWindow,
		uintptr(unsafe.Pointer(name)), 0, wsPopup, x, y, w, h, owner, 0, uintptr(hinst), 0)
	if video == 0 {
		return 0, fmt.Errorf("picture window: %w", err)
	}
	_, _, _ = procSetWindowPos.Call(video, 0, x, y, w, h, swpNoActivate|swpShowWindow)
	return video, nil
}

// pump runs this thread's message loop for d. The host windows belong to this
// thread, and mpv's child window sends its parent messages, so a sleep here
// would leave mpv waiting on a window nobody is servicing.
func pump(d time.Duration) {
	deadline := time.Now().Add(d)
	var m msg
	for time.Now().Before(deadline) {
		for {
			r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
			if r == 0 {
				break
			}
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		time.Sleep(5 * time.Millisecond)
	}
}
