/*
 * Package presence is who is watching what, right now, and nothing else.
 *
 * It exists because of [ADR 0045](../../docs/adr/0045-live-presence-between-paired-servers.md),
 * which permits one disclosure that [ADR 0035](../../docs/adr/0035-who-may-see-whose-viewing.md)
 * had forbidden: telling a named person on a paired server that you are
 * watching a particular work while you are still watching it.
 *
 * The whole justification for that permission is a distinction, and this
 * package is where the distinction is either kept or lost. 0035 protects
 * against **a record that accumulates** — something written down, growing,
 * readable later, correlatable. Presence is not that. It answers only *now*, it
 * leaves nothing behind, and when it stops there was never anything behind it.
 * 0045 puts it well: closer to a lit window than to a diary.
 *
 * So the rules below are not defensive coding, they are the argument:
 *
 * # Nothing here is ever persisted
 *
 * No table, no column, no log line, no crash report. A map and a mutex, gone on
 * restart — the same standing `internal/together` gives its rooms, for the same
 * reason it gives: state about the present that survives into a future where it
 * is false is not a record, it is a lie with a timestamp.
 *
 * There is therefore **no presence history and no "last seen watching"**. 0045
 * names that second one as the request that will arrive, that sounds harmless,
 * and that is a record wearing presence's clothes. This package cannot answer
 * it, and that is deliberate: the API to ask does not exist, so nobody can add
 * the feature without first deleting this comment.
 *
 * # The sweep really deletes
 *
 * 0045 §4: "presence that lingers because expiry was implemented as a display
 * filter is persistence with a polite UI". Expiry here is `delete`, and
 * `Snapshot` cannot see an expired entry because there is no expired entry to
 * see.
 *
 * # Record first, then sweep
 *
 * This project has shipped the opposite once. Watch Together swept members
 * before recording the caller's poll, so a host polling exactly on the interval
 * was judged absent and took down their own room mid-film *for being on time*.
 * The federation plan names it as the trap Phase 3 will recur. Every entry
 * point here records before it sweeps, and `TestPunctualHeartbeatSurvives`
 * exists to keep it that way.
 *
 * # Two signals, not one
 *
 * 0045 §3 discloses three things: that somebody is online, that they are
 * watching or idle, and the work by title. Online and watching are different
 * facts arriving from different places at different rates — any authenticated
 * request means online, and only a playback heartbeat means watching — so they
 * expire separately. Collapsing them would make somebody reading their email
 * indistinguishable from somebody halfway through a film.
 */
package presence

import (
	"sort"
	"sync"
	"time"
)

const (
	// onlineTimeout is how long after any authenticated request an account is
	// still "online". Generous, because the client polls several things on
	// varying timers and a person reading a synopsis is plainly still there.
	onlineTimeout = 90 * time.Second

	// watchingTimeout is how long a playback heartbeat means "watching".
	//
	// The client writes progress every five seconds while the picture is
	// moving and stops when it is paused, so this is a wide multiple of the
	// beat rather than a guess: three missed writes is a network hiccup, and
	// twenty seconds of silence is somebody who paused or closed the lid.
	//
	// Erring long here is the wrong error. Presence that lingers after
	// somebody stopped is a small false statement about the present, which is
	// the one thing this package exists not to make.
	watchingTimeout = 20 * time.Second
)

// State is what one account is doing, as much of it as ADR 0045 §3 permits to
// be disclosed and no more.
//
// There is deliberately no position, no duration, no elapsed time, no item id
// and no library. A reader cannot derive "how far in" from anything here,
// because §3 excludes it by name and a field that exists is a field that will
// be rendered.
type State struct {
	// Online is whether the account has been active at all recently.
	Online bool `json:"online"`
	// Watching is the work's title, empty when idle.
	//
	// The *work*: "Cowboy Bebop", never "Cowboy Bebop S01E02 — Stray Dog
	// Strut". 0045 §3 borrows the client's own spoiler reasoning for this —
	// announcing an episode title to a friend three seasons behind is a choice
	// nobody made on purpose.
	Watching string `json:"watching,omitempty"`
}

// Idle reports the case a caller most often wants to phrase differently:
// present, but not watching anything.
func (s State) Idle() bool { return s.Online && s.Watching == "" }

