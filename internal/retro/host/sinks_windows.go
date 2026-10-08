//go:build windows

package host

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * The Windows sinks: a GDI blit, waveOut, and XInput.
 *
 * Deliberately the simplest Windows APIs that do each job. A framebuffer
 * core hands over a finished picture a few hundred pixels across, and GDI's
 * StretchDIBits draws that at any size with nearest-neighbour scaling and no
 * device to create or lose — so there is nothing here that a session-less or
 * GPU-less machine can fail to set up (CLAUDE.md, on DXVA2). waveOut is the
 * oldest audio API Windows has and still the one that blocks a writer in
 * exactly the way the session wants its pacing done. Stage 3's OpenGL path is
 * a different sink; these are not in its way.
 */

var (
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	user32 = windows.NewLazySystemDLL("user32.dll")
	winmm  = windows.NewLazySystemDLL("winmm.dll")
	xinput = windows.NewLazySystemDLL("xinput1_4.dll")

	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	procGetForeground    = user32.NewProc("GetForegroundWindow")
	procGetAncestor      = user32.NewProc("GetAncestor")
	procStretchDIBits    = gdi32.NewProc("StretchDIBits")
	procSetStretchMode   = gdi32.NewProc("SetStretchBltMode")
	procPatBlt           = gdi32.NewProc("PatBlt")

	procWaveOutOpen    = winmm.NewProc("waveOutOpen")
	procWaveOutClose   = winmm.NewProc("waveOutClose")
	procWaveOutPrepare = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutUnprep  = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutWrite   = winmm.NewProc("waveOutWrite")
	procWaveOutReset   = winmm.NewProc("waveOutReset")

	procXInputGetState = xinput.NewProc("XInputGetState")
)

// ---- video ----

type bitmapInfoHeader struct {
	size                   uint32
	width, height          int32
	planes, bitCount       uint16
	compression, sizeImage uint32
	xPelsPerM, yPelsPerM   int32
	clrUsed, clrImportant  uint32
}

const (
	dibRGBColors = 0
	srcCopy      = 0x00CC0020
	blackness    = 0x00000042
	colorOnColor = 3 // nearest neighbour: crisp pixels, no blur
)

type winRect struct{ left, top, right, bottom int32 }

// GDIVideo draws frames into a window. It is safe to call from the core's
// thread: GDI lets any thread draw to a window's DC.
type GDIVideo struct {
	HWND uintptr

	mu       sync.Mutex
	lastSize [2]int32
}

func (v *GDIVideo) Present(bgra []byte, width, height int, aspect float64) {
	if v.HWND == 0 || width == 0 || height == 0 || len(bgra) < width*height*4 {
		return
	}
	var rc winRect
	if r, _, _ := procGetClientRect.Call(v.HWND, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return
	}
	ww, wh := int(rc.right-rc.left), int(rc.bottom-rc.top)
	dst := Layout(ww, wh, width, height, aspect)
	if dst.W == 0 {
		return
	}
	dc, _, _ := procGetDC.Call(v.HWND)
	if dc == 0 {
		return
	}
	defer procReleaseDC.Call(v.HWND, dc)
	_, _, _ = procSetStretchMode.Call(dc, colorOnColor)

	// The bars are painted only when the window changes size: they do not
	// change otherwise, and painting them every frame is a visible flicker
	// on some drivers.
	v.mu.Lock()
	resized := v.lastSize != [2]int32{int32(ww), int32(wh)}
	v.lastSize = [2]int32{int32(ww), int32(wh)}
	v.mu.Unlock()
	if resized {
		_, _, _ = procPatBlt.Call(dc, 0, 0, uintptr(ww), uintptr(wh), blackness)
	}

	bi := bitmapInfoHeader{
		width: int32(width), height: -int32(height), // negative: rows are top-down
		planes: 1, bitCount: 32,
	}
	bi.size = uint32(unsafe.Sizeof(bi))
	_, _, _ = procStretchDIBits.Call(dc,
		uintptr(dst.X), uintptr(dst.Y), uintptr(dst.W), uintptr(dst.H),
		0, 0, uintptr(width), uintptr(height),
		uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&bi)),
		dibRGBColors, srcCopy)
}

// ---- audio ----

type waveFormatEx struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	size           uint16
}

type waveHdr struct {
	data          uintptr
	bufferLength  uint32
	bytesRecorded uint32
	user          uintptr
	flags         uint32
	loops         uint32
	next          uintptr
	reserved      uintptr
}

