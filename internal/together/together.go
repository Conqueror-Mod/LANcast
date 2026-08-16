// Package together holds watch-together sessions: several people playing the
// same thing at the same position.
//
// The whole design question is where the truth lives, and the project's first
// principle answers it — the server owns truth. A session is server state: what
// is playing, where it is, whether it is paused, and who is in it. Clients
// follow. The alternative, where each client broadcasts its own position, makes
// the last writer win, and on a lossy connection that is whoever lagged worst.
//
// In memory, deliberately, with no schema behind it. A session means nothing
// after a restart: persisting one would resurrect a film nobody is watching and
// invite a client to rejoin a room whose other members went home hours ago.
// This is live state in the same sense scan progress is live state.
//
// Polling rather than sockets. Nothing else in this stack streams, and adding a
// socket layer for one feature is the dependency argument ADR 0013 settled for
// hls.js. A second of drift is acceptable for "we are watching this together";
// frame accuracy is not the goal and could not be delivered to three devices
// over a LAN in any case.
package together

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("no such session")
	ErrNotHost   = errors.New("only the host can do that")
	ErrNotMember = errors.New("not in this session")
)

// idleTimeout is how long a session survives with nobody polling it. A tab
// closed without leaving is the ordinary way a session ends — nobody presses
// "leave", they shut the laptop — so the room has to be able to notice that on
// its own or the list fills with ghosts.
const idleTimeout = 90 * time.Second

// Member is one participant. Names are resolved by the caller and frozen here,
// the same way the audit log freezes an actor: a room should still read
// correctly while somebody is being renamed.
type Member struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Host   bool   `json:"host"`
	// LastSeen is when this member last polled. Reported because "who is
	// actually here" is the question a room list answers, and a member who
	// closed their laptop is still in the map until the sweep.
	LastSeen int64 `json:"last_seen"`
}

// Session is a room, as clients see it.
type Session struct {
	ID     string `json:"id"`
	ItemID int64  `json:"item_id"`
	// HostID is who drives. Transport control is deliberately not shared: two
	// people scrubbing the same film is not synchronised playback, it is a
	// fight, and the loser cannot tell it from a bug.
	HostID string `json:"host_id"`
	// PositionMS and Paused are the truth clients converge on.
	PositionMS int64 `json:"position_ms"`
	Paused     bool  `json:"paused"`
	// UpdatedAt is when the host last reported. A follower uses it to work out
	// how far the film has moved since — without it, every poll would land a
	// client one poll-interval behind and it would never catch up.
	UpdatedAt int64    `json:"updated_at"`
	Members   []Member `json:"members"`
	CreatedAt int64    `json:"created_at"`
}

type room struct {
	id         string
	itemID     int64
	hostID     string
	positionMS int64
	paused     bool
	updatedAt  time.Time
	createdAt  time.Time
	members    map[string]*Member
}

// Manager owns every live session.
type Manager struct {
	mu    sync.Mutex
	rooms map[string]*room
	now   func() time.Time
}

func New() *Manager {
	return &Manager{rooms: map[string]*room{}, now: time.Now}
}

// Create opens a room with its creator as host.
func (m *Manager) Create(itemID int64, userID, name string, positionMS int64) Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()

	now := m.now()
	r := &room{
		id:         newID(),
		itemID:     itemID,
		hostID:     userID,
		positionMS: positionMS,
		paused:     false,
		updatedAt:  now,
		createdAt:  now,
		members: map[string]*Member{
			userID: {UserID: userID, Name: name, Host: true, LastSeen: now.Unix()},
		},
	}
	m.rooms[r.id] = r
	return snapshot(r)
}

// Join adds a member and returns the room they are joining.
func (m *Manager) Join(id, userID, name string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	now := m.now()
	// Rejoining is not an error and does not duplicate anybody: a refresh, a
	// dropped connection and a second tab all arrive here.
	r.members[userID] = &Member{
		UserID: userID, Name: name, Host: userID == r.hostID, LastSeen: now.Unix(),
	}
	// Swept after the join, so an arriving guest is never counted among the
	// absent — and so a room whose host has gone is still reported as gone.
	m.sweepLocked()
	if _, alive := m.rooms[id]; !alive {
		return Session{}, ErrNotFound
	}
	return snapshot(r), nil
}

