//go:build windows

package libretro

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"syscall"
	"unsafe"
)

/*
 * The libretro binding on Windows: a core DLL loaded by full path through
 * syscall, with no CGO and no link step — the technique internal/mpv uses for
 * libmpv (ADR 0067), pointed at a different DLL.
 *
 * Callbacks from C carry no context, so exactly one core receives them at a
 * time: the one that last called Init. That is a property of libretro itself
 * (a core's callbacks are process-wide), not a limit this host adds.
 *
 * Anything handed to a core that it may keep — a directory string, an option
 * value, the game's bytes — is allocated on the process heap, not in Go
 * memory. A core is entitled to hold the system-directory pointer for its
 * whole life, and a Go allocation it held would be a pointer the collector
 * knows nothing about.
 */

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procGetHeap     = kernel32.NewProc("GetProcessHeap")
	procHeapAlloc   = kernel32.NewProc("HeapAlloc")
	procHeapFree    = kernel32.NewProc("HeapFree")
	callbacksOnce   sync.Once
	cbEnvironment   uintptr
	cbVideoRefresh  uintptr
	cbAudioSample   uintptr
	cbAudioBatch    uintptr
	cbInputPoll     uintptr
	cbInputState    uintptr
	activeMu        sync.Mutex
	active          *dllCore
	errNoActiveCore = errors.New("libretro: callback with no active core")
)

// cPointer turns an address a core handed over back into a pointer. go vet
// cannot see that the address came from C, where its rules do not apply;
// unsafe.Add states that without tripping it.
func cPointer(addr uintptr) unsafe.Pointer { return unsafe.Add(unsafe.Pointer(nil), addr) }

const heapZeroMemory = 0x8

func cAlloc(n uintptr) unsafe.Pointer {
	if n == 0 {
		n = 1
	}
	h, _, _ := procGetHeap.Call()
	p, _, _ := procHeapAlloc.Call(h, heapZeroMemory, n)
	return cPointer(p)
}

func cFree(p unsafe.Pointer) {
	if p == nil {
		return
	}
	h, _, _ := procGetHeap.Call()
	_, _, _ = procHeapFree.Call(h, 0, uintptr(p))
}

// goString reads a NUL-terminated string a core owns.
func goString(p *byte) string {
	if p == nil {
		return ""
	}
	var n int
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
		if n > 1<<20 {
			break
		}
	}
	return string(unsafe.Slice(p, n))
}

// C mirrors of the structs a core exchanges with its host. Layouts match the
// header on 64-bit Windows: Go aligns these fields exactly as MSVC does.
type retroSystemInfo struct {
	libraryName, libraryVersion, validExtensions *byte
	needFullpath, blockExtract                   bool
}

type retroGameGeometry struct {
	baseWidth, baseHeight, maxWidth, maxHeight uint32
	aspectRatio                                float32
}

type retroSystemAVInfo struct {
	geometry        retroGameGeometry
	fps, sampleRate float64
}

type retroGameInfo struct {
	path *byte
	data unsafe.Pointer
	size uintptr
	meta *byte
}

type retroVariable struct {
	key, value *byte
}

type retroMessage struct {
	msg    *byte
	frames uint32
}

type dllCore struct {
	dll   *syscall.DLL
	procs map[string]*syscall.Proc
	info  SystemInfo

	fe     Frontend
	format PixelFormat
	// strs are the C strings handed to the core, kept for its life and by
	// value so asking twice returns the same pointer.
	strs     map[string]unsafe.Pointer
	game     unsafe.Pointer
	gamePath unsafe.Pointer
	loaded   bool
}

var coreFuncs = []string{
	"retro_set_environment", "retro_set_video_refresh", "retro_set_audio_sample",
	"retro_set_audio_sample_batch", "retro_set_input_poll", "retro_set_input_state",
	"retro_init", "retro_deinit", "retro_api_version", "retro_get_system_info",
	"retro_get_system_av_info", "retro_set_controller_port_device", "retro_reset",
	"retro_run", "retro_serialize_size", "retro_serialize", "retro_unserialize",
	"retro_load_game", "retro_unload_game", "retro_get_memory_data", "retro_get_memory_size",
}

