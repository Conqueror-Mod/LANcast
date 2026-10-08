// Package libretro hosts emulator cores through the libretro API (ADR 0073).
//
// A core is a DLL exporting retro_* functions that calls back into its host
// for frames, audio, input and questions. This package is the boundary: the
// Windows binding (dll_windows.go) loads a core by full path through syscall,
// the way internal/mpv loads libmpv, and turns every C struct and pointer the
// core hands it into the Go types below before anything above sees them.
//
// Everything above the binding is written against Core and Frontend, so the
// host's logic — pacing, saves, input — is tested with a fake core in Go,
// since pure Go cannot build a real core for tests.
//
// Constants and layouts were checked against libretro.h at libretro-common
// commit 2b96a82b. Where it matters (SET_SERIALIZATION_QUIRKS moved from 44
// to 87) the header won over memory.
package libretro

import "strings"

// APIVersion is the libretro API version this host speaks.
const APIVersion = 1

// Environment commands the host answers. Anything else is answered false,
// which every core must tolerate.
const (
	envExperimental           = 0x10000
	EnvSetRotation            = 1
	EnvGetOverscan            = 2
	EnvGetCanDupe             = 3
	EnvSetMessage             = 6
	EnvShutdown               = 7
	EnvSetPerformanceLevel    = 8
	EnvGetSystemDirectory     = 9
	EnvSetPixelFormat         = 10
	EnvSetInputDescriptors    = 11
	EnvSetHWRender            = 14
	EnvGetVariable            = 15
	EnvSetVariables           = 16
	EnvGetVariableUpdate      = 17
	EnvSetSupportNoGame       = 18
	EnvGetLogInterface        = 27
	EnvGetCoreAssetsDirectory = 30
	EnvGetSaveDirectory       = 31
	EnvSetSystemAVInfo        = 32
	EnvSetControllerInfo      = 35
	EnvSetGeometry            = 37
	EnvGetUsername            = 38
	EnvGetLanguage            = 39
	EnvGetAudioVideoEnable    = 47 | envExperimental
	EnvGetFastforwarding      = 49 | envExperimental
	EnvGetInputBitmasks       = 51 | envExperimental
	EnvGetCoreOptionsVersion  = 52
	EnvSetCoreOptions         = 53
	EnvSetCoreOptionsDisplay  = 55
	EnvGetMessageInterfaceVer = 59
	EnvGetInputMaxUsers       = 61
	EnvSetCoreOptionsV2       = 67
	EnvSetVariable            = 70
	EnvSetSerializationQuirks = 87
)

// PixelFormat is how a core's frames are laid out.
type PixelFormat uint32

const (
	// Format0RGB1555 is the default until a core asks for another.
	Format0RGB1555 PixelFormat = 0
	FormatXRGB8888 PixelFormat = 1
	FormatRGB565   PixelFormat = 2
)

// Supported reports whether the host can draw a format. The 10-bit and HDR
// formats exist in the header and no core this project targets uses them;
// refusing them makes a core fall back rather than draw garbage.
func (f PixelFormat) Supported() bool {
	return f == Format0RGB1555 || f == FormatXRGB8888 || f == FormatRGB565
}

// Input devices and ids.
const (
	DeviceNone     = 0
	DeviceJoypad   = 1
	DeviceKeyboard = 3
	DeviceAnalog   = 5

	// DeviceIDJoypadMask asks for every button at once as a bitmask, when the
	// host has said it supports input bitmasks.
	DeviceIDJoypadMask = 256

	AnalogIndexLeft  = 0
	AnalogIndexRight = 1
	AnalogIDX        = 0
	AnalogIDY        = 1
)

// RetroPad buttons, in libretro's numbering.
const (
	JoypadB = iota
	JoypadY
	JoypadSelect
	JoypadStart
	JoypadUp
	JoypadDown
	JoypadLeft
	JoypadRight
	JoypadA
	JoypadX
	JoypadL
	JoypadR
	JoypadL2
	JoypadR2
	JoypadL3
	JoypadR3
	JoypadButtons // count
)

// Memory regions.
const (
	MemorySaveRAM   = 0
	MemoryRTC       = 1
	MemorySystemRAM = 2
)

// SystemInfo is what a core says about itself before any game is loaded.
type SystemInfo struct {
	LibraryName     string
	LibraryVersion  string
	ValidExtensions string // "gba|gb|gbc", lower case, no dots
	// NeedFullpath means the core reads the game from disk itself and must
	// be given a path rather than bytes.
	NeedFullpath bool
	BlockExtract bool
}

// AVInfo is a loaded game's geometry and timing.
type AVInfo struct {
	BaseWidth, BaseHeight uint32
	MaxWidth, MaxHeight   uint32
	// AspectRatio is the display aspect, or zero for "width / height".
	AspectRatio float32
	FPS         float64
	SampleRate  float64
}

// DisplayAspect is the aspect the picture should be shown at.
func (a AVInfo) DisplayAspect() float64 {
	if a.AspectRatio > 0 {
		return float64(a.AspectRatio)
	}
	if a.BaseHeight == 0 {
		return 4.0 / 3.0
	}
	return float64(a.BaseWidth) / float64(a.BaseHeight)
}

