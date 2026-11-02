package host

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/retro/libretro"
)

// lockedLog is a log the session writes from its own thread while the test
// reads it.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// waitForLine waits for want to appear in the log, for the frames it takes.
func waitForLine(t *testing.T, log *lockedLog, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("log lacks %s:\n%s", want, log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var gbaMap = []libretro.MemoryDescriptor{
	{Flags: libretro.MemDescSystemRAM, Ptr: 1, Start: 0x02000000, Len: 0x40000},
	{Flags: libretro.MemDescSystemRAM, Ptr: 2, Start: 0x03000000, Len: 0x8000},
}

// A core's memory map is kept for achievements (ADR 0112), and every game
// says in the log which kind of memory its core gave. That line is how the
// cores LANcast ships are measured rather than assumed, so it is written once
// the game is running, not at load: mGBA and Mupen64Plus-Next send their maps
// after loading, and a line written then said they had none.
func TestTheMemoryMapIsKeptAndLogged(t *testing.T) {
	log := &lockedLog{}
	h := start(t, func(c *Config) {
		c.Core.(*fakeCore).memMap = gbaMap
		c.Log = slog.New(slog.NewTextHandler(log, nil))
	})
	waitForLine(t, log, `msg="retro session: memory map" core=fakecore regions=2 map="02000000+40000 ram 03000000+8000 ram"`)
	h.stop(t)

	if got := h.s.MemoryMap(); len(got) != 2 || got[1].Start != 0x03000000 {
		t.Errorf("memory map = %+v", got)
	}
}

func TestACoreWithoutAMapSaysSo(t *testing.T) {
	log := &lockedLog{}
	h := start(t, func(c *Config) { c.Log = slog.New(slog.NewTextHandler(log, nil)) })
	waitForLine(t, log, `msg="retro session: no memory map" core=fakecore system_ram=0`)
	h.stop(t)

	if h.s.MemoryMap() != nil {
		t.Errorf("memory map = %+v, want none", h.s.MemoryMap())
	}
}

// A map that arrives after the line was written is logged when it comes, so
// the log never ends on a "no memory map" that stopped being true.
func TestALateMapIsLoggedWhenItArrives(t *testing.T) {
	log := &lockedLog{}
	h := start(t, func(c *Config) {
		c.Log = slog.New(slog.NewTextHandler(log, nil))
		c.Core.(*fakeCore).onRun = func(fc *fakeCore) {
			if fc.counter.Load() == memoryLogAfter+10 {
				fc.fe.(libretro.MemoryMapSink).SetMemoryMap(gbaMap)
			}
		}
	})
	waitForLine(t, log, `msg="retro session: no memory map"`)
	waitForLine(t, log, `msg="retro session: memory map" core=fakecore regions=2 map="02000000+40000 ram 03000000+8000 ram" late=true`)
	h.stop(t)
}
