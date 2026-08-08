//go:build windows

// Package loader calls into Microsoft's WebView2Loader.dll — the small shim
// that finds the installed WebView2 runtime and hands back an environment.
//
// **Trimmed from upstream: this loads the DLL from disk only.** The original
// embeds three prebuilt copies of WebView2Loader.dll with go:embed and falls
// back to mapping one into memory with a from-memory PE loader. Both were
// removed, and PROVENANCE.md in the parent directory says why:
//
//   - a 137 KB opaque binary compiled into the shipped client is the thing
//     ADR 0013 refused when it turned down 300 KB of readable JavaScript
//   - loading a DLL from memory is a technique AV and EDR products flag, which
//     for a signed installer landing on someone else's machine is a support
//     problem waiting to happen
//
// Nothing is lost at runtime: upstream tried the on-disk DLL first anyway, so
// this is the path that already ran. The installer places Microsoft's own
// signed WebView2Loader.dll beside the executable, and `windows.NewLazyDLL`
// finds it there.
package loader

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	nativeModule                                       = windows.NewLazyDLL("WebView2Loader")
	nativeCreate                                       = nativeModule.NewProc("CreateCoreWebView2EnvironmentWithOptions")
	nativeCompareBrowserVersions                       = nativeModule.NewProc("CompareBrowserVersions")
	nativeGetAvailableCoreWebView2BrowserVersionString = nativeModule.NewProc("GetAvailableCoreWebView2BrowserVersionString")
)

// ErrLoaderMissing is returned when WebView2Loader.dll cannot be found.
//
// Its own error because it is the one failure an operator can act on, and the
// message has to say so: the alternative is a nil window and no explanation,
// which is the failure mode this project keeps writing postmortems about.
type ErrLoaderMissing struct{ Err error }

func (e *ErrLoaderMissing) Error() string {
	return fmt.Sprintf("WebView2Loader.dll could not be loaded (%v) — it ships beside "+
		"the LANcast executable and the install may be incomplete", e.Err)
}

func (e *ErrLoaderMissing) Unwrap() error { return e.Err }

// load resolves the DLL and one of its entry points, or explains why it could
// not.
func load(p *windows.LazyProc) error {
	if err := nativeModule.Load(); err != nil {
		return &ErrLoaderMissing{Err: err}
	}
	if err := p.Find(); err != nil {
		return &ErrLoaderMissing{Err: err}
	}
	return nil
}

// CompareBrowserVersions compares two runtime versions and returns -1, 0 or 1
// for v1 < v2, v1 == v2, v1 > v2.
func CompareBrowserVersions(v1 string, v2 string) (int, error) {
	_v1, err := windows.UTF16PtrFromString(v1)
	if err != nil {
		return 0, err
	}
	_v2, err := windows.UTF16PtrFromString(v2)
	if err != nil {
		return 0, err
	}
	if err := load(nativeCompareBrowserVersions); err != nil {
		return 0, err
	}

	var result int
	_, _, err = nativeCompareBrowserVersions.Call(
		uintptr(unsafe.Pointer(_v1)),
		uintptr(unsafe.Pointer(_v2)),
		uintptr(unsafe.Pointer(&result)))
	if err != windows.ERROR_SUCCESS {
		return result, err
	}
	return result, nil
}

// GetInstalledVersion returns the installed WebView2 runtime version, or an
// empty string when none is installed.
//
// An empty string with a nil error is the "runtime absent" answer, not a
// failure: the machine is fine, it just has nothing to render with, and the
// caller is expected to say so rather than crash.
func GetInstalledVersion() (string, error) {
	if err := load(nativeGetAvailableCoreWebView2BrowserVersionString); err != nil {
		return "", err
	}

	var result *uint16
	hr, _, _ := nativeGetAvailableCoreWebView2BrowserVersionString.Call(
		uintptr(unsafe.Pointer(nil)),
		uintptr(unsafe.Pointer(&result)))
	defer windows.CoTaskMemFree(unsafe.Pointer(result)) // safe even if result is nil

	if hr != uintptr(windows.S_OK) {
		// The low 16 bits carry the error code. FILE_NOT_FOUND here means the
		// runtime is not installed, which is a state and not an error.
		if hr&0xFFFF == uintptr(windows.ERROR_FILE_NOT_FOUND) {
			return "", nil
		}
		return "", fmt.Errorf("GetAvailableCoreWebView2BrowserVersionString returned HRESULT 0x%X", hr)
	}
	return windows.UTF16PtrToString(result), nil
}

// CreateCoreWebView2EnvironmentWithOptions creates the WebView2 environment.
func CreateCoreWebView2EnvironmentWithOptions(browserExecutableFolder, userDataFolder *uint16, environmentOptions uintptr, environmentCompletedHandle uintptr) (uintptr, error) {
	if err := load(nativeCreate); err != nil {
		return 0, err
	}
	res, _, _ := nativeCreate.Call(
		uintptr(unsafe.Pointer(browserExecutableFolder)),
		uintptr(unsafe.Pointer(userDataFolder)),
		environmentOptions,
		environmentCompletedHandle,
	)
	return res, nil
}
