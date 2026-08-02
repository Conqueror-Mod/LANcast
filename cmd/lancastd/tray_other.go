//go:build !windows

package main

// trayRun on non-Windows has no desktop tray (headless servers use the systemd
// service, ADR 0022), so it just runs the server foreground.
func trayRun(addr, dataDir string) error {
	return serviceRun(dataDir, addr)
}