type entry struct {
	lastSeen time.Time
	title    string
	lastBeat time.Time
}

// Tracker holds live presence for the accounts on this server.
//
// The zero value is not usable; call New.
type Tracker struct {
	mu  sync.Mutex
	now func() time.Time
	by  map[string]*entry
}

// New returns an empty tracker.
func New() *Tracker { return &Tracker{now: time.Now, by: map[string]*entry{}} }

// newAt is the same tracker with a clock a test can drive.
func newAt(now func() time.Time) *Tracker { return &Tracker{now: now, by: map[string]*entry{}} }

/*
 * Seen records that an account is active, without saying what it is doing.
 *
 * Called from middleware on any authenticated request, which makes "online" a
 * fact rather than an assumption — and makes it cost nothing, since the request
 * was going to happen anyway.
 *
 * Note the ordering, which is the trap this package's doc comment describes:
 * the caller is recorded *before* the sweep runs, so a request arriving exactly
 * on the expiry boundary refreshes the entry it would otherwise have been
 * judged by.
 */
func (t *Tracker) Seen(userID string) {
	if userID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entryFor(userID).lastSeen = t.now()
	t.sweepLocked()
}

/*
 * Watching records a playback heartbeat.
 *
 * title is the *work's* title and the caller is responsible for having reduced
 * an episode to its show — see State.Watching. An empty title is treated as
 * "not watching", so a caller with nothing to disclose has a way to say so that
 * is not a special case.
 *
 * A heartbeat is also activity, so this refreshes Online too. Recording one and
 * not the other would produce somebody watching a film while offline.
 */
func (t *Tracker) Watching(userID, title string) {
	if userID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entryFor(userID)
	now := t.now()
	e.lastSeen = now
	if title == "" {
		e.title, e.lastBeat = "", time.Time{}
	} else {
		e.title, e.lastBeat = title, now
	}
	t.sweepLocked()
}

/*
 * Stopped clears the watching half immediately, leaving the account online.
 *
 * Without it, closing the player leaves a title standing until the sweep
 * catches it, and "Chris is watching Blade Runner" would outlive Blade Runner
 * by twenty seconds. The sweep is the backstop for clients that vanish; this is
 * the answer for the ordinary case where somebody simply stopped.
 */
func (t *Tracker) Stopped(userID string) {
	if userID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entryFor(userID)
	e.lastSeen = t.now()
	e.title, e.lastBeat = "", time.Time{}
	t.sweepLocked()
}

// Forget removes an account outright — used when a session ends deliberately,
// and when an account is deleted.
func (t *Tracker) Forget(userID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.by, userID)
}

/*
 * Snapshot answers what one account is doing now.
 *
 * ok is false when there is nothing to say, which is not the same as an account
 * that is offline: it means this tracker has never heard of them, or has swept
 * them away. Callers rendering a peer list want to distinguish "offline" from
 * "not sharing with you", and neither of those questions is this one — see the
 * API layer, which knows about grants and this package deliberately does not.
 */
func (t *Tracker) Snapshot(userID string) (State, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked()
	e, ok := t.by[userID]
	if !ok {
		return State{}, false
	}
	return State{Online: true, Watching: e.title}, true
}

// Online lists the accounts currently present, sorted so a caller rendering
// them twice gets the same order twice.
func (t *Tracker) Online() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked()
	out := make([]string, 0, len(t.by))
	for id := range t.by {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (t *Tracker) entryFor(userID string) *entry {
	e, ok := t.by[userID]
	if !ok {
		e = &entry{}
		t.by[userID] = e
	}
	return e
}

/*
 * sweepLocked expires both halves, and it deletes rather than hides.
 *
 * Called from every entry point rather than from a ticker, matching
 * `internal/together`: a background goroutine tending a map that is only
 * interesting while somebody is looking at it is a goroutine that runs all
 * night on an idle server.
 */
func (t *Tracker) sweepLocked() {
	now := t.now()
	for id, e := range t.by {
		if !e.lastBeat.IsZero() && now.Sub(e.lastBeat) > watchingTimeout {
			e.title, e.lastBeat = "", time.Time{}
		}
		if now.Sub(e.lastSeen) > onlineTimeout {
			delete(t.by, id)
		}
	}
}
