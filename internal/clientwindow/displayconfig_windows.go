package clientwindow

import (
	"syscall"
	"unsafe"
)

/*
 * What a monitor is called, and which monitor it is.
 *
 * GetMonitorInfo names a screen `\\.\DISPLAY6`, and neither half of that is
 * something to show a person or to remember. The number is the graphics
 * driver's, not the one Windows' display settings show, and it climbs every
 * time the driver re-enumerates: on the machine this was written on, three
 * monitors were DISPLAY6, 7 and 8, and a choice saved weeks earlier named a
 * DISPLAY3 that no longer existed. The picker read "Display 6", and the old
 * choice silently stopped applying.
 *
 * The display configuration API knows better. For each active source it gives
 * the same GDI name, so the two can be joined, and for its target, the monitor
 * itself, two things:
 *
 *   - the friendly name from the monitor's EDID, "LG ULTRAGEAR", the name
 *     Windows' advanced display settings show;
 *   - the monitor's device path, which names the physical monitor on its port
 *     and survives the renumbering.
 */

type luid struct {
	Low  uint32
	High int32
}

type displayConfigRational struct{ Num, Den uint32 }

// displayConfigPathInfo is DISPLAYCONFIG_PATH_INFO: 72 bytes.
type displayConfigPathInfo struct {
	SourceAdapter     luid
	SourceID          uint32
	SourceModeIdx     uint32
	SourceStatusFlags uint32

	TargetAdapter     luid
	TargetID          uint32
	TargetModeIdx     uint32
	OutputTechnology  uint32
	Rotation          uint32
	Scaling           uint32
	RefreshRate       displayConfigRational
	ScanLineOrdering  uint32
	TargetAvailable   int32
	TargetStatusFlags uint32

	Flags uint32
}

// displayConfigModeInfo is DISPLAYCONFIG_MODE_INFO: 64 bytes. Its union is
// never read here; it only has to be the right size for the array.
type displayConfigModeInfo struct {
	InfoType  uint32
	ID        uint32
	AdapterID luid
	union     [48]byte
}

type displayConfigHeader struct {
	Type      uint32
	Size      uint32
	AdapterID luid
	ID        uint32
}

// displayConfigSourceName is DISPLAYCONFIG_SOURCE_DEVICE_NAME: 84 bytes.
type displayConfigSourceName struct {
	Header  displayConfigHeader
	GDIName [32]uint16
}

// displayConfigTargetName is DISPLAYCONFIG_TARGET_DEVICE_NAME: 420 bytes.
type displayConfigTargetName struct {
	Header            displayConfigHeader
	Flags             uint32
	OutputTechnology  uint32
	EDIDManufactureID uint16
	EDIDProductCodeID uint16
	ConnectorInstance uint32
	FriendlyName      [64]uint16
	DevicePath        [128]uint16
}

const (
	qdcOnlyActivePaths         = 0x2
	displayConfigGetSourceName = 1
	displayConfigGetTargetName = 2
	errorInsufficientBuffer    = 122
	displayConfigQueryAttempts = 3
)

var (
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
)

// monitorName is what the display configuration says about one screen.
type monitorName struct {
	Friendly string
	Path     string
}

/*
 * monitorNames maps each active screen's GDI name to its friendly name and
 * device path. Empty on any failure: callers fall back to the GDI name, which
 * is what they used before this existed.
 *
 * Queried more than once if needed, because the number of paths can change
 * between asking how big the buffers must be and filling them (a monitor
 * waking up), and the API says so with ERROR_INSUFFICIENT_BUFFER.
 */
func monitorNames() map[string]monitorName {
	out := map[string]monitorName{}
	var paths []displayConfigPathInfo
	for attempt := 0; attempt < displayConfigQueryAttempts; attempt++ {
		var nPath, nMode uint32
		if r, _, _ := procGetDisplayConfigBufferSizes.Call(qdcOnlyActivePaths,
			uintptr(unsafe.Pointer(&nPath)), uintptr(unsafe.Pointer(&nMode))); r != 0 || nPath == 0 {
			return out
		}
		paths = make([]displayConfigPathInfo, nPath)
		if nMode == 0 {
			nMode = 1
		}
		modes := make([]displayConfigModeInfo, nMode)
		r, _, _ := procQueryDisplayConfig.Call(qdcOnlyActivePaths,
			uintptr(unsafe.Pointer(&nPath)), uintptr(unsafe.Pointer(&paths[0])),
			uintptr(unsafe.Pointer(&nMode)), uintptr(unsafe.Pointer(&modes[0])), 0)
		if r == errorInsufficientBuffer {
			continue
		}
		if r != 0 {
			return out
		}
		paths = paths[:nPath]
		break
	}

	for _, p := range paths {
		src := displayConfigSourceName{Header: displayConfigHeader{
			Type: displayConfigGetSourceName, Size: uint32(unsafe.Sizeof(displayConfigSourceName{})),
			AdapterID: p.SourceAdapter, ID: p.SourceID,
		}}
		if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&src))); r != 0 {
			continue
		}
		tgt := displayConfigTargetName{Header: displayConfigHeader{
			Type: displayConfigGetTargetName, Size: uint32(unsafe.Sizeof(displayConfigTargetName{})),
			AdapterID: p.TargetAdapter, ID: p.TargetID,
		}}
		if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&tgt))); r != 0 {
			continue
		}
		gdi := syscall.UTF16ToString(src.GDIName[:])
		if gdi == "" {
			continue
		}
		// A screen cloned to two monitors has one source and two targets;
		// the first answer stands, since a window can only be on the source.
		if _, seen := out[gdi]; seen {
			continue
		}
		out[gdi] = monitorName{
			Friendly: syscall.UTF16ToString(tgt.FriendlyName[:]),
			Path:     syscall.UTF16ToString(tgt.DevicePath[:]),
		}
	}
	return out
}