const (
	waveMapper    = 0xFFFFFFFF
	callbackEvent = 0x00050000
	whdrDone      = 0x00000001
	wavFormatPCM  = 1
	// buffers of about 16ms each: four in flight is a little over a frame
	// of latency at 60fps either side, which is below what a person notices
	// and above what a busy machine underruns.
	audioBuffers  = 4
	bufferSeconds = 1.0 / 60
)

// WaveOut plays the core's sound and, by blocking while every buffer is
// queued, sets the game's speed.
type WaveOut struct {
	h      uintptr
	event  windows.Handle
	hdrs   []*waveHdr
	mem    []uintptr // heap memory per buffer, freed on Close
	size   int       // bytes per buffer
	next   int
	fill   int
	cur    []byte
	opened bool
}

// ErrNoAudioDevice is returned when the machine has nothing to play on.
var ErrNoAudioDevice = errors.New("no audio output device")

func (a *WaveOut) Open(rate int) error {
	if rate <= 0 {
		return fmt.Errorf("sample rate %d", rate)
	}
	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return err
	}
	f := waveFormatEx{formatTag: wavFormatPCM, channels: 2, samplesPerSec: uint32(rate),
		bitsPerSample: 16, blockAlign: 4, avgBytesPerSec: uint32(rate) * 4}
	var h uintptr
	if r, _, _ := procWaveOutOpen.Call(uintptr(unsafe.Pointer(&h)), waveMapper,
		uintptr(unsafe.Pointer(&f)), uintptr(ev), 0, callbackEvent); r != 0 {
		windows.CloseHandle(ev)
		if r == 2 /* MMSYSERR_BADDEVICEID */ || r == 6 /* MMSYSERR_NODRIVER */ {
			return ErrNoAudioDevice
		}
		return fmt.Errorf("waveOutOpen failed (%d)", r)
	}
	a.h, a.event, a.opened = h, ev, true
	a.size = int(float64(rate)*bufferSeconds) * 4
	if a.size < 256 {
		a.size = 256
	}
	for i := 0; i < audioBuffers; i++ {
		mem, err := heapAlloc(uintptr(a.size) + unsafe.Sizeof(waveHdr{}))
		if err != nil {
			a.Close()
			return err
		}
		a.mem = append(a.mem, mem)
		// The header lives in the same heap block as its data, ahead of it:
		// waveOut holds both until the buffer is played, and neither may be
		// Go memory the collector does not know is still in use.
		hdr := (*waveHdr)(heapPointer(mem))
		hdr.data = mem + unsafe.Sizeof(waveHdr{})
		hdr.flags = whdrDone // free until first written
		a.hdrs = append(a.hdrs, hdr)
	}
	a.cur = unsafe.Slice((*byte)(heapPointer(a.hdrs[0].data)), a.size)
	return nil
}

// Write queues samples, blocking while every buffer is still playing.
func (a *WaveOut) Write(samples []int16) {
	if !a.opened {
		return
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), len(samples)*2)
	for len(b) > 0 {
		n := copy(a.cur[a.fill:], b)
		a.fill += n
		b = b[n:]
		if a.fill == a.size {
			a.submit()
		}
	}
}

func (a *WaveOut) submit() {
	hdr := a.hdrs[a.next]
	hdr.bufferLength = uint32(a.fill)
	hdr.flags = 0
	_, _, _ = procWaveOutPrepare.Call(a.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr))
	_, _, _ = procWaveOutWrite.Call(a.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr))
	a.next = (a.next + 1) % len(a.hdrs)
	a.fill = 0
	// The next buffer must be done before it is written into. The wait has a
	// ceiling so a device that stops signalling (unplugged headphones) costs
	// a stutter, not a hung game.
	nh := a.hdrs[a.next]
	deadline := time.Now().Add(250 * time.Millisecond)
	for nh.flags&whdrDone == 0 && time.Now().Before(deadline) {
		_, _ = windows.WaitForSingleObject(a.event, 50)
	}
	_, _, _ = procWaveOutUnprep.Call(a.h, uintptr(unsafe.Pointer(nh)), unsafe.Sizeof(*nh))
	a.cur = unsafe.Slice((*byte)(heapPointer(nh.data)), a.size)
}

