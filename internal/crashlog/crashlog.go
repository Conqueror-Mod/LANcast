// Package crashlog records panics as readable reports beside the database.
//
// A panic in an HTTP handler today kills the connection and leaves a stack
// trace in lancastd.log, mixed in with every other line — findable only by
// somebody who already suspects it happened, at the moment they least want to
// be reading a log file. A crash is not a log line. It is an event with a
// beginning, a cause and a stack, and it deserves to be countable.
//
// Reports are files in the data directory, one JSON object each. No database:
// the crash that matters most is the one that happened while the database was
// the thing going wrong, and a crash reporter that needs the failing subsystem
// to record a failure is a crash reporter that loses exactly the crashes worth
// having.
//
// Nothing is sent anywhere. LANcast does not phone home, and "except for crash
// reports" is how every product that phones home began.
package crashlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DirName is the subdirectory of the data directory holding reports.
const DirName = "crashes"

// keep is how many reports survive. A crash loop can produce thousands, and the
// hundredth copy of one stack answers nothing the first did not — but the
// *oldest* are the ones dropped, because the first crash after a change is the
// informative one and the flood after it is the same fault repeating.
const keep = 50

// Report is one recovered panic.
type Report struct {
	// ID is the file's basename, which is also its sort key: a report is
	// identified by when it happened, and two crashes in the same millisecond
	// are separated by the counter.
	ID   string `json:"id"`
	At   int64  `json:"at"`
	Kind string `json:"kind"`
	// Where is the route or worker that panicked, not a file and line — the
	// stack has those. "GET /api/items/{id}" is the sentence somebody can act
	// on; runtime/panic.go:914 is not.
	Where   string `json:"where"`
	Value   string `json:"value"`
	Stack   string `json:"stack"`
	Version string `json:"version"`
}

// Recorder writes reports into a directory.
type Recorder struct {
	dir     string
	version string

	mu  sync.Mutex
	seq int
}

// New returns a Recorder writing under dataDir. The directory is created
// lazily on the first crash: an empty `crashes` folder in every installation
// that has never crashed is a folder people ask about.
func New(dataDir, version string) *Recorder {
	return &Recorder{dir: filepath.Join(dataDir, DirName), version: version}
}

// Record writes a report and returns it. Errors are swallowed deliberately and
// returned rather than logged here — the caller is already in the middle of
// handling a panic, and a crash reporter that panics is not a feature.
func (r *Recorder) Record(where string, value any, stack []byte) (Report, error) {
	r.mu.Lock()
	r.seq++
	seq := r.seq
	r.mu.Unlock()

	now := time.Now()
	rep := Report{
		ID:      fmt.Sprintf("%s-%03d", now.UTC().Format("20060102-150405.000"), seq%1000),
		At:      now.UnixMilli(),
		Kind:    "panic",
		Where:   where,
		Value:   fmt.Sprint(value),
		Stack:   string(stack),
		Version: r.version,
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return rep, err
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return rep, err
	}
	// Written whole and renamed into place, so a reader never sees half a
	// report — the process that just panicked is the one most likely to be
	// killed between two writes.
	tmp := filepath.Join(r.dir, rep.ID+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return rep, err
	}
	if err := os.Rename(tmp, filepath.Join(r.dir, rep.ID+".json")); err != nil {
		_ = os.Remove(tmp)
		return rep, err
	}
	r.prune()
	return rep, nil
}

// List returns reports newest first.
func (r *Recorder) List() ([]Report, error) {
	names, err := r.files()
	if err != nil || len(names) == 0 {
		return []Report{}, err
	}
	out := make([]Report, 0, len(names))
	// Newest first: the filenames sort chronologically by construction, so
	// reversing the sorted list is the whole of the ordering.
	for i := len(names) - 1; i >= 0; i-- {
		data, err := os.ReadFile(filepath.Join(r.dir, names[i]))
		if err != nil {
			continue
		}
		var rep Report
		if err := json.Unmarshal(data, &rep); err != nil {
			// A corrupt report is itself worth seeing rather than silently
			// skipping — something wrote it, and a gap explains nothing.
			rep = Report{
				ID: strings.TrimSuffix(names[i], ".json"), Kind: "unreadable",
				Value: "this report could not be parsed",
			}
		}
		out = append(out, rep)
	}
	return out, nil
}

// Clear removes every report. Offered because the crash list is one of the few
// screens whose ideal state is empty, and a fault fixed three versions ago
// should not still be the first thing an operator sees.
func (r *Recorder) Clear() error {
	names, err := r.files()
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := os.Remove(filepath.Join(r.dir, n)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Recorder) files() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r *Recorder) prune() {
	names, err := r.files()
	if err != nil || len(names) <= keep {
		return
	}
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(r.dir, n))
	}
}
