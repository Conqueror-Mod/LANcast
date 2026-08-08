//go:build !windows

package clientwindow

import "errors"

// ErrUnsupported means there is no native window on this platform, so the
// caller should open a browser instead.
//
// Windows is where the friction ADR 0023 catalogues actually lives, and where
// a webview is reachable without CGO. macOS and Linux keep the browser path,
// which is the retreat that ADR already named as acceptable — not an oversight
// to be filled in later by whoever notices this file.
var ErrUnsupported = errors.New("client window: not supported on this platform")

func open(Options) error { return ErrUnsupported }

func check() error { return ErrUnsupported }
