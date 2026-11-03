package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"lancast/internal/retro/libretro"
)

/*
 * The User-Agent RetroAchievements asks an emulator for (ADR 0112 §3):
 *
 *	LANcast/0.10.2 (Windows 10.0) mupen64plus-next/2.6-Vulkan rcheevos/12.5
 *
 * The emulator and its version, the system, the core that is playing and
 * its version, then rcheevos' clause about itself. RetroAchievements uses it
 * to tell which emulator and core an unlock came from, and requires it to be
 * unique and stable before it approves an emulator for hardcore.
 *
 * The player builds it because only the player knows the core. The server
 * checks it against that shape (ra.UserAgentOK) and sends its own, shorter
 * one for anything that does not match.
 */
func raUserAgent(version string, core libretro.SystemInfo, clause string) string {
	name := uaToken(strings.ToLower(core.LibraryName))
	ver := uaToken(core.LibraryVersion)
	if name == "" {
		name = "core"
	}
	if ver == "" {
		ver = "0"
	}
	agent := fmt.Sprintf("LANcast/%s (Windows %s) %s/%s", version, windowsVersion(), name, ver)
	if clause != "" {
		agent += " " + clause
	}
	return agent
}

// uaToken keeps what a User-Agent product token may hold: letters, digits
// and ._+-. A space becomes '-', so "2.6-Vulkan 5d2ac21" stays one token.
func uaToken(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._+-", r):
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

// osVersionInfo mirrors OSVERSIONINFOW.
type osVersionInfo struct {
	size, major, minor, build, platform uint32
	csd                                 [128]uint16
}

var procRtlGetVersion = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")

// windowsVersion is "10.0" on Windows 10 and 11, read with RtlGetVersion:
// GetVersionEx reports 6.2 to a program with no manifest saying otherwise.
func windowsVersion() string {
	v := osVersionInfo{size: uint32(unsafe.Sizeof(osVersionInfo{}))}
	if r, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v))); r != 0 || v.major == 0 {
		return "10.0"
	}
	return fmt.Sprintf("%d.%d", v.major, v.minor)
}
