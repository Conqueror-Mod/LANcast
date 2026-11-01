//go:build windows

package displaymode

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

/*
 * The Win32 half: list a monitor's modes, change its frequency, put it back.
 *
 * Deliberately thin. Every decision is Choose's; this file only fetches facts
 * and makes the one call Choose asked for.
 *
 * Which monitor is always "the one the video window is on", found from the
 * window handle at the moment of the switch. Never the primary display: the
 * games work learned that a launcher which touches the primary display moves
 * the person's whole desk around (memory: games display targeting).
 */

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procEnumDisplaySettingsExW   = user32.NewProc("EnumDisplaySettingsExW")
	procChangeDisplaySettingsExW = user32.NewProc("ChangeDisplaySettingsExW")
)

const (
	monitorDefaultNearest = 0x2
	enumCurrentSettings   = 0xFFFFFFFF

	dmBitsPerPel       = 0x00040000
	dmPelsWidth        = 0x00080000
	dmPelsHeight       = 0x00100000
	dmDisplayFrequency = 0x00400000
	dmInterlaced       = 0x2

	cdsTest       = 0x2
	cdsFullscreen = 0x4

	dispChangeSuccessful = 0
)

type rect struct{ Left, Top, Right, Bottom int32 }

// monitorInfoEx is MONITORINFOEXW: the device name is the point of it.
type monitorInfoEx struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
	SzDevice  [32]uint16
}

// devMode is DEVMODEW in its display form: 220 bytes, which a test checks,
// because a struct one field off would be read by Windows as somebody else's
// numbers without any error at all.
type devMode struct {
	DeviceName       [32]uint16
	SpecVersion      uint16
	DriverVersion    uint16
	Size             uint16
	DriverExtra      uint16
	Fields           uint32
	PositionX        int32
	PositionY        int32
	Orientation      uint32
	FixedOutput      uint32
	Color            int16
	Duplex           int16
	YResolution      int16
	TTOption         int16
	Collate          int16
	FormName         [32]uint16
	LogPixels        uint16
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	ICMMethod        uint32
	ICMIntent        uint32
	MediaType        uint32
	DitherType       uint32
	Reserved1        uint32
	Reserved2        uint32
	PanningWidth     uint32
	PanningHeight    uint32
}

func newDevMode() devMode {
	var d devMode
	d.Size = uint16(unsafe.Sizeof(d))
	return d
}

func (d devMode) mode() Mode {
	return Mode{Width: int(d.PelsWidth), Height: int(d.PelsHeight), Bits: int(d.BitsPerPel), Hz: int(d.DisplayFrequency)}
}

// MonitorOf is the GDI device name of the monitor hwnd is (mostly) on.
func MonitorOf(hwnd uintptr) (string, error) {
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultNearest)
	if mon == 0 {
		return "", errors.New("no monitor for the window")
	}
	var mi monitorInfoEx
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return "", errors.New("GetMonitorInfo failed")
	}
	return syscall.UTF16ToString(mi.SzDevice[:]), nil
}

func current(device *uint16) (Mode, error) {
	d := newDevMode()
	if ok, _, _ := procEnumDisplaySettingsExW.Call(uintptr(unsafe.Pointer(device)),
		enumCurrentSettings, uintptr(unsafe.Pointer(&d)), 0); ok == 0 {
		return Mode{}, errors.New("cannot read the current display mode")
	}
	return d.mode(), nil
}

// offered lists the monitor's progressive modes. Interlaced ones are dropped
// here, so Choose never sees them: 1080i at 60 is not a home for a film.
func offered(device *uint16) []Mode {
	var out []Mode
	for i := uint32(0); ; i++ {
		d := newDevMode()
		if ok, _, _ := procEnumDisplaySettingsExW.Call(uintptr(unsafe.Pointer(device)),
			uintptr(i), uintptr(unsafe.Pointer(&d)), 0); ok == 0 {
			return out
		}
		if d.DisplayFlags&dmInterlaced != 0 {
			continue
		}
		out = append(out, d.mode())
	}
}

func change(device *uint16, m Mode, flags uintptr) uintptr {
	d := newDevMode()
	d.Fields = dmPelsWidth | dmPelsHeight | dmBitsPerPel | dmDisplayFrequency
	d.PelsWidth, d.PelsHeight = uint32(m.Width), uint32(m.Height)
	d.BitsPerPel, d.DisplayFrequency = uint32(m.Bits), uint32(m.Hz)
	r, _, _ := procChangeDisplaySettingsExW.Call(uintptr(unsafe.Pointer(device)),
		uintptr(unsafe.Pointer(&d)), 0, flags, 0)
	return r
}

