//go:build !windows

package raise

// Nothing to do off Windows. The desktop window is a Windows build (ADR 0023),
// so a second launch elsewhere has no window to raise — and a no-op that
// reports success is honest here rather than convenient: there was nobody to
// tell, which is exactly what happened.
func signal() error { return nil }

func listen(func()) (func(), error) { return func() {}, nil }
