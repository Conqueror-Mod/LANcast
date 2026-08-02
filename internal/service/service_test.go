package service

import (
	"strings"
	"testing"
)

func TestDefaultDataDirIsMachineWide(t *testing.T) {
	if got := DefaultDataDir("linux"); got != "/var/lib/lancast" {
		t.Errorf("linux data dir = %q, want /var/lib/lancast", got)
	}
	// Windows resolves under ProgramData — never a per-user path (the ADR 0016 trap).
	win := DefaultDataDir("windows")
	if !strings.Contains(win, "LANcast") {
		t.Errorf("windows data dir = %q, want a LANcast dir under ProgramData", win)
	}
	if strings.Contains(strings.ToLower(win), "users") {
		t.Errorf("windows data dir = %q looks per-user; must be machine-wide", win)
	}
}

func TestValidateRefusesUnsetDataDir(t *testing.T) {
	base := Config{DataDir: "/var/lib/lancast", Addr: ":8080", ExePath: "/usr/bin/lancastd"}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	bad := base
	bad.DataDir = "  "
	if err := bad.Validate(); err == nil {
		t.Error("an unset data dir must be refused — it is the trap the service exists to avoid")
	}

	noExe := base
	noExe.ExePath = ""
	if err := noExe.Validate(); err == nil {
		t.Error("a missing executable path must be refused")
	}
}

func TestServiceArgsPinDataDir(t *testing.T) {
	c := Config{DataDir: `C:\ProgramData\LANcast`, Addr: ":9000"}
	got := strings.Join(c.ServiceArgs(), " ")
	want := `service run -data C:\ProgramData\LANcast -addr :9000`
	if got != want {
		t.Errorf("ServiceArgs = %q, want %q", got, want)
	}
}

func TestSystemdUnitContents(t *testing.T) {
	c := Config{DataDir: "/var/lib/lancast", Addr: ":8080", ExePath: "/usr/bin/lancastd"}
	unit := SystemdUnit(c)

	for _, want := range []string{
		"Description=" + Description,
		"ExecStart=/usr/bin/lancastd -data /var/lib/lancast -addr :8080",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q\n---\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "Environment=PATH=") {
		t.Error("no ffmpeg dir was set, so no PATH override should appear")
	}

	// With an ffmpeg dir, it is prepended to PATH so a service account finds it.
	c.FFmpegDir = "/opt/ffmpeg/bin"
	if u := SystemdUnit(c); !strings.Contains(u, "Environment=PATH=/opt/ffmpeg/bin:") {
		t.Errorf("ffmpeg dir not prepended to PATH\n---\n%s", u)
	}
}