// HWContext is the kind of hardware context a core asks for.
type HWContext uint32

const (
	HWContextNone       HWContext = 0
	HWContextOpenGL     HWContext = 1 // compatibility profile
	HWContextOpenGLES2  HWContext = 2
	HWContextOpenGLCore HWContext = 3
	HWContextOpenGLES3  HWContext = 4
	HWContextVulkan     HWContext = 6
)

// HWRender is a core's request to render through the GPU rather than hand
// over finished frames (SET_HW_RENDER). The N64 cores make it; framebuffer
// cores never do.
type HWRender struct {
	Context          HWContext
	Major, Minor     uint32
	Depth, Stencil   bool
	BottomLeftOrigin bool
	Debug            bool
}

// Frame is one video frame as the core produced it. Data is nil when the
// core repeats the previous frame (it may, because the host says it can dupe).
//
// Data is only valid during the call that delivers it: it is the core's
// memory, and the core reuses it for the next frame. Copy or convert it
// before returning.
type Frame struct {
	Data          []byte
	Width, Height uint32
	Pitch         uintptr
	Format        PixelFormat
	// HW marks a frame the core rendered into the host's framebuffer object
	// (RETRO_HW_FRAME_BUFFER_VALID); Data is nil and Width and Height say how
	// much of the framebuffer it used.
	HW bool
}

// Variable is one core option, in the legacy SET_VARIABLES form every core
// still offers: a key, a description, and its allowed values, first being the
// default.
type Variable struct {
	Key         string
	Description string
	Values      []string
}

// Default is the value a core uses when nothing has been chosen.
func (v Variable) Default() string {
	if len(v.Values) == 0 {
		return ""
	}
	return v.Values[0]
}

// ParseVariable reads one SET_VARIABLES value: "Description; a|b|c".
func ParseVariable(key, value string) Variable {
	v := Variable{Key: key}
	desc, values, found := strings.Cut(value, ";")
	if !found {
		v.Description = strings.TrimSpace(value)
		return v
	}
	v.Description = strings.TrimSpace(desc)
	for _, x := range strings.Split(strings.TrimSpace(values), "|") {
		if x != "" {
			v.Values = append(v.Values, x)
		}
	}
	return v
}

/*
 * Frontend is everything a core may ask of the host, already decoded.
 *
 * The binding answers the environment commands it can express here and
 * declines the rest. Implementations are called on the thread that called
 * Core.Run, which is the one locked thread the core lives on.
 */
type Frontend interface {
	SetPixelFormat(PixelFormat) bool
	SystemDirectory() string
	SaveDirectory() string
	// Variable is the current value of a core option, or false for one the
	// host knows nothing about.
	Variable(key string) (string, bool)
	SetVariables([]Variable)
	// VariablesChanged reports, once, that an option changed since the core
	// last asked.
	VariablesChanged() bool
	SetGeometry(AVInfo)
	SetSystemAVInfo(AVInfo)
	Message(text string, frames uint32)
	Shutdown()

	VideoRefresh(Frame)

	// SetHWRender is asked when a core wants a GPU context. Returning false
	// makes the core fall back or refuse the game; true promises a context by
	// the time Core.ContextReset is called.
	SetHWRender(HWRender) bool
	// CurrentFramebuffer is the framebuffer object the core draws into this
	// frame, and ProcAddress resolves a GL function for it. Both are asked
	// on the core's thread with the context current.
	CurrentFramebuffer() uintptr
	ProcAddress(name string) uintptr
	// AudioBatch receives interleaved stereo samples and returns how many
	// frames (pairs) it consumed.
	AudioBatch(samples []int16) int
	InputPoll()
	InputState(port, device, index, id uint32) int16
}

/*
 * Core is one loaded core.
 *
 * Every method must be called from the same OS thread, the one that loaded
 * it — libretro assumes it, and the OpenGL cores of stage 3 depend on it.
 */
type Core interface {
	SystemInfo() SystemInfo
	// Init sets the host's callbacks and initialises the core.
	Init(Frontend) error
	// LoadGame loads a game by path, with its bytes unless the core needs
	// the full path. The bytes are kept until UnloadGame.
	LoadGame(path string, data []byte) error
	AVInfo() AVInfo
	SetControllerPortDevice(port, device uint32)
	Reset()
	Run()
	SerializeSize() int
	Serialize([]byte) bool
	Unserialize([]byte) bool
	// Memory is a view of a core memory region, or nil. It is the core's
	// own memory: read it on the core's thread and copy before keeping it.
	Memory(id uint32) []byte
	UnloadGame()
	// ContextReset tells a hardware-rendering core its context is ready (or
	// ready again); ContextDestroy that it is about to go. Both do nothing for
	// a core that never asked for one.
	ContextReset()
	ContextDestroy()
	// Close deinitialises the core. It is not unloaded from the process: a
	// DLL that has run is not safely unloadable, and the next game may use
	// the same core.
	Close()
}