// toRegistry puts a monitor back to the mode saved in the registry: the one
// the person chose in Windows' settings, since nothing here ever saves one.
func toRegistry(device string) error {
	p, err := syscall.UTF16PtrFromString(device)
	if err != nil {
		return err
	}
	if r, _, _ := procChangeDisplaySettingsExW.Call(uintptr(unsafe.Pointer(p)), 0, 0, 0, 0); r != dispChangeSuccessful {
		return fmt.Errorf("restoring %s: ChangeDisplaySettingsEx returned %d", device, int32(r))
	}
	return nil
}

// Result is what one Match did, for the log.
type Result struct {
	Device   string
	From     Mode
	Decision Decision
}

// Switcher matches one window's monitor to its films and undoes it.
type Switcher struct {
	dir    string
	mu     sync.Mutex
	active *Record
}

// NewSwitcher keeps its restore record in dir, the client's data directory.
func NewSwitcher(dir string) *Switcher { return &Switcher{dir: dir} }

// Active reports whether a monitor is switched and not yet restored.
func (s *Switcher) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active != nil
}

/*
 * Match switches the monitor hwnd is on to suit a film at fps, if Choose
 * finds a mode for it.
 *
 * A second film on an already-switched monitor is matched from where it is,
 * and the record keeps the *original* rate, so one restore undoes any number
 * of films. A window that has moved to another monitor since the last switch
 * puts the old one back first.
 *
 * The change is tested (CDS_TEST) before it is made: a driver that lists a
 * mode it will not set is common enough on HDMI that asking first is cheaper
 * than a black screen.
 */
func (s *Switcher) Match(hwnd uintptr, fps float64) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, err := MonitorOf(hwnd)
	if err != nil {
		return Result{}, err
	}
	if s.active != nil && s.active.Device != device {
		if err := s.restoreLocked(); err != nil {
			return Result{}, err
		}
	}
	p, err := syscall.UTF16PtrFromString(device)
	if err != nil {
		return Result{}, err
	}
	cur, err := current(p)
	if err != nil {
		return Result{}, err
	}
	res := Result{Device: device, From: cur, Decision: Choose(fps, cur, offered(p))}
	if !res.Decision.Switch {
		return res, nil
	}
	rec := Record{Device: device, FromHz: cur.Hz, ToHz: res.Decision.To.Hz}
	if s.active != nil {
		rec.FromHz = s.active.FromHz
	}
	if r := change(p, res.Decision.To, cdsTest); r != dispChangeSuccessful {
		res.Decision = Decision{Reason: fmt.Sprintf("driver refused %s (%d)", res.Decision.To, int32(r))}
		return res, nil
	}
	if err := WriteRecord(s.dir, rec); err != nil {
		// No record, no switch: a switch that a crash could not undo is
		// worse than judder.
		return res, fmt.Errorf("writing the restore record: %w", err)
	}
	if r := change(p, res.Decision.To, cdsFullscreen); r != dispChangeSuccessful {
		_ = ClearRecord(s.dir)
		res.Decision = Decision{Reason: fmt.Sprintf("switching to %s failed (%d)", res.Decision.To, int32(r))}
		return res, nil
	}
	s.active = &rec
	return res, nil
}

// Restore puts a switched monitor back. Nothing to do is not an error.
func (s *Switcher) Restore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restoreLocked()
}

func (s *Switcher) restoreLocked() error {
	if s.active == nil {
		return nil
	}
	err := toRegistry(s.active.Device)
	// The record goes either way. A restore that failed now will fail the
	// same way at the next launch, and a monitor left at 24 Hz is fixed by
	// Windows' own display settings in a way a looping retry is not.
	_ = ClearRecord(s.dir)
	s.active = nil
	return err
}

// Recover undoes a switch the last run left behind, if its record is on disk.
func Recover(dir string) (Record, bool, error) {
	r, ok := ReadRecord(dir)
	if !ok {
		return Record{}, false, nil
	}
	err := toRegistry(r.Device)
	_ = ClearRecord(dir)
	return r, true, err
}