// Open loads a core DLL by full path. It is not initialised until Init.
func Open(path string) (Core, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("libretro: load %s: %w", path, err)
	}
	c := &dllCore{dll: dll, procs: map[string]*syscall.Proc{}, strs: map[string]unsafe.Pointer{}}
	for _, name := range coreFuncs {
		p, err := dll.FindProc(name)
		if err != nil {
			return nil, fmt.Errorf("libretro: %s is not a core: %w", path, err)
		}
		c.procs[name] = p
	}
	if v, _, _ := c.procs["retro_api_version"].Call(); uint32(v) != APIVersion {
		return nil, fmt.Errorf("libretro: %s speaks API version %d, not %d", path, uint32(v), APIVersion)
	}
	var si retroSystemInfo
	_, _, _ = c.procs["retro_get_system_info"].Call(uintptr(unsafe.Pointer(&si)))
	c.info = SystemInfo{
		LibraryName:     goString(si.libraryName),
		LibraryVersion:  goString(si.libraryVersion),
		ValidExtensions: goString(si.validExtensions),
		NeedFullpath:    si.needFullpath,
		BlockExtract:    si.blockExtract,
	}
	return c, nil
}

func (c *dllCore) SystemInfo() SystemInfo { return c.info }

func (c *dllCore) call(name string, args ...uintptr) uintptr {
	r, _, _ := c.procs[name].Call(args...)
	return r
}

func (c *dllCore) Init(fe Frontend) error {
	callbacksOnce.Do(func() {
		cbEnvironment = syscall.NewCallback(environmentCB)
		cbVideoRefresh = syscall.NewCallback(videoRefreshCB)
		cbAudioSample = syscall.NewCallback(audioSampleCB)
		cbAudioBatch = syscall.NewCallback(audioBatchCB)
		cbInputPoll = syscall.NewCallback(inputPollCB)
		cbInputState = syscall.NewCallback(inputStateCB)
	})
	activeMu.Lock()
	active = c
	activeMu.Unlock()
	c.fe = fe
	c.format = Format0RGB1555
	// The environment comes first: a core may ask questions from inside
	// retro_init, and the header says set_environment precedes it.
	c.call("retro_set_environment", cbEnvironment)
	c.call("retro_set_video_refresh", cbVideoRefresh)
	c.call("retro_set_audio_sample", cbAudioSample)
	c.call("retro_set_audio_sample_batch", cbAudioBatch)
	c.call("retro_set_input_poll", cbInputPoll)
	c.call("retro_set_input_state", cbInputState)
	c.call("retro_init")
	return nil
}

func (c *dllCore) LoadGame(path string, data []byte) error {
	gi := retroGameInfo{path: (*byte)(c.cstr(path))}
	c.gamePath = unsafe.Pointer(gi.path)
	if !c.info.NeedFullpath && len(data) > 0 {
		c.game = cAlloc(uintptr(len(data)))
		copy(unsafe.Slice((*byte)(c.game), len(data)), data)
		gi.data, gi.size = c.game, uintptr(len(data))
	}
	if c.call("retro_load_game", uintptr(unsafe.Pointer(&gi)))&0xff == 0 {
		c.freeGame()
		return errors.New("libretro: the core could not load this game")
	}
	c.loaded = true
	return nil
}

func (c *dllCore) freeGame() {
	cFree(c.game)
	c.game = nil
}

func (c *dllCore) AVInfo() AVInfo {
	var av retroSystemAVInfo
	c.call("retro_get_system_av_info", uintptr(unsafe.Pointer(&av)))
	return avInfo(av)
}

