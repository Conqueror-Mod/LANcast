//go:build windows

package service

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// ConfiguredAddr is the -addr the installed service is configured with,
// running or not. Asked with the least privilege that can answer, like
// RunningServer, because the desktop window asks it as an ordinary user.
func ConfiguredAddr() (string, bool) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "", false
	}
	defer windows.CloseServiceHandle(scm)
	name, err := windows.UTF16PtrFromString(Name)
	if err != nil {
		return "", false
	}
	h, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return "", false
	}
	defer windows.CloseServiceHandle(h)
	args, ok := serviceArgs(h)
	if !ok {
		return "", false
	}
	_, addr := dataDirFromArgs(args)
	return addr, addr != ""
}

/*
 * SetAddr rewrites the installed service's -addr, and nothing else about it.
 *
 * ChangeServiceConfig with SERVICE_NO_CHANGE everywhere but the command line,
 * rather than mgr's UpdateConfig, which rewrites every field it has, the
 * account and its password included. Needs SERVICE_CHANGE_CONFIG: an
 * administrator, or the service's own LocalSystem account, which is who asks
 * when the port is changed in Settings.
 */
func SetAddr(addr string) error {
	return withService(func(s *mgr.Service) error {
		cur, err := s.Config()
		if err != nil {
			return fmt.Errorf("read the service's configuration: %w", err)
		}
		bin, err := windows.UTF16PtrFromString(withAddr(cur.BinaryPathName, addr))
		if err != nil {
			return err
		}
		const keep = windows.SERVICE_NO_CHANGE
		if err := windows.ChangeServiceConfig(s.Handle, keep, keep, keep, bin,
			nil, nil, nil, nil, nil, nil); err != nil {
			return fmt.Errorf("rewrite the service's command line: %w", err)
		}
		return nil
	})
}

/*
 * StartFresh stops the service if it is running, waits for the stop, and
 * starts it.
 *
 * Restart's difference is the first step: Restart fails when there is nothing
 * to stop. A server that could not listen on a new port has exited, and Windows
 * may be partway through its recovery restarts; this is the rollback, and it
 * has to work from whatever state that left.
 */
func StartFresh(timeout time.Duration) error {
	_ = withService(func(s *mgr.Service) error {
		_, err := s.Control(svc.Stop)
		return err
	})
	deadline := time.Now().Add(timeout)
	for {
		var state svc.State
		if err := withService(func(s *mgr.Service) error {
			st, err := s.Query()
			state = st.State
			return err
		}); err != nil {
			return fmt.Errorf("waiting for the service to stop: %w", err)
		}
		if state == svc.Stopped {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service did not stop within %s (state %s)", timeout, svcStateName(state))
		}
		time.Sleep(250 * time.Millisecond)
	}
	return (windowsManager{}).Start()
}
