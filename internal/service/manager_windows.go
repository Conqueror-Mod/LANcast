//go:build windows

package service

import (
	"fmt"

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
	s.Close()
	return nil
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
