//go:build windows

package webview2

import (
	"image"
	"unsafe"

	"lancast/internal/webview2/w32"

	"golang.org/x/sys/windows"
)

/*
 * The taskbar thumbnail and the peek preview. LOCAL ADDITION — see
 * PROVENANCE.md.
 *
 * Windows draws a window's taskbar thumbnail from the window's own redirection
 * surface, and this window's own surface holds nothing: the page is drawn by
 * WebView2 in a child window that belongs to another process, and a film or a
 * game in popups of their own (overlay.go). So the thumbnail was a white
 * rectangle. Measured, not supposed: PrintWindow of the main window with no
 * flags — what that surface holds — came back 97% white, and the same call
 * with PW_RENDERFULLCONTENT, which renders composited content too, came back
 * as the page.
 *
 * So the window tells DWM it will supply its own pictures (an "iconic"
 * representation), and supplies them from PW_RENDERFULLCONTENT captures: the
 * page, then whichever picture windows are on screen over it, each where it
 * sits. A film playing full is its picture, a docked film is the page with
 * the picture in its corner, a game is the game.
 *
 * DWM caches what it is given, so a timer invalidates it every few seconds;
 * that costs nothing until somebody looks, because DWM only asks again when a
 * thumbnail is about to be shown. A minimised window has no surface to
 * capture, so the last capture before minimising is kept and shown instead.
 */

var (
	dwmapi                         = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute      = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmSetIconicThumbnail      = dwmapi.NewProc("DwmSetIconicThumbnail")
	procDwmSetIconicLivePreviewBmp = dwmapi.NewProc("DwmSetIconicLivePreviewBitmap")
	procDwmInvalidateIconicBitmaps = dwmapi.NewProc("DwmInvalidateIconicBitmaps")
	user32thumb                    = windows.NewLazySystemDLL("user32.dll")
	procPrintWindow                = user32thumb.NewProc("PrintWindow")
	procGetWindowRectThumb         = user32thumb.NewProc("GetWindowRect")
	procGetDCThumb                 = user32thumb.NewProc("GetDC")
	procReleaseDCThumb             = user32thumb.NewProc("ReleaseDC")
	gdi32thumb                     = windows.NewLazySystemDLL("gdi32.dll")
	procCreateCompatibleDC         = gdi32thumb.NewProc("CreateCompatibleDC")
	procCreateDIBSection           = gdi32thumb.NewProc("CreateDIBSection")
	procSelectObjectThumb          = gdi32thumb.NewProc("SelectObject")
	procDeleteObjectThumb          = gdi32thumb.NewProc("DeleteObject")
	procDeleteDCThumb              = gdi32thumb.NewProc("DeleteDC")
)

const (
	dwmwaForceIconicRepresentation = 7
	dwmwaHasIconicBitmap           = 10
	wmDwmSendIconicThumbnail       = 0x0323
	wmDwmSendIconicLivePreview     = 0x0326
	wmSysCommand                   = 0x0112
	scMinimize                     = 0xF020
	pwClientOnly                   = 0x1
	pwRenderFullContent            = 0x2
	dwmSitDisplayFrame             = 0x1
	thumbTimer                     = 0x4c54
	thumbInterval                  = 3000
)

// enableIconicPreview asks DWM to take its thumbnail and peek pictures from
// this window rather than from its empty surface.
func (w *webview) enableIconicPreview() {
	on := int32(1)
	for _, attr := range []uintptr{dwmwaForceIconicRepresentation, dwmwaHasIconicBitmap} {
		if r, _, _ := procDwmSetWindowAttribute.Call(w.hwnd, attr, uintptr(unsafe.Pointer(&on)), 4); r != 0 {
			// Not available (no DWM, or a system that refuses): the white
			// thumbnail it was, rather than a window that fails to open.
			return
		}
	}
	w.iconic = true
	_, _, _ = procSetTimer.Call(w.hwnd, thumbTimer, thumbInterval, 0)
}