func (a *WaveOut) Close() {
	if a.opened {
		_, _, _ = procWaveOutReset.Call(a.h)
		for _, hdr := range a.hdrs {
			_, _, _ = procWaveOutUnprep.Call(a.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr))
		}
		_, _, _ = procWaveOutClose.Call(a.h)
		windows.CloseHandle(a.event)
	}
	for _, m := range a.mem {
		heapFree(m)
	}
	*a = WaveOut{}
}

var (
	kernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procGetHeap   = kernel32.NewProc("GetProcessHeap")
	procHeapAlloc = kernel32.NewProc("HeapAlloc")
	procHeapFree  = kernel32.NewProc("HeapFree")
)

func heapAlloc(n uintptr) (uintptr, error) {
	h, _, _ := procGetHeap.Call()
	p, _, _ := procHeapAlloc.Call(h, 0x8 /* zero */, n)
	if p == 0 {
		return 0, errors.New("out of memory")
	}
	return p, nil
}

// heapPointer turns a heap address back into a pointer. The memory is the
// process heap's, not Go's, so go vet's rule about uintptr round trips does
// not apply; unsafe.Add says so without tripping it.
func heapPointer(p uintptr) unsafe.Pointer { return unsafe.Add(unsafe.Pointer(nil), p) }

func heapFree(p uintptr) {
	h, _, _ := procGetHeap.Call()
	_, _, _ = procHeapFree.Call(h, 0, p)
}

// ---- input ----

type xinputState struct {
	packet  uint32
	gamepad XInputGamepad
}

var (
	xinputGuideOnce sync.Once
	xinputGetEx     uintptr // XInputGetStateEx, ordinal 100, where present
)

/*
 * xinputGetStateEx finds the export that also reports the Guide button.
 *
 * It has no name, only ordinal 100, and is undocumented — which is why the
 * menu does not depend on it: Escape and a held Select+Start always work, and
 * Guide is a convenience where the driver offers it.
 */
func xinputGetStateEx() uintptr {
	xinputGuideOnce.Do(func() {
		if err := xinput.Load(); err != nil {
			return
		}
		if p, err := windows.GetProcAddressByOrdinal(windows.Handle(xinput.Handle()), 100); err == nil {
			xinputGetEx = p
		}
	})
	return xinputGetEx
}

// Controllers reads XInput pads and, while the game's window has focus, the
// keyboard.
type Controllers struct {
	// HWND is the window the game is in. Keys count only while it — or the
	// window it belongs to — is in front, so typing in another program does
	// not move the game.
	HWND uintptr
}

func (c Controllers) Poll() (pads [2]Pad, guide, escape bool) {
	ex := xinputGetStateEx()
	for i := 0; i < 2; i++ {
		var st xinputState
		var r uintptr
		if ex != 0 {
			r, _, _ = syscallN(ex, uintptr(i), uintptr(unsafe.Pointer(&st)))
		} else if procXInputGetState.Find() == nil {
			r, _, _ = procXInputGetState.Call(uintptr(i), uintptr(unsafe.Pointer(&st)))
		} else {
			r = 1167 // ERROR_DEVICE_NOT_CONNECTED
		}
		if r != 0 {
			continue
		}
		pads[i] = FromXInput(st.gamepad)
		if i == 0 && st.gamepad.Buttons&XInputGuide != 0 {
			guide = true
		}
	}
	if c.focused() {
		kb := FromKeyboard(keyDown)
		pads[0] = Merge(pads[0], kb)
		escape = keyDown(VKEscape)
	}
	return pads, guide, escape
}

func keyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

/*
 * focused reports whether the keyboard is the game's.
 *
 * Compared by root owner, not by handle: the video window is an owned popup,
 * and while a game runs the window in front is the main window or the page
 * overlay above the picture (internal/webview2/overlay.go) — never the video
 * window itself. All three share one root owner, and another program does not.
 */
func (c Controllers) focused() bool {
	if c.HWND == 0 {
		return false
	}
	fg, _, _ := procGetForeground.Call()
	if fg == 0 {
		return false
	}
	const gaRootOwner = 3
	mine, _, _ := procGetAncestor.Call(c.HWND, gaRootOwner)
	theirs, _, _ := procGetAncestor.Call(fg, gaRootOwner)
	return mine != 0 && mine == theirs
}

func syscallN(fn uintptr, args ...uintptr) (uintptr, uintptr, error) {
	r1, r2, errno := syscall.SyscallN(fn, args...)
	return r1, r2, errno
}
