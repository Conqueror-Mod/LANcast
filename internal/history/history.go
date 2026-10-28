// Package history keeps what the server was doing, minute by minute, for the
// dashboard's graphs (ADR 0106): a ring of samples in memory, saved beside
// the database so a restart does not wipe the night before.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Keep is how far back the ring reaches.
const Keep = 48 * time.Hour

// Every is how often a sample is taken.
const Every = time.Minute

// Sample is one minute of the server.
type Sample struct {
	At int64 `json:"at"` // unix seconds, the end of the minute
	// Watching is how many people here are playing something.
	Watching int `json:"watching"`
	// Encoding and Copying are conversions running: re-encoding the picture,
	// or only rewrapping it.
	Encoding int `json:"encoding"`
	Copying  int `json:"copying"`
	// CPU is the whole machine's busy share, 0–100, or -1 where it cannot
	// be read.
	CPU float64 `json:"cpu"`
	// MemoryMB is what this program holds from the operating system.
	MemoryMB float64 `json:"memory_mb"`
	// Requests and Errors are how many requests arrived, and how many were
	// answered with an unexpected server error, during the minute.
	Requests int64 `json:"requests"`
	Errors   int64 `json:"errors"`
	// MediaBytes is media sent during the minute: streams, conversions,
	// downloads and the rest of the media routes. Zero in history saved by a
	// build from before it was counted.
	MediaBytes int64 `json:"media_bytes"`
}

// Ring is the samples, oldest first.
type Ring struct {
	mu      sync.Mutex
	samples []Sample
}

// Add appends a sample and drops what is older than Keep.
func (r *Ring) Add(s Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, s)
	cut := s.At - int64(Keep/time.Second)
	i := 0
	for i < len(r.samples) && r.samples[i].At < cut {
		i++
	}
	if i > 0 {
		r.samples = append([]Sample(nil), r.samples[i:]...)
	}
}

// Since is the samples taken after t, oldest first.
func (r *Ring) Since(t time.Time) []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Sample{}
	for _, s := range r.samples {
		if s.At > t.Unix() {
			out = append(out, s)
		}
	}
	return out
}

// Save writes the ring to path, atomically.
func (r *Ring) Save(path string) error {
	r.mu.Lock()
	raw, err := json.Marshal(r.samples)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a ring saved by Save, keeping what is still inside Keep. A
// missing or unreadable file is an empty history, not an error worth
// stopping for: history is a convenience.
func Load(path string, now time.Time) *Ring {
	r := &Ring{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	var samples []Sample
	if json.Unmarshal(raw, &samples) != nil {
		return r
	}
	cut := now.Add(-Keep).Unix()
	for _, s := range samples {
		if s.At >= cut && s.At <= now.Unix()+60 {
			r.samples = append(r.samples, s)
		}
	}
	return r
}

// LoadInto replaces r's samples with those saved at path, under r's lock, so a
// ring already handed to readers can be filled without swapping it.
func (r *Ring) LoadInto(path string, now time.Time) {
	loaded := Load(path, now)
	r.mu.Lock()
	r.samples = loaded.samples
	r.mu.Unlock()
}

// File is where the ring is kept in a data directory.
func File(dataDir string) string { return filepath.Join(dataDir, "history.json") }

// CPU reads the machine's busy share between two calls.
type CPU struct {
	idle, total uint64
	ok          bool
}

// Percent is the busy share since the last call, or -1 on the first call or
// where the platform cannot be read.
func (c *CPU) Percent() float64 {
	idle, total, ok := systemTimes()
	if !ok {
		return -1
	}
	prevIdle, prevTotal, had := c.idle, c.total, c.ok
	c.idle, c.total, c.ok = idle, total, true
	if !had || total <= prevTotal {
		return -1
	}
	busy := 1 - float64(idle-prevIdle)/float64(total-prevTotal)
	if busy < 0 {
		busy = 0
	}
	return busy * 100
}