func avInfo(av retroSystemAVInfo) AVInfo {
	return AVInfo{
		BaseWidth: av.geometry.baseWidth, BaseHeight: av.geometry.baseHeight,
		MaxWidth: av.geometry.maxWidth, MaxHeight: av.geometry.maxHeight,
		AspectRatio: av.geometry.aspectRatio, FPS: av.fps, SampleRate: av.sampleRate,
	}
}

func (c *dllCore) SetControllerPortDevice(port, device uint32) {
	c.call("retro_set_controller_port_device", uintptr(port), uintptr(device))
}

func (c *dllCore) Reset() { c.call("retro_reset") }
func (c *dllCore) Run()   { c.call("retro_run") }

func (c *dllCore) SerializeSize() int { return int(c.call("retro_serialize_size")) }

func (c *dllCore) Serialize(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	return c.call("retro_serialize", uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))&0xff != 0
}

func (c *dllCore) Unserialize(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	return c.call("retro_unserialize", uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))&0xff != 0
}

func (c *dllCore) Memory(id uint32) []byte {
	p := c.call("retro_get_memory_data", uintptr(id))
	n := c.call("retro_get_memory_size", uintptr(id))
	if p == 0 || n == 0 || n > math.MaxInt32 {
		return nil
	}
	return unsafe.Slice((*byte)(cPointer(p)), int(n))
}

func (c *dllCore) UnloadGame() {
	if c.loaded {
		c.call("retro_unload_game")
		c.loaded = false
	}
	c.freeGame()
}

func (c *dllCore) Close() {
	c.UnloadGame()
	c.call("retro_deinit")
	activeMu.Lock()
	if active == c {
		active = nil
	}
	activeMu.Unlock()
	for k, p := range c.strs {
		cFree(p)
		delete(c.strs, k)
	}
}

// cstr is a C copy of s that lives as long as the core.
func (c *dllCore) cstr(s string) unsafe.Pointer {
	if p, ok := c.strs[s]; ok {
		return p
	}
	p := cAlloc(uintptr(len(s) + 1))
	copy(unsafe.Slice((*byte)(p), len(s)), s)
	c.strs[s] = p
	return p
}

func current() (*dllCore, error) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if active == nil || active.fe == nil {
		return nil, errNoActiveCore
	}
	return active, nil
}

