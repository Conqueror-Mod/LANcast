//go:build windows

package cheevos

import (
	"archive/zip"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"lancast/internal/retro/libretro"
)

/*
 * The binding against the real DLL: third_party/rcheevos built here with
 * whatever gcc is on the PATH, with build.sh's flags. Skipped where there is
 * none, as internal/retro/libretro's test core is.
 *
 * The shim keeps one client per process, so no test here runs in parallel,
 * and each closes what it opens.
 */

var (
	dllOnce sync.Once
	dllPath string
	dllErr  string
)

func buildDLL(t *testing.T) string {
	t.Helper()
	// CHEEVOS_DLL tests a DLL built elsewhere instead: the one a release
	// cross-compiles with mingw on Linux, which is not the gcc found here.
	if p := os.Getenv("CHEEVOS_DLL"); p != "" {
		return p
	}
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("no gcc on the PATH; the binding is tested where one is installed")
	}
	dllOnce.Do(func() {
		root := filepath.Join("..", "..", "..", "third_party", "rcheevos")
		var sources []string
		_ = filepath.WalkDir(filepath.Join(root, "upstream", "src"), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".c") && d.Name() != "rc_client_raintegration.c" {
				sources = append(sources, p)
			}
			return nil
		})
		sort.Strings(sources)
		sources = append(sources, filepath.Join(root, "shim", "lancast_cheevos.c"))
		dir, err := os.MkdirTemp("", "lancast-cheevos-")
		if err != nil {
			dllErr = err.Error()
			return
		}
		dllPath = filepath.Join(dir, "lancast-cheevos.dll")
		args := []string{"-shared", "-O2", "-std=c99", "-DRC_CLIENT_SUPPORTS_HASH", "-D_CRT_SECURE_NO_WARNINGS",
			"-I" + filepath.Join(root, "upstream", "include"), "-I" + filepath.Join(root, "upstream", "src"),
			"-static", "-o", dllPath}
		out, err := exec.Command(gcc, append(args, sources...)...).CombinedOutput()
		if err != nil {
			dllErr = err.Error() + "\n" + string(out)
		}
	})
	if dllErr != "" {
		t.Fatalf("building the DLL: %s", dllErr)
	}
	return dllPath
}

// recorder is a Handler that keeps everything it is told.
type recorder struct {
	mu       sync.Mutex
	requests chan Request
	done     chan doneCall
	events   []Event
	logs     []string
}

type doneCall struct {
	op     Op
	result int
	err    string
}

func newRecorder() *recorder {
	return &recorder{requests: make(chan Request, 16), done: make(chan doneCall, 16)}
}

func (r *recorder) Request(q Request) { r.requests <- q }
func (r *recorder) Event(e Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}
func (r *recorder) Log(s string) {
	r.mu.Lock()
	r.logs = append(r.logs, s)
	r.mu.Unlock()
}
func (r *recorder) Done(op Op, result int, err string) { r.done <- doneCall{op, result, err} }

func open(t *testing.T) (*Client, *recorder) {
	t.Helper()
	rec := newRecorder()
	c, err := Open(buildDLL(t), rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, rec
}

func nextRequest(t *testing.T, rec *recorder) Request {
	t.Helper()
	select {
	case q := <-rec.requests:
		return q
	case <-time.After(5 * time.Second):
		t.Fatal("rcheevos made no request")
		return Request{}
	}
}

func nextDone(t *testing.T, rec *recorder) doneCall {
	t.Helper()
	select {
	case d := <-rec.done:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("no completion")
		return doneCall{}
	}
}

func TestOneClientAtATime(t *testing.T) {
	path := buildDLL(t)
	c, err := Open(path, newRecorder())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, newRecorder()); err == nil {
		t.Fatal("a second client opened while the first was open")
	}
	c.Close()
	c.Close() // twice is harmless
	again, err := Open(path, newRecorder())
	if err != nil {
		t.Fatalf("reopening after Close: %v", err)
	}
	again.Close()
}

