// Package service manages LANcast as an OS service — start on boot, stay running,
// no terminal (ADR 0016). The pure core here (config, data-dir resolution,
// systemd-unit generation) is testable on any platform; the actual register/
// start/stop lives in per-OS Manager implementations.
package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Name is the OS-level service identifier.
	Name = "lancastd"
	// DisplayName and Description are what the OS shows for the service.
	DisplayName = "LANcast media server"
	Description = "LANcast self-hosted media server"
)

// Config is what an install records so the running service and an interactive
// lancastd pointed at the same data see the same library.
type Config struct {
	// DataDir is the machine-wide, explicit data directory — never the per-user
	// default. This is the ADR 0016 trap: a service runs as an account whose
	// per-user config dir is not the installer's, so an unset data dir silently
	// builds a second, empty library.
	DataDir string
	// Addr is the listen address the service passes to the server.
	Addr string
	// ExePath is the lancastd binary the service runs.
	ExePath string
	// FFmpegDir, when set, is prepended to the service's PATH so a
	// user-installed ffmpeg is found even though a service PATH usually omits it.
	FFmpegDir string
}

// DefaultDataDir is the machine-wide data directory for the platform — the pin
// that keeps a service off the per-user default. Passed goos so it is testable
// from any host.
func DefaultDataDir(goos string) string {
	switch goos {
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "LANcast")
	default:
		return "/var/lib/lancast"
	}
}

// Validate refuses a config a service must not run with — chiefly an unset data
// dir, which is the trap above.
func (c Config) Validate() error {
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("a service needs an explicit --data directory (never the per-user default)")
	}
	if strings.TrimSpace(c.ExePath) == "" {
		return errors.New("the service could not determine its own executable path")
	}
	if strings.TrimSpace(c.Addr) == "" {
		return errors.New("a service needs a listen address")
	}
	return nil
}

// ServiceArgs are the command-line arguments the service invokes lancastd with:
// the internal run mode plus the pinned data dir and address.
func (c Config) ServiceArgs() []string {
	return []string{"service", "run", "-data", c.DataDir, "-addr", c.Addr}
}

// SystemdUnit renders the systemd unit for the config. Kept pure so its content
// is asserted in tests without touching /etc or systemctl. If an ffmpeg dir is
// recorded it is prepended to PATH, so the existing exec.LookPath finds ffmpeg
// under a service account with no change to the server.
func SystemdUnit(c Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\nAfter=network-online.target\nWants=network-online.target\n\n", Description)
	b.WriteString("[Service]\nType=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s -data %s -addr %s\n", c.ExePath, c.DataDir, c.Addr)
	if c.FFmpegDir != "" {
		fmt.Fprintf(&b, "Environment=PATH=%s:/usr/local/bin:/usr/bin:/bin\n", c.FFmpegDir)
	}
	b.WriteString("Restart=on-failure\nRestartSec=5\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\n")
	return b.String()
}

// Manager registers and controls the service on one platform.
type Manager interface {
	Install(Config) error
	Uninstall() error
	Start() error
	Stop() error
	Status() (string, error)
}

// ErrUnsupported is returned by NewManager on a platform without service support
// (today anything but Windows and Linux — macOS is deferred, ADR 0016).
var ErrUnsupported = errors.New("service management is not supported on this platform")
