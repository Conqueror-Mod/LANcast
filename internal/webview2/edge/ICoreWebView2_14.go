//go:build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * LOCAL ADDITION, not present upstream. See PROVENANCE.md.
 *
 * ICoreWebView2_14 is the first interface with ServerCertificateErrorDetected,
 * which is how the host -- rather than Chromium's interstitial -- decides what
 * happens when a certificate cannot be verified (review finding R11-3). Without
 * it a pin mismatch was a security warning with a way through it.
 *
 * COM interfaces inherit by extending the vtable, so _14's three methods come
 * after every method of _2 to _13. Only the slot counts of _4 to _13 matter
 * here, since none of them is called; they are named so the count can be
 * checked against the WebView2 SDK's WebView2.h, which is where they came from:
 *
 *	_4  FrameCreated (add, remove), DownloadStarting (add, remove)   4
 *	_5  ClientCertificateRequested (add, remove)                      2
 *	_6  OpenTaskManagerWindow                                         1
 *	_7  PrintToPdf                                                    1
 *	_8  IsMuted (get, put), IsDocumentPlayingAudio (get),
 *	    IsMutedChanged and IsDocumentPlayingAudioChanged (add, remove) 7
 *	_9  IsDefaultDownloadDialogOpenChanged (add, remove),
 *	    IsDefaultDownloadDialogOpen (get), Open/CloseDefaultDownloadDialog,
 *	    DefaultDownloadDialogCornerAlignment and Margin (get, put)    9
 *	_10 BasicAuthenticationRequested (add, remove)                    2
 *	_11 CallDevToolsProtocolMethodForSession,
 *	    ContextMenuRequested (add, remove)                            3
 *	_12 StatusBarTextChanged (add, remove), StatusBarText (get)       3
 *	_13 Profile (get)                                                 1
 *
 * A wrong count calls the wrong method with this handler as its argument, so
 * TestTheVtableReachesInterface14 pins the offset.
 */
type iCoreWebView2_14Vtbl struct {
	iCoreWebView2_3Vtbl
	_ [icoreWebView2_4to13Slots]ComProc

	AddServerCertificateErrorDetected    ComProc
	RemoveServerCertificateErrorDetected ComProc
	ClearServerCertificateErrorActions   ComProc
}

// icoreWebView2_4to13Slots is the sum of the table above.
const icoreWebView2_4to13Slots = 4 + 2 + 1 + 1 + 7 + 9 + 2 + 3 + 3 + 1

type ICoreWebView2_14 struct {
	vtbl *iCoreWebView2_14Vtbl
}

// GetICoreWebView2_14 is nil on a runtime too old to have it (before
// 1.0.1245), which the caller reports rather than fails on.
func (i *ICoreWebView2) GetICoreWebView2_14() *ICoreWebView2_14 {
	var result *ICoreWebView2_14
	iid := NewGUID("{6DAA4F10-4A90-4753-8898-77C5DF534165}")
	_, _, _ = i.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&result)))
	return result
}

// COREWEBVIEW2_SERVER_CERTIFICATE_ERROR_ACTION, from WebView2.h.
const (
	serverCertificateErrorActionAlwaysAllow = 0
	serverCertificateErrorActionCancel      = 1
	serverCertificateErrorActionDefault     = 2
)

type _ICoreWebView2ServerCertificateErrorDetectedEventArgsVtbl struct {
	_IUnknownVtbl
	GetErrorStatus       ComProc
	GetRequestUri        ComProc
	GetServerCertificate ComProc
	GetAction            ComProc
	PutAction            ComProc
	GetDeferral          ComProc
}

type ICoreWebView2ServerCertificateErrorDetectedEventArgs struct {
	vtbl *_ICoreWebView2ServerCertificateErrorDetectedEventArgsVtbl
}

// RequestURI is the address that failed. Empty if it could not be read.
func (a *ICoreWebView2ServerCertificateErrorDetectedEventArgs) RequestURI() string {
	var p *uint16
	_, _, _ = a.vtbl.GetRequestUri.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&p)))
	if p == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p))
	return windows.UTF16PtrToString(p)
}

