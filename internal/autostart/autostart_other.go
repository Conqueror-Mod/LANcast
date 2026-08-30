//go:build !windows

package autostart

import "errors"

// ErrUnsupported is returned where there is no implementation. Callers treat it
// as "this option does not exist here" rather than as a failure — the same shape
// clientwindow uses for a platform without a web view.
var ErrUnsupported = errors.New("autostart: not supported on this platform")

func enabled(Target) (bool, error) { return false, nil }

func enable(Target, ...string) error { return ErrUnsupported }

func disable(Target) error { return nil }
