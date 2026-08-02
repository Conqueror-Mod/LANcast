//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const unitPath = "/etc/systemd/system/lancastd.service"

type systemdManager struct{}

// NewManager returns the systemd-backed manager.
func NewManager() (Manager, error) { return systemdManager{}, nil }

func (systemdManager) Install(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath, []byte(SystemdUnit(c)), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return systemctl("enable", "--now", Name)
}

func (systemdManager) Uninstall() error {
	// Best-effort stop/disable; a not-installed service is not an error to remove.
	_ = systemctl("disable", "--now", Name)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit: %w", err)
	}
	return systemctl("daemon-reload")
}

func (systemdManager) Start() error { return systemctl("start", Name) }
func (systemdManager) Stop() error  { return systemctl("stop", Name) }

func (systemdManager) Status() (string, error) {
	// is-active exits nonzero when inactive/failed, which is a normal answer, not
	// a failure — so the trimmed output is the status regardless of exit code.
	out, _ := exec.Command("systemctl", "is-active", Name).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "unknown", nil
	}
	return s, nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
