//go:build windows

package service

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunningServer asks the service control manager whether the installed LANcast
// service is the thing already holding the machine, and what it was started
// with. ok is false when the question could not be answered — no service
// installed, not running, or no rights to look — and the caller then falls back
// to the generic message rather than asserting something untrue.
//
// **This deliberately does not use mgr.Connect and withService**, which the rest
// of this package uses for install/start/stop. mgr.Connect asks for
// SC_MANAGER_ALL_ACCESS, so it fails for anyone who is not an administrator —
// fine for a command that is going to change the service, useless for an error
// message. Someone hitting the single-instance guard is very often *not*
// elevated (that is half the reason they are confused), and a diagnostic that
// only works with the rights you are missing is not a diagnostic.
//
// So: SC_MANAGER_CONNECT to reach the SCM, and QUERY_STATUS|QUERY_CONFIG on the
// service itself. Both are granted to authenticated users by default, which is
// what makes this readable from an ordinary desktop launch.
func RunningServer() (Running, bool) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return Running{}, false
	}
	defer windows.CloseServiceHandle(scm)

	name, err := windows.UTF16PtrFromString(Name)
	if err != nil {
		return Running{}, false
	}
	h, err := windows.OpenService(scm,
		name, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		// Not installed, or not ours to look at. Either way the holder is
		// something else and saying so would be a guess.
		return Running{}, false
	}
	defer windows.CloseServiceHandle(h)

	pid, running := servicePID(h)
	if !running {
		// Installed but stopped. That is worth knowing — it means the holder is
		// a stray process rather than the service, which is a different fix —
		// so report the absence rather than the service.
		return Running{}, false
	}

	r := Running{Service: true, PID: pid}
	if args, ok := serviceArgs(h); ok {
		r.DataDir, r.Addr = dataDirFromArgs(args)
	}
	return r, true
}

// servicePID reads the service's process id via QueryServiceStatusEx, which is
// the only call that reports it. Returns running=false for any state that is
// not actually running, since a stopped service holds nothing.
func servicePID(h windows.Handle) (uint32, bool) {
	var needed uint32
	// The first call fails with ERROR_INSUFFICIENT_BUFFER and reports the size.
	_ = windows.QueryServiceStatusEx(h, windows.SC_STATUS_PROCESS_INFO, nil, 0, &needed)
	if needed == 0 {
		return 0, false
	}
	buf := make([]byte, needed)
	if err := windows.QueryServiceStatusEx(h, windows.SC_STATUS_PROCESS_INFO,
		&buf[0], needed, &needed); err != nil {
		return 0, false
	}
	st := (*windows.SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0]))
	if st.CurrentState != windows.SERVICE_RUNNING {
		return 0, false
	}
	return st.ProcessId, true
}

// serviceArgs reads the command line the service was configured with, so the
// message can name the data directory the holder actually has open.
func serviceArgs(h windows.Handle) ([]string, bool) {
	var needed uint32
	_ = windows.QueryServiceConfig(h, nil, 0, &needed)
	if needed == 0 {
		return nil, false
	}
	buf := make([]byte, needed)
	cfg := (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buf[0]))
	if err := windows.QueryServiceConfig(h, cfg, needed, &needed); err != nil {
		return nil, false
	}
	if cfg.BinaryPathName == nil {
		return nil, false
	}
	return splitCommandLine(windows.UTF16PtrToString(cfg.BinaryPathName)), true
}
