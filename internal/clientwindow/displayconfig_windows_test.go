package clientwindow

import (
	"testing"
	"unsafe"
)

// The structures cross into user32 by pointer, so their sizes are the contract:
// a field out of place is a corrupted read with no error. The sizes are the
// ones wingdi.h defines.
func TestDisplayConfigStructsMatchWingdi(t *testing.T) {
	for name, c := range map[string]struct{ got, want uintptr }{
		"DISPLAYCONFIG_PATH_INFO":          {unsafe.Sizeof(displayConfigPathInfo{}), 72},
		"DISPLAYCONFIG_MODE_INFO":          {unsafe.Sizeof(displayConfigModeInfo{}), 64},
		"DISPLAYCONFIG_DEVICE_INFO_HEADER": {unsafe.Sizeof(displayConfigHeader{}), 20},
		"DISPLAYCONFIG_SOURCE_DEVICE_NAME": {unsafe.Sizeof(displayConfigSourceName{}), 84},
		"DISPLAYCONFIG_TARGET_DEVICE_NAME": {unsafe.Sizeof(displayConfigTargetName{}), 420},
	} {
		if c.got != c.want {
			t.Errorf("%s is %d bytes, want %d", name, c.got, c.want)
		}
	}
}

// On a real desktop every active monitor should come back with a path. Skipped
// where there is no display (a service, CI).
func TestMonitorsCarryNamesAndPaths(t *testing.T) {
	mons := Monitors()
	if len(mons) == 0 {
		t.Skip("no monitors attached")
	}
	for _, m := range mons {
		if m.Path == "" {
			t.Errorf("%s has no device path", m.Device)
		}
	}
}
