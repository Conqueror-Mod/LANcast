//go:build windows

package service

import (
	"fmt"
	"time"

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

func (windowsManager) Status() (string, error) {
	var out string
	err := withService(func(s *mgr.Service) error {
		st, err := s.Query()
		if err != nil {
			return err
		}
		out = svcStateName(st.State)
		return nil
	})
	if err != nil {
		return "unknown", err
	}
	return out, nil
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
