package host

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
 * Saves this session did not load are never written over (R12-4).
 *
 * The fake core changes its save RAM on every frame, which is what a cartridge
 * that formats blank save RAM at boot looks like from here: without the hold,
 * each of these would upload within a millisecond.
 */

// events records every event, for tests that need the ones before "started".
type events struct {
	mu  sync.Mutex
	all []Event
}

func (e *events) wrap(next func(Event)) func(Event) {
	return func(ev Event) {
		e.mu.Lock()
		e.all = append(e.all, ev)
		e.mu.Unlock()
		next(ev)
	}
}

func (e *events) errors(slot string) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Event
	for _, ev := range e.all {
		if ev.Kind == "error" && ev.Slot == slot {
			out = append(out, ev)
		}
	}
	return out
}

func TestUnfetchedSaveRAMIsNeverUploaded(t *testing.T) {
	var seen events
	fs := newFakeSaves()
	fs.failSRAM = errors.New("timeout")
	h := start(t, func(c *Config) {
		c.Saves = fs
		c.OnEvent = seen.wrap(c.OnEvent)
	})
	time.Sleep(50 * time.Millisecond) // many flush intervals, every frame a change
	h.s.Pause()
	h.waitFor(t, "paused") // pausing forces a flush
	h.stop(t)              // and so does closing

	fs.mu.Lock()
	puts := fs.sramPuts
	fs.mu.Unlock()
	if puts != 0 {
		t.Errorf("%d uploads of save RAM the session never loaded: the save on the server would be replaced", puts)
	}
	errs := seen.errors("sram")
	if len(errs) != 1 || !strings.Contains(errs[0].Text, "nothing this game saves") {
		t.Errorf("the person was not told saves are held: %+v", errs)
	}
}

func TestARefusedUploadStopsUploading(t *testing.T) {
	var seen events
	fs := newFakeSaves()
	fs.conflict = true
	h := start(t, func(c *Config) {
		c.Saves = fs
		c.OnEvent = seen.wrap(c.OnEvent)
	})
	deadline := time.Now().Add(5 * time.Second)
	for len(seen.errors("sram")) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	h.stop(t)

	fs.mu.Lock()
	puts := fs.sramPuts
	fs.mu.Unlock()
	errs := seen.errors("sram")
	if len(errs) == 0 || !strings.Contains(errs[0].Text, "changed since") {
		t.Fatalf("a refused upload was not explained: %+v", errs)
	}
	// Uploads already in flight when the first refusal lands may finish, but
	// the session must not keep sending one every interval.
	if puts > 3 {
		t.Errorf("%d uploads after the server refused the first", puts)
	}
}

func TestAnUnfetchedContinuePointIsNotReplaced(t *testing.T) {
	var seen events
	fs := newFakeSaves()
	fs.states["auto"] = []byte{9, 9, 9, 9}
	fs.stateCore["auto"] = [2]string{"fakecore", ""}
	fs.failState = errors.New("server restarting")
	h := start(t, func(c *Config) {
		c.Saves = fs
		c.ResumeState = "auto"
		c.SaveStateOnStop = true
		c.OnEvent = seen.wrap(c.OnEvent)
	})
	h.stop(t)

	fs.mu.Lock()
	auto := fs.states["auto"]
	fs.mu.Unlock()
	if string(auto) != string([]byte{9, 9, 9, 9}) {
		t.Errorf("closing replaced a Continue point it could not fetch: auto = %v", auto)
	}
	if errs := seen.errors("auto"); len(errs) != 1 {
		t.Errorf("the person was not told: %+v", errs)
	}
}

// An empty Continue slot is not a failed fetch: closing writes one.
func TestAnEmptyContinueSlotIsWritten(t *testing.T) {
	h := start(t, func(c *Config) {
		c.ResumeState = "auto"
		c.SaveStateOnStop = true
	})
	h.stop(t)
	h.saves.mu.Lock()
	defer h.saves.mu.Unlock()
	if len(h.saves.states["auto"]) == 0 {
		t.Error("closing wrote no Continue point into an empty slot")
	}
}
