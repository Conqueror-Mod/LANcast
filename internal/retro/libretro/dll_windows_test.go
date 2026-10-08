//go:build windows

package libretro

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

/*
 * The binding against a real DLL: testdata/testcore.c, built here with
 * whatever gcc is on the PATH. Skipped where there is none — which is every
 * CI runner — so this proves the syscall layer on a developer's machine and
 * the logic above it is proven everywhere by the host's fake-core tests.
 */

var (
	testCoreOnce sync.Once
	testCorePath string
	testCoreErr  error
)

/*
 * buildTestCore compiles the test core once per test binary.
 *
 * Not into t.TempDir: a loaded DLL cannot be deleted on Windows, and the
 * binding never unloads one (Close says why), so the directory's cleanup
 * would fail the test that used it. It goes under the OS temp directory and
 * is left there, which costs one small file per run.
 */
func buildTestCore(t *testing.T) string {
	t.Helper()
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("no gcc on the PATH; the binding is tested where one is installed")
	}
	testCoreOnce.Do(func() {
		dir, err := os.MkdirTemp("", "lancast-testcore-")
		if err != nil {
			testCoreErr = err
			return
		}
		testCorePath = filepath.Join(dir, "testcore.dll")
		cmd := exec.Command(gcc, "-shared", "-O2", "-o", testCorePath, filepath.Join("testdata", "testcore.c"))
		if b, err := cmd.CombinedOutput(); err != nil {
			testCoreErr = fmt.Errorf("%v\n%s", err, b)
		}
	})
	if testCoreErr != nil {
		t.Fatalf("building the test core: %v", testCoreErr)
	}
	return testCorePath
}

type recorder struct {
	format   PixelFormat
	vars     []Variable
	values   map[string]string
	frames   []Frame
	pixels   [][]byte
	audio    []int16
	pressA   bool
	sysDir   string
	polls    int
	messages []string
}

func (r *recorder) SetPixelFormat(f PixelFormat) bool { r.format = f; return true }
func (r *recorder) SystemDirectory() string           { return r.sysDir }
func (r *recorder) SaveDirectory() string             { return r.sysDir }
func (r *recorder) Variable(k string) (string, bool) {
	v, ok := r.values[k]
	return v, ok
}
func (r *recorder) SetVariables(v []Variable)     { r.vars = v }
func (r *recorder) VariablesChanged() bool        { return false }
func (r *recorder) SetGeometry(AVInfo)            {}
func (r *recorder) SetSystemAVInfo(AVInfo)        {}
func (r *recorder) Message(text string, _ uint32) { r.messages = append(r.messages, text) }
func (r *recorder) Shutdown()                     {}
func (r *recorder) InputPoll()                    { r.polls++ }
func (r *recorder) AudioBatch(s []int16) int      { r.audio = append(r.audio, s...); return len(s) / 2 }
func (r *recorder) VideoRefresh(f Frame) {
	r.frames = append(r.frames, f)
	var copyOf []byte
	if f.Data != nil {
		copyOf = append([]byte(nil), f.Data...)
	}
	r.pixels = append(r.pixels, copyOf)
}
func (r *recorder) InputState(port, device, index, id uint32) int16 {
	if r.pressA && port == 0 && device == DeviceJoypad && id == JoypadA {
		return 1
	}
	return 0
}

func TestBindingAgainstARealCore(t *testing.T) {
	path := buildTestCore(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	core, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	info := core.SystemInfo()
	if info.LibraryName != "lancast-test" || info.ValidExtensions != "tst|bin" || info.NeedFullpath {
		t.Errorf("system info = %+v", info)
	}

	rec := &recorder{sysDir: `C:\lancast\system`, values: map[string]string{"test_colour": "green"}}
	if err := core.Init(rec); err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	if rec.format != FormatXRGB8888 {
		t.Errorf("pixel format = %d, want XRGB8888", rec.format)
	}
	if len(rec.vars) != 2 || rec.vars[0].Key != "test_colour" || rec.vars[0].Default() != "red" ||
		rec.vars[0].Description != "Colour" || len(rec.vars[0].Values) != 3 {
		t.Errorf("variables = %+v", rec.vars)
	}

	if err := core.LoadGame(`C:\games\x.tst`, []byte{42, 1, 2}); err != nil {
		t.Fatal(err)
	}
	av := core.AVInfo()
	if av.BaseWidth != 4 || av.BaseHeight != 2 || av.FPS != 60 || av.SampleRate != 32768 || av.DisplayAspect() != 2 {
		t.Errorf("av info = %+v", av)
	}

	rec.pressA = true
	for i := 0; i < 3; i++ {
		core.Run()
	}
	if len(rec.frames) != 3 || rec.polls != 3 {
		t.Fatalf("frames %d polls %d, want 3 each", len(rec.frames), rec.polls)
	}
	f := rec.pixels[0]
	if len(f) != 16*2 || binary.LittleEndian.Uint32(f[0:4]) != 0x0000FF00 {
		t.Errorf("first frame = % x, want green pixels (the option value reached the core)", f)
	}
	if rec.pixels[2] != nil {
		t.Error("the third frame should be a repeat with no data")
	}
	if len(rec.audio) != 12 || rec.audio[2] != 7 || rec.audio[3] != -7 {
		t.Errorf("audio = %v", rec.audio)
	}

	sram := core.Memory(MemorySaveRAM)
	if len(sram) != 16 || sram[0] != 2 || sram[1] != 1 || sram[2] != 42 || sram[15] != 3 {
		t.Errorf("save RAM = % x (counter, A seen, game byte, game size)", sram)
	}
	if core.Memory(MemorySystemRAM) != nil {
		t.Error("a region the core does not expose came back non-nil")
	}

	state := make([]byte, core.SerializeSize())
	if !core.Serialize(state) {
		t.Fatal("serialize failed")
	}
	core.Run()
	core.Run()
	if !core.Unserialize(state) {
		t.Fatal("unserialize failed")
	}
	core.Run()
	if got := core.Memory(MemorySaveRAM)[0]; got != 3 {
		t.Errorf("after loading the state the counter ran on from %d, want 3", got)
	}
}

func TestOpenRefusesWhatIsNotACore(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "missing.dll")); err == nil {
		t.Error("a missing DLL opened")
	}
	if _, err := Open(`C:\Windows\System32\kernel32.dll`); err == nil {
		t.Error("kernel32 opened as a core")
	}
}