// thumbnailMessage answers DWM's requests, and reports whether it consumed
// the message.
func (w *webview) thumbnailMessage(msg, wp, lp uintptr) (uintptr, bool) {
	if !w.iconic {
		return 0, false
	}
	switch msg {
	case wmTimer:
		if wp == thumbTimer {
			_, _, _ = procDwmInvalidateIconicBitmaps.Call(w.hwnd)
			return 0, true
		}
	case wmSysCommand:
		// About to minimise: the last moment there is a surface to capture.
		// Not consumed — the minimise still happens.
		if wp&0xFFF0 == scMinimize {
			w.thumbCache = w.composite()
		}
	case wmDwmSendIconicThumbnail:
		maxW, maxH := int(lp>>16&0xFFFF), int(lp&0xFFFF)
		if img := w.previewImage(); img != nil {
			tw, th := fitWithin(img.Bounds().Dx(), img.Bounds().Dy(), maxW, maxH)
			if bmp := toHBITMAP(scaleBox(img, tw, th)); bmp != 0 {
				_, _, _ = procDwmSetIconicThumbnail.Call(w.hwnd, bmp, 0)
				_, _, _ = procDeleteObjectThumb.Call(bmp)
			}
		}
		return 0, true
	case wmDwmSendIconicLivePreview:
		if img := w.previewImage(); img != nil {
			if bmp := toHBITMAP(img); bmp != 0 {
				// The capture is the client area; say where it sits in the
				// window, and let DWM draw the frame around it.
				var wr w32.Rect
				_, _, _ = procGetWindowRectThumb.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
				origin := w32.Point{}
				_, _, _ = procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
				off := w32.Point{X: origin.X - wr.Left, Y: origin.Y - wr.Top}
				_, _, _ = procDwmSetIconicLivePreviewBmp.Call(w.hwnd, bmp, uintptr(unsafe.Pointer(&off)), dwmSitDisplayFrame)
				_, _, _ = procDeleteObjectThumb.Call(bmp)
			}
		}
		return 0, true
	}
	return 0, false
}

// previewImage is what the window looks like now, or the last capture before
// it was minimised.
func (w *webview) previewImage() *image.RGBA {
	if iconic, _, _ := procIsIconic.Call(w.hwnd); iconic != 0 {
		return w.thumbCache
	}
	return w.composite()
}

/*
 * composite is the client area as somebody would see it: the page, then each
 * picture window that is showing, bottom to top — the game, then the film —
 * at its place over the page.
 *
 * The page overlay is left out. It is a transparent window whose captured
 * pixels carry no reliable alpha, so laying it on would cover the picture in
 * black; a thumbnail of a film is better as the film than as the film's
 * controls.
 */
func (w *webview) composite() *image.RGBA {
	base := capture(w.hwnd, pwClientOnly|pwRenderFullContent)
	if base == nil {
		return nil
	}
	origin := w32.Point{}
	_, _, _ = procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
	for _, h := range []uintptr{w.game, w.video} {
		if h == 0 {
			continue
		}
		if vis, _, _ := procIsWindowVisible.Call(h); vis == 0 {
			continue
		}
		var r w32.Rect
		_, _, _ = procGetWindowRectThumb.Call(h, uintptr(unsafe.Pointer(&r)))
		if pic := capture(h, pwRenderFullContent); pic != nil {
			drawAt(base, pic, int(r.Left-origin.X), int(r.Top-origin.Y))
		}
	}
	return base
}

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes, Bits  uint16
	Compression   uint32
	SizeImage     uint32
	XPPM, YPPM    int32
	ClrUsed, Imp  uint32
}

// capture renders one window into an image with PrintWindow, or nil.
func capture(h uintptr, flags uintptr) *image.RGBA {
	var r w32.Rect
	if flags&pwClientOnly != 0 {
		_, _, _ = w32.User32GetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	} else {
		_, _, _ = procGetWindowRectThumb.Call(h, uintptr(unsafe.Pointer(&r)))
	}
	width, height := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if width <= 0 || height <= 0 {
		return nil
	}
	screen, _, _ := procGetDCThumb.Call(0)
	defer procReleaseDCThumb.Call(0, screen)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil
	}
	defer procDeleteDCThumb.Call(mem)
	hdr := bitmapInfoHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, Bits: 32}
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil
	}
	defer procDeleteObjectThumb.Call(bmp)
	old, _, _ := procSelectObjectThumb.Call(mem, bmp)
	ok, _, _ := procPrintWindow.Call(h, mem, flags)
	_, _, _ = procSelectObjectThumb.Call(mem, old)
	if ok == 0 {
		return nil
	}
	src := unsafe.Slice((*byte)(bits), width*height*4)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		// BGRA to RGBA, opaque: a capture's alpha is not a statement about
		// transparency, and DWM wants premultiplied, so opaque is the safe
		// reading.
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = src[i*4+2], src[i*4+1], src[i*4], 255
	}
	return img
}

// toHBITMAP makes a 32-bit top-down DIB section of img for DWM. The caller
// deletes it.
func toHBITMAP(img *image.RGBA) uintptr {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if width <= 0 || height <= 0 {
		return 0
	}
	hdr := bitmapInfoHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, Bits: 32}
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return 0
	}
	dst := unsafe.Slice((*byte)(bits), width*height*4)
	toBGRA(dst, img)
	return bmp
}