// Cancel refuses the request: no interstitial, no way through.
func (a *ICoreWebView2ServerCertificateErrorDetectedEventArgs) Cancel() error {
	r, _, _ := a.vtbl.PutAction.Call(uintptr(unsafe.Pointer(a)), serverCertificateErrorActionCancel)
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

type _ICoreWebView2ServerCertificateErrorDetectedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type ICoreWebView2ServerCertificateErrorDetectedEventHandler struct {
	vtbl *_ICoreWebView2ServerCertificateErrorDetectedEventHandlerVtbl
	impl _ICoreWebView2ServerCertificateErrorDetectedEventHandlerImpl
}

type _ICoreWebView2ServerCertificateErrorDetectedEventHandlerImpl interface {
	_IUnknownImpl
	ServerCertificateErrorDetected(sender *ICoreWebView2, args *ICoreWebView2ServerCertificateErrorDetectedEventArgs) uintptr
}

func _ICoreWebView2ServerCertificateErrorDetectedEventHandlerIUnknownQueryInterface(this *ICoreWebView2ServerCertificateErrorDetectedEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}

func _ICoreWebView2ServerCertificateErrorDetectedEventHandlerIUnknownAddRef(this *ICoreWebView2ServerCertificateErrorDetectedEventHandler) uintptr {
	return this.impl.AddRef()
}

func _ICoreWebView2ServerCertificateErrorDetectedEventHandlerIUnknownRelease(this *ICoreWebView2ServerCertificateErrorDetectedEventHandler) uintptr {
	return this.impl.Release()
}

func _ICoreWebView2ServerCertificateErrorDetectedEventHandlerInvoke(this *ICoreWebView2ServerCertificateErrorDetectedEventHandler, sender *ICoreWebView2, args *ICoreWebView2ServerCertificateErrorDetectedEventArgs) uintptr {
	return this.impl.ServerCertificateErrorDetected(sender, args)
}

var _ICoreWebView2ServerCertificateErrorDetectedEventHandlerFn = _ICoreWebView2ServerCertificateErrorDetectedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_ICoreWebView2ServerCertificateErrorDetectedEventHandlerIUnknownQueryInterface),
		NewComProc(_ICoreWebView2ServerCertificateErrorDetectedEventHandlerIUnknownAddRef),
		NewComProc(_ICoreWebView2ServerCertificateErrorDetectedEventHandlerIUnknownRelease),
	},
	NewComProc(_ICoreWebView2ServerCertificateErrorDetectedEventHandlerInvoke),
}

func newICoreWebView2ServerCertificateErrorDetectedEventHandler(impl _ICoreWebView2ServerCertificateErrorDetectedEventHandlerImpl) *ICoreWebView2ServerCertificateErrorDetectedEventHandler {
	return &ICoreWebView2ServerCertificateErrorDetectedEventHandler{
		vtbl: &_ICoreWebView2ServerCertificateErrorDetectedEventHandlerFn,
		impl: impl,
	}
}

/*
 * ServerCertificateErrorDetected refuses every certificate error, always.
 *
 * Not "unless the host says otherwise": there is no case in this app where a
 * certificate that failed both normal validation and the pin should be let
 * through. The pin itself is not an error -- Chromium's
 * --ignore-certificate-errors-spki-list exempts exactly that key before this
 * event is ever raised -- so what reaches here is a key that is neither
 * validly issued nor the one this window pinned. Chromium's own answer was an
 * interstitial with a way through it, which carried the session cookie to the
 * new key (R11-3).
 *
 * Cancelled first, then reported, so a host callback that misbehaves cannot
 * leave the request undecided.
 */
func (e *Chromium) ServerCertificateErrorDetected(_ *ICoreWebView2, args *ICoreWebView2ServerCertificateErrorDetectedEventArgs) uintptr {
	_ = args.Cancel()
	if e.ServerCertificateErrorCallback != nil {
		e.ServerCertificateErrorCallback(args.RequestURI())
	}
	return 0
}