func TestUserAgentClauseNamesTheVendoredVersion(t *testing.T) {
	c, _ := open(t)
	// rcheevos gives major.minor only; v12.5.0 is the tag (PROVENANCE.md).
	if got := c.UserAgentClause(); got != "rcheevos/12.5" {
		t.Fatalf("user agent clause = %q, want rcheevos/12.5", got)
	}
}

// A Game Boy Advance core describes its RAM only through a map. Achievement
// addresses are RetroAchievements' own: 0 is the console's 0x03000000 (the
// 32 KiB internal work RAM) and 0x8000 is 0x02000000 (the 256 KiB external
// one). Reading the right byte at both proves the map is being followed,
// not the order the regions were given in.
func TestMemoryIsReadThroughTheMap(t *testing.T) {
	c, _ := open(t)
	ewram := make([]byte, 0x40000)
	iwram := make([]byte, 0x8000)
	ewram[0x20] = 0xCD
	iwram[0x10] = 0xAB
	mmap := []libretro.MemoryDescriptor{
		{Flags: libretro.MemDescSystemRAM, Ptr: ptrOf(ewram), Start: 0x02000000, Len: uint64(len(ewram))},
		{Flags: libretro.MemDescSystemRAM, Ptr: ptrOf(iwram), Start: 0x03000000, Len: uint64(len(iwram))},
	}
	if !c.SetMemory(GameBoyAdv, mmap, nil, nil) {
		t.Fatal("SetMemory found no memory in the map")
	}
	if c.MemoryRegions() == 0 {
		t.Fatal("no regions")
	}
	if got := c.ReadMemory(0x10, 1); len(got) != 1 || got[0] != 0xAB {
		t.Fatalf("RA 0x10 = %x, want ab (IWRAM 0x03000010)", got)
	}
	if got := c.ReadMemory(0x8020, 1); len(got) != 1 || got[0] != 0xCD {
		t.Fatalf("RA 0x8020 = %x, want cd (EWRAM 0x02000020)", got)
	}
	// The core's memory is read in place, not copied at registration.
	iwram[0x10] = 0x5A
	if got := c.ReadMemory(0x10, 1); len(got) != 1 || got[0] != 0x5A {
		t.Fatalf("after a write, RA 0x10 = %x, want 5a", got)
	}
	runtime.KeepAlive(ewram)
	runtime.KeepAlive(iwram)
}

// A core that sends no map (BlastEm, measured in step 1) is read through its
// system RAM block, laid over the console's regions: an NES's 2 KiB of RAM
// at 0. The mirrors above it are rcheevos' "virtual" RAM, which it backs only
// from a map, so the edges of the block are what is asserted.
func TestMemoryWithoutAMapUsesSystemRAM(t *testing.T) {
	c, _ := open(t)
	ram := make([]byte, 0x800)
	ram[5] = 0x42
	if !c.SetMemory(NES, nil, ram, nil) {
		t.Fatal("SetMemory found no memory")
	}
	if got := c.ReadMemory(5, 1); len(got) != 1 || got[0] != 0x42 {
		t.Fatalf("RA 0x5 = %x, want 42", got)
	}
	ram[0x7FF] = 0x99
	if got := c.ReadMemory(0x7FF, 1); len(got) != 1 || got[0] != 0x99 {
		t.Fatalf("RA 0x7ff, the last byte of RAM = %x, want 99", got)
	}
	runtime.KeepAlive(ram)
}

func TestNoMemoryAtAllIsReported(t *testing.T) {
	c, _ := open(t)
	if c.SetMemory(NES, nil, nil, nil) {
		t.Fatal("SetMemory reported memory where there is none")
	}
	if got := c.ReadMemory(0, 1); len(got) != 0 {
		t.Fatalf("read %x from no memory", got)
	}
}

