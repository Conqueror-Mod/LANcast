//go:build !linux && !windows

package service

// NewManager reports that service management is unsupported here. macOS (launchd)
// is deferred until there is a way to test it (ADR 0016).
func NewManager() (Manager, error) { return nil, ErrUnsupported }
