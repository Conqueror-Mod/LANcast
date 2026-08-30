//go:build windows

package service

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type windowsManager struct{}

// NewManager returns the Windows-SCM-backed manager.
func NewManager() (Manager, error) { return windowsManager{}, nil }

func (windowsManager) Install(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator?): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Name); err == nil {
		s.Close()
		return fmt.Errorf("service %q is already installed", Name)
	}

	cfg := mgr.Config{
		DisplayName:      DisplayName,
		Description:      Description,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
	}
	// The service invokes lancastd in its internal run mode, pinned to the
	// explicit data dir — never the service account's per-user default.
	s, err := m.CreateService(Name, c.ExePath, cfg, c.ServiceArgs()...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Come back after an unexpected exit. Without recovery actions Windows does
	// nothing when the process dies — killed from Task Manager, or crashed —
	// and LANcast stays down until somebody notices it is missing. That
	// happened twice on one machine in a day.
	//
	// Three attempts with increasing delays, then stop. A server that cannot
	// start at all — the schema of a database written by a newer build is the
	// real case — must not restart forever; it retries a few times, gives up,
	// and leaves a clean record instead of a loop. The counter resets after a
	// day, so an isolated failure months apart is always retried.
	//
	// Best-effort: a service that runs but will not auto-restart is better than
	// refusing to install.
	if err := setRecoveryActions(s); err != nil {
		return fmt.Errorf("service installed, but automatic restart could not be configured: %w", err)
	}
	return nil
}

// setRecoveryActions asks Windows to restart the service after an unexpected
// exit.
func setRecoveryActions(s *mgr.Service) error {
	return s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, uint32((24 * time.Hour).Seconds()))
}

func withService(fn func(*mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator?): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed", Name)
	}
	defer s.Close()
	return fn(s)
}

func (windowsManager) Uninstall() error {
	return withService(func(s *mgr.Service) error {
		_, _ = s.Control(svc.Stop) // best-effort; ignore if already stopped
		if err := s.Delete(); err != nil {
			return fmt.Errorf("delete service: %w", err)
		}
		return nil
	})
}

func (windowsManager) Start() error {
	return withService(func(s *mgr.Service) error {
		if err := s.Start(); err != nil {
			return fmt.Errorf("start service: %w", err)
		}
		return nil
	})
}

func (windowsManager) Stop() error {
	return withService(func(s *mgr.Service) error {
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("stop service: %w", err)
		}
		return nil
	})
}

/*
 * Status asks with the least privilege that can answer, which is the whole
 * point of it not going through withService.
 *
 * `mgr.Connect` requests `SC_MANAGER_ALL_ACCESS`, and the service control
 * manager grants that to administrators only. Every other method here needs it
 * — installing, starting, deleting are administrator acts and a consent prompt
 * is the honest cost of them. Reading a state is not, and routing it through
 * the same door made it fail with **"Access is denied"** on any launch that was
 * not elevated.
 *
 * That is not a cosmetic difference. `installedService()` reads this and treats
 * an error as *not installed*, so on a normal double-click the launcher decided
 * there was no service, fell through to its "somebody else holds the name"
 * branch, opened a browser and exited. **The entire service-aware path was dead
 * for every unelevated launch**, which is every ordinary one — found only when
 * a tray icon that was supposed to appear did not, and the browser opened
 * instead exactly as it always had.
 *
 * `SC_MANAGER_CONNECT` plus `SERVICE_QUERY_STATUS` is what an authenticated
 * user holds by default, and it is enough to answer "is it there, and is it
 * running".
 */
func (windowsManager) Status() (string, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "unknown", fmt.Errorf("connect to service manager: %w", err)
	}
	defer windows.CloseServiceHandle(scm)

	name, err := windows.UTF16PtrFromString(Name)
	if err != nil {
		return "unknown", err
	}
	h, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return "unknown", fmt.Errorf("service %q is not installed: %w", Name, err)
	}
	defer windows.CloseServiceHandle(h)

	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return "unknown", fmt.Errorf("query service %q: %w", Name, err)
	}
	return svcStateName(svc.State(st.CurrentState)), nil
}

func svcStateName(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start pending"
	case svc.StopPending:
		return "stop pending"
	case svc.Running:
		return "running"
	case svc.PausePending:
		return "pause pending"
	case svc.Paused:
		return "paused"
	case svc.ContinuePending:
		return "continue pending"
	default:
		return "unknown"
	}
}
