//go:build !windows

package raise

// Nothing to do off Windows. The desktop window is a Windows build (ADR 0023),
// so a second launch elsewhere has no window to raise — and a no-op that
// reports success is honest here rather than convenient: there was nobody to
// tell, which is exactly what happened.
func signalShow() error { return nil }
func signalQuit() error { return nil }

func listen(_, _ func()) (func(), error) { return func() {}, nil }

// No tray on these platforms, so nothing can restore a hidden window and
// close-to-tray must mean close.
func trayPresent() bool { return false }

func holdTray() (func(), error) { return func() {}, nil }
