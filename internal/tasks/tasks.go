// Package tasks is the board behind Status › Scheduled tasks
// (docs/settings-plan.md, step 3): every background job the server runs, what
// sets it off, when it last ran and how that went, and a way to run it now.
//
// It schedules nothing. The jobs keep their own triggers — a scan finishing,
// a timer, a setting — exactly as before; each reports to the board when it
// starts and finishes, and the board answers what a person wants to know
// about it. That keeps this a window onto the jobs rather than a second
// scheduler that could disagree with the first.
package tasks

import (
	"sort"
	"sync"
	"time"
)

// Trigger says what sets a job off.
type Trigger string

const (
	// AfterScan jobs run when a library scan finishes, and at startup.
	AfterScan Trigger = "after-scan"
	// Interval jobs run on a timer.
	Interval Trigger = "interval"
	// OnDemand jobs run only when somebody asks.
	OnDemand Trigger = "on-demand"
)

// Spec describes one job. Run starts it in the background and returns at
// once; the job reports through Started and Finished. Every and Last are
// optional: Every, for an Interval job, is its period as currently set (read
// each time, since a setting can change it); Last supplies a last run the
// server kept somewhere durable, for jobs whose history should outlive a
// restart. Enabled, when set, says whether the job is switched on at all.
type Spec struct {
	ID          string
	Name        string
	Description string
	Trigger     Trigger
	Every       func() time.Duration
	Last        func() time.Time
	Enabled     func() bool
	// Busy, when set, says the job is running by its own account — for a job
	// whose runs start somewhere the board does not see, such as a scan
	// started from a library's page.
	Busy func() bool
	Run  func()
}

// Task is one job as the board reports it.
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Trigger     Trigger    `json:"trigger"`
	EveryHours  float64    `json:"every_hours,omitempty"`
	Enabled     bool       `json:"enabled"`
	Running     bool       `json:"running"`
	LastStarted *time.Time `json:"last_started_at,omitempty"`
	LastEnded   *time.Time `json:"last_finished_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	NextAt      *time.Time `json:"next_at,omitempty"`
}

type state struct {
	running  int
	started  time.Time
	finished time.Time
	err      string
}

// Board holds the jobs and what they last reported.
type Board struct {
	mu    sync.Mutex
	order []string
	specs map[string]Spec
	state map[string]*state
	now   func() time.Time
}

// New makes an empty board.
func New() *Board {
	return &Board{specs: map[string]Spec{}, state: map[string]*state{}, now: time.Now}
}

// Add registers a job. Adding an id twice replaces it.
func (b *Board) Add(s Spec) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.specs[s.ID]; !ok {
		b.order = append(b.order, s.ID)
	}
	b.specs[s.ID] = s
	if b.state[s.ID] == nil {
		b.state[s.ID] = &state{}
	}
}

// Started records that a job began. Safe on a nil board, so a job can
// report whether or not anything is watching.
func (b *Board) Started(id string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.stateFor(id)
	st.running++
	st.started = b.now()
}

// Finished records that a job ended, with its error or nil.
func (b *Board) Finished(id string, err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.stateFor(id)
	if st.running > 0 {
		st.running--
	}
	st.finished = b.now()
	st.err = ""
	if err != nil {
		st.err = err.Error()
	}
}

// Track wraps one run of a job: Started, the work, Finished with its error.
func (b *Board) Track(id string, work func() error) error {
	b.Started(id)
	err := work()
	b.Finished(id, err)
	return err
}

func (b *Board) stateFor(id string) *state {
	st := b.state[id]
	if st == nil {
		st = &state{}
		b.state[id] = st
	}
	return st
}

// RunNow starts a job, and reports false for one the board does not know or
// that has no way to be run.
func (b *Board) RunNow(id string) bool {
	b.mu.Lock()
	s, ok := b.specs[id]
	b.mu.Unlock()
	if !ok || s.Run == nil {
		return false
	}
	s.Run()
	return true
}

// List reports every job, in the order they were added.
func (b *Board) List() []Task {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Task, 0, len(b.order))
	for _, id := range b.order {
		s := b.specs[id]
		st := b.state[id]
		t := Task{
			ID: s.ID, Name: s.Name, Description: s.Description, Trigger: s.Trigger,
			Enabled:   s.Enabled == nil || s.Enabled(),
			Running:   st.running > 0 || (s.Busy != nil && s.Busy()),
			LastError: st.err,
		}
		started, finished := st.started, st.finished
		// A durable record wins when it is newer than what this run of the
		// server has seen, which is always the case just after a restart.
		if s.Last != nil {
			if last := s.Last(); last.After(finished) {
				finished = last
				if started.IsZero() || started.Before(last) {
					started = last
				}
			}
		}
		if !started.IsZero() {
			v := started
			t.LastStarted = &v
		}
		if !finished.IsZero() {
			v := finished
			t.LastEnded = &v
		}
		if s.Trigger == Interval && s.Every != nil {
			if every := s.Every(); every > 0 {
				t.EveryHours = every.Hours()
				if t.Enabled && !finished.IsZero() {
					next := finished.Add(every)
					t.NextAt = &next
				}
			}
		}
		out = append(out, t)
	}
	return out
}

// IDs lists the registered job ids, sorted, for tests and diagnostics.
func (b *Board) IDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := append([]string(nil), b.order...)
	sort.Strings(ids)
	return ids
}
