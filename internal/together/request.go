package together

import (
	"errors"
	"sort"
	"time"
)

/*
 * Requests to join, from somebody on another server.
 *
 * ADR 0045 §7 gives a person who may see you the right to *ask* to join what
 * you are watching. It does not let them in. The host answers in the moment,
 * and this file holds the question while it is open.
 *
 * Three rules, each from the ADR or the Phase 5 plan, and each a test:
 *
 *   - **Silence is a no.** A request nobody answered within requestTimeout is
 *     declined. A host who is asleep has not agreed to anything.
 *   - **The asker hears only "not now".** Declined, timed out, replaced and
 *     cooling down all read the same from their side. A decline that explains
 *     itself invites a negotiation about why.
 *   - **No nagging.** One open request per asker and host, and after a no the
 *     same asker cannot ask the same host again for requestCooldown.
 *
 * In memory, like the rooms and for the same reason: a request that survives a
 * restart is a question about a film that stopped playing when the server did.
 *
 * Who may ask at all (a presence grant from the host) is the caller's check,
 * against the database, at the moment of asking. This package knows nothing
 * of grants or peers beyond the strings it is handed.
 */

const (
	// requestTimeout is how long a host has to answer. Long enough to find
	// the remote and read a name, short enough that the asker is not left
	// staring at "waiting" through a whole scene.
	requestTimeout = 60 * time.Second

	// requestCooldown is how long an asker waits, after a no, before the same
	// host can be asked again. Without it "not now" is a prompt that comes
	// back until the host gives in.
	requestCooldown = 2 * time.Minute

	// requestRetention is how long an answered request stays readable, so the
	// asker's server, polling every couple of seconds, learns the answer.
	requestRetention = 2 * time.Minute
)

var (
	// ErrRequestClosed is an answer to a request that is no longer open: it
	// was answered already, or it timed out.
	ErrRequestClosed = errors.New("that request is no longer open")
)

// Request states. What an asker is shown is narrower; see AskerState.
const (
	RequestPending  = "pending"
	RequestAccepted = "accepted"
	RequestDeclined = "declined"
)

// Request is one ask, as the host's side sees it.
type Request struct {
	ID     string `json:"id"`
	HostID string `json:"host_id"`
	// Peer and Person are who is asking, in the asking server's word. Name
	// and Server are frozen at the time of asking, like a member's name.
	Peer      string `json:"peer"`
	Person    string `json:"person"`
	Name      string `json:"name"`
	Server    string `json:"server"`
	State     string `json:"state"`
	RoomID    string `json:"room_id,omitempty"`
	CreatedAt int64  `json:"created_at"`
	// ExpiresAt is when an unanswered request becomes a no, so a prompt can
	// show the host how long is left rather than vanishing without warning.
	ExpiresAt int64 `json:"expires_at"`
}

// AskerState is all the asker's side is told: still waiting, in (and where),
// or not now. A decline, a timeout and a cooldown are deliberately the same.
type AskerState struct {
	State  string `json:"state"` // "pending", "accepted" or "not_now"
	RoomID string `json:"room_id,omitempty"`
}

type request struct {
	Request
	created  time.Time
	answered time.Time
}

func askKey(hostID, fingerprint, person string) string {
	return hostID + "\x00" + fingerprint + "\x00" + person
}

/*
 * Ask opens a request from a remote person to a host on this server.
 *
 * It always returns a request id, even when the answer is already no. An asker
 * in their cooldown gets a request that is born declined and that the host
 * never sees, so cooling down is indistinguishable from being turned down
 * just now. Anything else would tell the asker the host had said no earlier,
 * which is the explanation the rule exists to withhold.
 *
 * Asking again while a request is open replaces it rather than stacking a
 * second prompt in front of the host.
 */
func (m *Manager) Ask(hostID, fingerprint, person, name, server string) Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sweepRequestsLocked(now)

	key := askKey(hostID, fingerprint, person)
	r := &request{
		Request: Request{
			ID: newID(), HostID: hostID,
			Peer: fingerprint, Person: person, Name: name, Server: server,
			State:     RequestPending,
			CreatedAt: now.Unix(),
			ExpiresAt: now.Add(requestTimeout).Unix(),
		},
		created: now,
	}

	if until, ok := m.cooldown[key]; ok && now.Before(until) {
		r.State = RequestDeclined
		r.answered = now
		m.requests[r.ID] = r
		return r.Request
	}

	for id, old := range m.requests {
		if old.State == RequestPending && askKey(old.HostID, old.Peer, old.Person) == key {
			delete(m.requests, id)
		}
	}
	m.requests[r.ID] = r
	return r.Request
}

