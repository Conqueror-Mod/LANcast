//go:build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * LOCAL ADDITION, not present upstream. See PROVENANCE.md.
 *
 * NavigationStarting lets the host refuse a top-level navigation before it
 * happens (review finding R11-2). Upstream registered no handler, so a page
 * could send the main frame anywhere, and the bindings -- injected into every
 * document -- went with it.
 */

type _ICoreWebView2NavigationStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetCancel          ComProc
	PutCancel          ComProc
	GetNavigationId    ComProc
}

type ICoreWebView2NavigationStartingEventArgs struct {
	vtbl *_ICoreWebView2NavigationStartingEventArgsVtbl
}

// URI is where the navigation is going. Empty if it could not be read.
func (a *ICoreWebView2NavigationStartingEventArgs) URI() string {
	var p *uint16
	_, _, _ = a.vtbl.GetUri.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&p)))
	if p == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p))
	return windows.UTF16PtrToString(p)
}

// Cancel stops the navigation. put_Cancel takes a BOOL by value.
func (a *ICoreWebView2NavigationStartingEventArgs) Cancel() {
	_, _, _ = a.vtbl.PutCancel.Call(uintptr(unsafe.Pointer(a)), 1)
}

type _ICoreWebView2NavigationStartingEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type ICoreWebView2NavigationStartingEventHandler struct {
	vtbl *_ICoreWebView2NavigationStartingEventHandlerVtbl
	impl _ICoreWebView2NavigationStartingEventHandlerImpl
}

type _ICoreWebView2NavigationStartingEventHandlerImpl interface {
	_IUnknownImpl
	NavigationStarting(sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr
}

func _ICoreWebView2NavigationStartingEventHandlerIUnknownQueryInterface(this *ICoreWebView2NavigationStartingEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}

func _ICoreWebView2NavigationStartingEventHandlerIUnknownAddRef(this *ICoreWebView2NavigationStartingEventHandler) uintptr {
	return this.impl.AddRef()
}

func _ICoreWebView2NavigationStartingEventHandlerIUnknownRelease(this *ICoreWebView2NavigationStartingEventHandler) uintptr {
	return this.impl.Release()
}

func _ICoreWebView2NavigationStartingEventHandlerInvoke(this *ICoreWebView2NavigationStartingEventHandler, sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr {
	return this.impl.NavigationStarting(sender, args)
}

var _ICoreWebView2NavigationStartingEventHandlerFn = _ICoreWebView2NavigationStartingEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_ICoreWebView2NavigationStartingEventHandlerIUnknownQueryInterface),
		NewComProc(_ICoreWebView2NavigationStartingEventHandlerIUnknownAddRef),
		NewComProc(_ICoreWebView2NavigationStartingEventHandlerIUnknownRelease),
	},
	NewComProc(_ICoreWebView2NavigationStartingEventHandlerInvoke),
}

func newICoreWebView2NavigationStartingEventHandler(impl _ICoreWebView2NavigationStartingEventHandlerImpl) *ICoreWebView2NavigationStartingEventHandler {
	return &ICoreWebView2NavigationStartingEventHandler{
		vtbl: &_ICoreWebView2NavigationStartingEventHandlerFn,
		impl: impl,
	}
}

// NavigationStarting asks the host whether a top-level navigation may
// proceed, and cancels it if not. No callback is upstream's behaviour.
func (e *Chromium) NavigationStarting(_ *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr {
	if e.NavigationStartingCallback != nil && !e.NavigationStartingCallback(args.URI()) {
		args.Cancel()
	}
	return 0
}