func boolResult(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// environmentCB answers a core's questions: bool (unsigned cmd, void *data).
func environmentCB(cmd, data uintptr) uintptr {
	c, err := current()
	if err != nil {
		return 0
	}
	return boolResult(c.environment(uint32(cmd), cPointer(data)))
}

func (c *dllCore) environment(cmd uint32, data unsafe.Pointer) bool {
	fe := c.fe
	switch cmd {
	case EnvGetCanDupe:
		if data != nil {
			*(*bool)(data) = true
		}
		return true
	case EnvSetPixelFormat:
		if data == nil {
			return false
		}
		f := PixelFormat(*(*uint32)(data))
		if !f.Supported() || !fe.SetPixelFormat(f) {
			return false
		}
		c.format = f
		return true
	case EnvGetSystemDirectory, EnvGetCoreAssetsDirectory:
		if data == nil {
			return false
		}
		*(*unsafe.Pointer)(data) = c.cstr(fe.SystemDirectory())
		return true
	case EnvGetSaveDirectory:
		if data == nil {
			return false
		}
		*(*unsafe.Pointer)(data) = c.cstr(fe.SaveDirectory())
		return true
	case EnvGetVariable:
		if data == nil {
			return false
		}
		v := (*retroVariable)(data)
		val, ok := fe.Variable(goString(v.key))
		if !ok {
			v.value = nil
			return false
		}
		v.value = (*byte)(c.cstr(val))
		return true
	case EnvSetVariables:
		if data == nil {
			return false
		}
		var vars []Variable
		for p := (*retroVariable)(data); p.key != nil; p = (*retroVariable)(unsafe.Add(unsafe.Pointer(p), unsafe.Sizeof(retroVariable{}))) {
			vars = append(vars, ParseVariable(goString(p.key), goString(p.value)))
			if len(vars) > 4096 {
				break
			}
		}
		fe.SetVariables(vars)
		return true
	case EnvGetVariableUpdate:
		if data == nil {
			return false
		}
		*(*bool)(data) = fe.VariablesChanged()
		return true
	case EnvSetGeometry:
		if data == nil {
			return false
		}
		g := *(*retroGameGeometry)(data)
		fe.SetGeometry(AVInfo{BaseWidth: g.baseWidth, BaseHeight: g.baseHeight,
			MaxWidth: g.maxWidth, MaxHeight: g.maxHeight, AspectRatio: g.aspectRatio})
		return true
	case EnvSetSystemAVInfo:
		if data == nil {
			return false
		}
		fe.SetSystemAVInfo(avInfo(*(*retroSystemAVInfo)(data)))
		return true
	case EnvSetMessage:
		if data == nil {
			return false
		}
		m := (*retroMessage)(data)
		fe.Message(goString(m.msg), m.frames)
		return true
	case EnvShutdown:
		fe.Shutdown()
		return true
	case EnvGetLanguage:
		if data != nil {
			*(*uint32)(data) = 0 // English
		}
		return true
	case EnvGetAudioVideoEnable:
		if data != nil {
			*(*int32)(data) = 3 // video and audio
		}
		return true
	case EnvGetFastforwarding:
		if data != nil {
			*(*bool)(data) = false
		}
		return true
	case EnvGetInputMaxUsers:
		if data != nil {
			*(*uint32)(data) = 4
		}
		return true
	case EnvSetPerformanceLevel, EnvSetInputDescriptors, EnvSetControllerInfo,
		EnvSetSupportNoGame, EnvSetSerializationQuirks:
		// Acknowledged and ignored: nothing here changes what the host does.
		return true
	}
	/*
	 * Declined: core options v1 and v2 (a core falls back to SET_VARIABLES
	 * when GET_CORE_OPTIONS_VERSION is refused), hardware rendering until
	 * stage 3, the log interface (its callback is variadic, which a Go
	 * callback cannot be), rotation, and everything else.
	 */
	return false
}

// videoRefreshCB: void (const void *data, unsigned width, unsigned height, size_t pitch).
func videoRefreshCB(data, width, height, pitch uintptr) uintptr {
	c, err := current()
	if err != nil {
		return 0
	}
	f := Frame{Width: uint32(width), Height: uint32(height), Pitch: pitch, Format: c.format}
	if data != 0 && f.Height > 0 && pitch > 0 && pitch < 1<<20 && f.Height < 1<<16 {
		f.Data = unsafe.Slice((*byte)(cPointer(data)), int(pitch)*int(f.Height))
	}
	c.fe.VideoRefresh(f)
	return 0
}

// audioSampleCB: void (int16_t left, int16_t right). One frame at a time, from
// cores that do not batch; passed on as a batch of one.
func audioSampleCB(left, right uintptr) uintptr {
	c, err := current()
	if err != nil {
		return 0
	}
	s := [2]int16{int16(left), int16(right)}
	c.fe.AudioBatch(s[:])
	return 0
}

// audioBatchCB: size_t (const int16_t *data, size_t frames).
func audioBatchCB(data, frames uintptr) uintptr {
	c, err := current()
	if err != nil || data == 0 || frames == 0 || frames > 1<<20 {
		return frames
	}
	return uintptr(c.fe.AudioBatch(unsafe.Slice((*int16)(cPointer(data)), int(frames)*2)))
}

// inputPollCB: void (void).
func inputPollCB() uintptr {
	if c, err := current(); err == nil {
		c.fe.InputPoll()
	}
	return 0
}

// inputStateCB: int16_t (unsigned port, unsigned device, unsigned index, unsigned id).
func inputStateCB(port, device, index, id uintptr) uintptr {
	c, err := current()
	if err != nil {
		return 0
	}
	return uintptr(uint16(c.fe.InputState(uint32(port), uint32(device), uint32(index), uint32(id))))
}