// Pending lists the open requests addressed to one host, oldest first, so a
// host with two prompts answers them in the order they arrived.
func (m *Manager) Pending(hostID string) []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepRequestsLocked(m.now())

	out := []Request{}
	for _, r := range m.requests {
		if r.HostID == hostID && r.State == RequestPending {
			out = append(out, r.Request)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

/*
 * Accept answers yes, and puts the asker in the host's room in the same step.
 *
 * The room must exist and the caller must host it: a host cannot admit
 * somebody into a room another person is driving. The client opens a room
 * first when the host was watching alone, which is the common case, and then
 * accepts into it.
 *
 * The remote member is added here and nowhere else. JoinRemote only lets an
 * already-admitted member back in, so nobody arrives in a room because it
 * happened to be open.
 */
func (m *Manager) Accept(requestID, hostID, roomID string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sweepRequestsLocked(now)

	r, err := m.openRequestLocked(requestID, hostID)
	if err != nil {
		return Session{}, err
	}
	room, ok := m.rooms[roomID]
	if !ok {
		return Session{}, ErrNotFound
	}
	if room.hostID != hostID {
		return Session{}, ErrNotHost
	}

	r.State = RequestAccepted
	r.RoomID = roomID
	r.answered = now
	return m.joinLocked(roomID, Member{
		UserID: RemoteID(r.Peer, r.Person),
		Name:   r.Name,
		Peer:   r.Peer,
		Server: r.Server,
	})
}

// Decline answers no. The asker cannot ask this host again for the cooldown.
func (m *Manager) Decline(requestID, hostID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sweepRequestsLocked(now)

	r, err := m.openRequestLocked(requestID, hostID)
	if err != nil {
		return err
	}
	m.declineLocked(r, now)
	return nil
}

/*
 * Withdraw takes back a request its asker no longer wants answered.
 *
 * Only the asker, and only while it is still open: a request belonging to
 * somebody else, or already answered, is left alone, and nothing is said
 * about which it was. Unlike a decline it starts no cooldown — changing your
 * mind is not being turned down, and the asker may ask again at once. The
 * request is removed, so it leaves the host's prompt on their next poll.
 */
func (m *Manager) Withdraw(requestID, fingerprint, person string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.requests[requestID]; ok &&
		r.Peer == fingerprint && r.Person == person && r.State == RequestPending {
		delete(m.requests, requestID)
	}
}

/*
 * Status is the asker's view of their own request.
 *
 * Only the asker may read it: a request id is not a secret, and without the
 * check one paired server could watch the answers another person was given.
 * A request that is gone (replaced, or answered long enough ago to have been
 * cleared) reads as not now, which is what it is.
 */
func (m *Manager) Status(requestID, fingerprint, person string) AskerState {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepRequestsLocked(m.now())

	r, ok := m.requests[requestID]
	if !ok || r.Peer != fingerprint || r.Person != person {
		return AskerState{State: "not_now"}
	}
	switch r.State {
	case RequestPending:
		return AskerState{State: RequestPending}
	case RequestAccepted:
		return AskerState{State: RequestAccepted, RoomID: r.RoomID}
	default:
		return AskerState{State: "not_now"}
	}
}

func (m *Manager) openRequestLocked(requestID, hostID string) (*request, error) {
	r, ok := m.requests[requestID]
	// Somebody else's request reads as missing, not as forbidden: a host has
	// no business learning who is asking whom.
	if !ok || r.HostID != hostID {
		return nil, ErrNotFound
	}
	if r.State != RequestPending {
		return nil, ErrRequestClosed
	}
	return r, nil
}

func (m *Manager) declineLocked(r *request, at time.Time) {
	r.State = RequestDeclined
	r.answered = at
	m.cooldown[askKey(r.HostID, r.Peer, r.Person)] = at.Add(requestCooldown)
}

/*
 * sweepRequestsLocked turns silence into a no and clears what nobody needs.
 *
 * A request is open for exactly requestTimeout: one answered at the boundary
 * is answered in time. The same off-by-one Poll records the caller first to
 * avoid, pointed the other way: a host who presses Accept as the countdown
 * reaches zero has said yes.
 *
 * A timeout starts the cooldown too. A host who did not answer is as entitled
 * not to be asked again at once as one who said no.
 */
func (m *Manager) sweepRequestsLocked(now time.Time) {
	for id, r := range m.requests {
		if r.State == RequestPending && now.Sub(r.created) > requestTimeout {
			m.declineLocked(r, r.created.Add(requestTimeout))
		}
		if r.State != RequestPending && now.Sub(r.answered) > requestRetention {
			delete(m.requests, id)
		}
	}
	for key, until := range m.cooldown {
		if !now.Before(until) {
			delete(m.cooldown, key)
		}
	}
}