// ptrOf is a Go buffer's address, standing in for a core's memory. Go's
// collector does not move heap objects, and each test keeps its buffers alive
// past its last read.
func ptrOf(b []byte) uintptr { return uintptr(unsafe.Pointer(&b[0])) }

func md5Hex(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

// rc_hash's rules, checked against an MD5 computed here: an NES dump is
// hashed without its 16-byte iNES header; a Game Boy Advance one whole.
func TestHashFollowsEachConsolesRule(t *testing.T) {
	c, _ := open(t)
	payload := make([]byte, 0x8000)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	nes := append([]byte("NES\x1a\x02\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), payload...)
	if got, ok := c.Hash(NES, "", nes); !ok || got != md5Hex(payload) {
		t.Fatalf("NES hash = %q %v, want the MD5 of the dump without its header %s", got, ok, md5Hex(payload))
	}
	if got, ok := c.Hash(GameBoyAdv, "", payload); !ok || got != md5Hex(payload) {
		t.Fatalf("GBA hash = %q %v, want %s", got, ok, md5Hex(payload))
	}

	// From a path, the same answer as from the bytes.
	path := filepath.Join(t.TempDir(), "game.nes")
	if err := os.WriteFile(path, nes, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Hash(NES, path, nil); !ok || got != md5Hex(payload) {
		t.Fatalf("NES hash from a path = %q %v, want %s", got, ok, md5Hex(payload))
	}
	if _, ok := c.Hash(NES, filepath.Join(t.TempDir(), "missing.nes"), nil); ok {
		t.Fatal("hashed a file that does not exist")
	}
}

// Login goes out as a request for Go to make, and comes back through
// Respond. Nothing here touches the network.
func TestLoginIsARequestGoAnswers(t *testing.T) {
	c, rec := open(t)
	if err := c.Login("Tester", "tok123"); err != nil {
		t.Fatal(err)
	}
	q := nextRequest(t, rec)
	for _, want := range []string{"r=login2", "u=Tester", "t=tok123"} {
		if !strings.Contains(q.PostData, want) {
			t.Fatalf("login post data %q lacks %q", q.PostData, want)
		}
	}
	if !strings.Contains(q.URL, "dorequest.php") {
		t.Fatalf("login URL = %q", q.URL)
	}
	go c.Respond(q.ID, []byte(`{"Success":true,"User":"Tester","Token":"tok123","Score":10,"SoftcoreScore":5,"Messages":0}`), 200)
	if d := nextDone(t, rec); d.op != OpLogin || d.result != OK {
		t.Fatalf("login done = %+v, want OpLogin OK", d)
	}
}

func TestLoginRefusedSaysWhy(t *testing.T) {
	c, rec := open(t)
	if err := c.Login("Tester", "stale"); err != nil {
		t.Fatal(err)
	}
	q := nextRequest(t, rec)
	go c.Respond(q.ID, []byte(`{"Success":false,"Error":"Invalid user/token combination.","Code":"invalid_credentials"}`), 401)
	d := nextDone(t, rec)
	if d.op != OpLogin || d.result == OK || !strings.Contains(d.err, "Invalid user/token") {
		t.Fatalf("refused login done = %+v", d)
	}
}

// An answer that arrives after the client has gone — the player quit while
// the server was still asking RetroAchievements — is dropped, not delivered
// into freed memory.
func TestAnAnswerAfterCloseIsDropped(t *testing.T) {
	path := buildDLL(t)
	rec := newRecorder()
	c, err := Open(path, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("Tester", "tok"); err != nil {
		t.Fatal(err)
	}
	q := nextRequest(t, rec)
	c.Close()
	c.Respond(q.ID, []byte(`{"Success":true,"User":"Tester","Token":"tok"}`), 200)
	// A fresh client's ids may repeat the old one's; the old answer must not
	// reach the new client either.
	c2, err := Open(path, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	c.Respond(q.ID, []byte(`{"Success":true,"User":"Tester","Token":"tok"}`), 200)
	select {
	case d := <-rec.done:
		t.Fatalf("a dropped answer completed: %+v", d)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestProgressWithNoGameIsEmpty(t *testing.T) {
	c, _ := open(t)
	if _, err := c.Progress(); err != nil {
		t.Fatalf("progress with no game: %v", err)
	}
}

// Real dumps, opt-in: RETRO_LIB names a folder of games, loose or zipped.
// Each cartridge is checked against an MD5 computed here by its console's
// rule (expectedHash), so rcheevos is not being compared with itself.
func TestRealDumpHashes(t *testing.T) {
	lib := os.Getenv("RETRO_LIB")
	if lib == "" {
		t.Skip("set RETRO_LIB to a folder of game dumps")
	}
	c, _ := open(t)
	counts := map[Console]int{}
	_ = filepath.WalkDir(lib, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// A .bin is a Mega Drive cartridge or a disc track; no cartridge
		// here is bigger than 64 MiB (N64's largest), and no disc smaller.
		if info, err := d.Info(); err != nil || info.Size() > 64<<20 {
			return nil
		}
		name, data, err := readDump(p)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
			return nil
		}
		console, ok := dumpConsoles[strings.ToLower(filepath.Ext(name))]
		if !ok {
			return nil
		}
		want := expectedHash(console, data)
		if got, ok := c.Hash(console, "", data); !ok || got != want {
			t.Errorf("%s: hash %q, want %s", name, got, want)
		}
		counts[console]++
		return nil
	})
	if len(counts) == 0 {
		t.Skip("no cartridge dumps under RETRO_LIB")
	}
	t.Logf("hashed as RetroAchievements does, by console id: %v", counts)
}

var dumpConsoles = map[string]Console{
	".nes": NES, ".gb": GameBoy, ".gbc": GameBoyColor, ".gba": GameBoyAdv,
	".sfc": SNES, ".smc": SNES, ".md": MegaDrive, ".gen": MegaDrive, ".bin": MegaDrive,
	".z64": Nintendo64, ".v64": Nintendo64, ".n64": Nintendo64,
}

// readDump reads a loose dump, or the one dump inside a zip.
func readDump(path string) (string, []byte, error) {
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		b, err := os.ReadFile(path)
		return filepath.Base(path), b, err
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, err
	}
	defer z.Close()
	for _, f := range z.File {
		if _, ok := dumpConsoles[strings.ToLower(filepath.Ext(f.Name))]; !ok {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return "", nil, err
		}
		b, err := io.ReadAll(r)
		r.Close()
		return f.Name, b, err
	}
	return filepath.Base(path), nil, nil
}

// expectedHash is RetroAchievements' documented rule for each console,
// written out independently of rcheevos:
//   - NES: MD5 without the 16-byte iNES header;
//   - SNES: without a 512-byte copier header, where the size says one is there;
//   - N64: of the big-endian (.z64) byte order, whatever order the file is in;
//   - the rest: MD5 of the whole file.
func expectedHash(console Console, data []byte) string {
	switch console {
	case NES:
		if len(data) > 16 && string(data[:4]) == "NES" {
			data = data[16:]
		}
	case SNES:
		if len(data)%0x2000 == 512 {
			data = data[512:]
		}
	case Nintendo64:
		if len(data) > 0 && data[0] != 0x80 {
			swapped := make([]byte, len(data))
			copy(swapped, data)
			switch data[0] {
			case 0x37: // .v64: each pair of bytes swapped
				for i := 0; i+1 < len(swapped); i += 2 {
					swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
				}
			case 0x40: // .n64: little endian, each word reversed
				for i := 0; i+3 < len(swapped); i += 4 {
					swapped[i], swapped[i+1], swapped[i+2], swapped[i+3] = swapped[i+3], swapped[i+2], swapped[i+1], swapped[i]
				}
			}
			data = swapped
		}
	}
	return md5Hex(data)
}