// Poll is what a follower calls. It records that the caller is still here and
// returns the room, which is the whole of the client's synchronisation input.
func (m *Manager) Poll(id, userID string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	mem, ok := r.members[userID]
	if !ok {
		return Session{}, ErrNotMember
	}
	/*
	 * Record the caller before sweeping, not after.
	 *
	 * The sweep drops anyone who has not been seen inside the timeout, and this
	 * call *is* being seen. Sweeping first meant a host polling at the boundary
	 * — which is precisely when the interval lands — was judged absent and took
	 * their own room down with them, mid-film, for being on time.
	 */
	mem.LastSeen = m.now().Unix()
	m.sweepLocked()

	// The sweep can still have closed the room around the caller: a guest whose
	// host went quiet is in a room that no longer has a driver.
	if _, alive := m.rooms[id]; !alive {
		return Session{}, ErrNotFound
	}
	return snapshot(r), nil
}

// Report is the host telling the room where it is. Only the host may.
func (m *Manager) Report(id, userID string, positionMS int64, paused bool) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	if r.hostID != userID {
		return Session{}, ErrNotHost
	}
	now := m.now()
	r.positionMS = positionMS
	r.paused = paused
	r.updatedAt = now
	// Same ordering rule as Poll: the host reporting is the host being seen.
	if mem, ok := r.members[userID]; ok {
		mem.LastSeen = now.Unix()
	}
	m.sweepLocked()
	return snapshot(r), nil
}

/*
 * Leave removes a member, and closes the room when the host goes.
 *
 * The host leaving ends it rather than promoting somebody. Promotion sounds
 * generous and is worse: the film keeps playing in three houses under a new
 * driver nobody chose, and the person who started it has no way to stop what
 * they began. Ending is honest and recoverable — anyone can open a new room in
 * one action.
 */
func (m *Manager) Leave(id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[id]
	if !ok {
		return ErrNotFound
	}
	if userID == r.hostID {
		delete(m.rooms, id)
		return nil
	}
	delete(r.members, userID)
	if len(r.members) == 0 {
		delete(m.rooms, id)
	}
	return nil
}

// List returns open rooms, so somebody arriving can find one to join without
// being sent a link. On a household server that is the ordinary case.
func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()

	out := make([]Session, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, snapshot(r))
	}
	// Newest first, and stable: a list that reorders itself between polls is a
	// list nobody can click.
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

/*
 * sweepLocked drops members who stopped polling, and rooms left empty.
 *
 * Nobody presses "leave" — they close the tab, or the laptop, or walk out of
 * range. Without this the member list is a record of everyone who was ever
 * here, which is exactly the wrong answer to "who is watching this with me".
 *
 * Called from every entry point rather than from a ticker: a background
 * goroutine for a map that is only interesting while somebody is looking at it
 * is a goroutine that runs all night on an idle server.
 */
func (m *Manager) sweepLocked() {
	cutoff := m.now().Add(-idleTimeout).Unix()
	for id, r := range m.rooms {
		for uid, mem := range r.members {
			if mem.LastSeen < cutoff {
				delete(r.members, uid)
			}
		}
		// A room whose host has gone quiet is over, for the same reason the
		// host leaving ends it.
		if _, ok := r.members[r.hostID]; !ok || len(r.members) == 0 {
			delete(m.rooms, id)
		}
	}
}

func snapshot(r *room) Session {
	members := make([]Member, 0, len(r.members))
	for _, mem := range r.members {
		members = append(members, *mem)
	}
	// Host first, then by name: a member list in map order changes on every
	// request and reads as flicker.
	sort.Slice(members, func(i, j int) bool {
		if members[i].Host != members[j].Host {
			return members[i].Host
		}
		return members[i].Name < members[j].Name
	})
	return Session{
		ID:         r.id,
		ItemID:     r.itemID,
		HostID:     r.hostID,
		PositionMS: r.positionMS,
		Paused:     r.paused,
		UpdatedAt:  r.updatedAt.Unix(),
		Members:    members,
		CreatedAt:  r.createdAt.Unix(),
	}
}

// newID is short enough to read aloud across a room and random enough not to be
// guessed by somebody idly trying. It is not a secret — joining also requires a
// session on this server — but a guessable id would let one account drop into
// another's room uninvited.
func newID() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		// Cannot happen on any supported platform; a time-based fallback beats
		// returning an empty id that would collide with the next one.
		return strings.ToLower(base32.StdEncoding.EncodeToString(
			[]byte(time.Now().Format("150405.000000"))))
	}
	return strings.ToLower(strings.TrimRight(
		base32.StdEncoding.EncodeToString(b), "="))
}
