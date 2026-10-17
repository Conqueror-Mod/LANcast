//go:build !windows

package service

import "time"

// ConfiguredAddr reports nothing off Windows: a systemd unit's port is the
// unit file's, and LANcast does not rewrite it (ADR 0089).
func ConfiguredAddr() (string, bool) { return "", false }

// SetAddr is Windows-only (ADR 0089).
func SetAddr(string) error { return ErrUnsupported }

// StartFresh is Windows-only (ADR 0089).
func StartFresh(time.Duration) error { return ErrUnsupported }
