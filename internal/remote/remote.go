/*
Package remote lets one of a person's windows play something on another of
theirs ([ADR 0117](../../docs/adr/0117-play-on-another-device.md)).

A *player* is a signed-in window that has offered itself. It reports what it is
playing every few seconds, and a player not heard from for PlayerTimeout is
gone. Another window of the same account can queue commands for it (play this,
pause, seek, stop), and the player collects them.

Everything is keyed by account first. A player is invisible to every other
account, and a command to somebody else's player fails exactly as a command to
a player that does not exist: there is no "play on someone else's screen".

In memory and per process, like `together`: a window that is not open is not a
player, so there is nothing to keep across a restart.
*/
package remote

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is a player that does not exist, has gone quiet, or belongs to
// another account. The three are deliberately one error.
var ErrNotFound = errors.New("no such player")

// ErrBadCommand is a command the player could not act on.
var ErrBadCommand = errors.New("bad command")

const (
	// PlayerTimeout is how long a player survives without reporting. It
	// reports every 5 s, so this is three missed reports and a little more:
	// one slow answer is not a closed laptop.
	PlayerTimeout = 20 * time.Second
	// MaxPending is how many commands wait for one player. A player that has
	// stopped collecting cannot grow the server's memory without bound.
	MaxPending = 16
	// CommandTTL is how long a command waits before it is dropped unrun. A
	// film that starts a minute after somebody pressed Play, when they have
	// given up and walked away, is worse than one that never starts.
	CommandTTL = 30 * time.Second
	// MaxQueue bounds a play command's queue. A randomized music library is
	// several thousand ids; this is far above that and far below a body that
	// costs anything to hold.
	MaxQueue = 20000
	// maxName bounds a player's name, in runes.
	maxName = 60
)

// The actions a command can carry.
const (
	ActionPlay     = "play"
	ActionPause    = "pause"
	ActionResume   = "resume"
	ActionSeek     = "seek"
	ActionStop     = "stop"
	ActionNext     = "next"
	ActionPrevious = "previous"
	ActionVolume   = "volume"
)

// The kinds of window a player can be. Only a hint for the picker's icon.
const (
	KindDesktop = "desktop"
	KindBrowser = "browser"
	KindPhone   = "phone"
)

// Command is one instruction for a player.
type Command struct {
	Action string `json:"action"`
	// ItemID is what to play. Play only.
	ItemID int64 `json:"item_id,omitempty"`
	// Queue is what plays after it, the item included. Play only, optional.
	Queue []int64 `json:"queue,omitempty"`
	/*
	 * PositionMS is where to start (play) or where to go (seek). For play it
	 * is optional: absent means wherever Play would start the item on that
	 * device, which is how an item page's "Play on…" behaves. A pointer so
	 * that "start at zero" and "no opinion" are different.
	 */
	PositionMS *int64 `json:"position_ms,omitempty"`
	// FromStart asks a play to ignore any resume point. Play only.
	FromStart bool `json:"from_start,omitempty"`
	// Volume is 0 to 1. Volume only.
	Volume *float64 `json:"volume,omitempty"`
	// From is the sending player's name, if it is one, for the note the
	// receiving window shows. Set by the server, never trusted from the body.
	From string `json:"from,omitempty"`
	// SentAt is when the server queued it, in Unix milliseconds.
	SentAt int64 `json:"sent_at"`
}

// Validate checks that c makes sense for its action.
func (c Command) Validate() error {
	switch c.Action {
	case ActionPlay:
		if c.ItemID <= 0 {
			return errors.Join(ErrBadCommand, errors.New("play needs an item_id"))
		}
		if len(c.Queue) > MaxQueue {
			return errors.Join(ErrBadCommand, errors.New("queue is too long"))
		}
		if c.PositionMS != nil && *c.PositionMS < 0 {
			return errors.Join(ErrBadCommand, errors.New("position_ms cannot be negative"))
		}
	case ActionSeek:
		if c.PositionMS == nil || *c.PositionMS < 0 {
			return errors.Join(ErrBadCommand, errors.New("seek needs a position_ms"))
		}
	case ActionVolume:
		if c.Volume == nil || *c.Volume < 0 || *c.Volume > 1 {
			return errors.Join(ErrBadCommand, errors.New("volume needs a value from 0 to 1"))
		}
	case ActionPause, ActionResume, ActionStop, ActionNext, ActionPrevious:
	default:
		return errors.Join(ErrBadCommand, errors.New("unknown action"))
	}
	return nil
}

// Report is what a player says about itself.
type Report struct {
	Name       string
	Kind       string
	ItemID     int64
	PositionMS int64
	DurationMS int64
	Paused     bool
	Volume     float64
}

// Player is one window offering itself, as its owner's other windows see it.
type Player struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// ItemID is what it is playing, 0 for nothing.
	ItemID     int64   `json:"item_id"`
	PositionMS int64   `json:"position_ms"`
	DurationMS int64   `json:"duration_ms"`
	Paused     bool    `json:"paused"`
	Volume     float64 `json:"volume"`
	/*
	 * AgeMS is how long ago it reported, measured by this server as it
	 * answered. A controller extrapolates a playing position from it with its
	 * own clock, so no two machines' clocks are ever compared; the same reason
	 * a Watch Together room carries one.
	 */
	AgeMS int64 `json:"age_ms"`
	// Pending is how many commands wait for it.
	Pending int `json:"pending"`
}

type player struct {
	Player
	seen     time.Time
	commands []Command
}

type key struct{ user, id string }

// Manager holds every player.
type Manager struct {
	mu      sync.Mutex
	players map[key]*player
	now     func() time.Time

	// OnGone, when set, hears which account lost a player to the sweep, so
	// that account's windows can be told. Called with the lock released.
	OnGone func(user string)
}

// New makes an empty manager.
func New() *Manager {
	return &Manager{players: map[key]*player{}, now: time.Now}
}

/*
Report records what a player says about itself, registering it if it is new.

changed is true when somebody looking at the player list would see a
difference: a new player, or a new name, item or play state. Position alone is
not a change; a controller extrapolates it (ADR 0117 §4).
*/
func (m *Manager) Report(user, id string, r Report) (p Player, changed bool) {
	r.Name = cleanName(r.Name)
	switch r.Kind {
	case KindDesktop, KindBrowser, KindPhone:
	default:
		r.Kind = KindBrowser
	}
	if r.ItemID < 0 {
		r.ItemID = 0
	}
	if r.Volume < 0 {
		r.Volume = 0
	}
	if r.Volume > 1 {
		r.Volume = 1
	}

	m.mu.Lock()
	now := m.now()
	k := key{user, id}
	pl, ok := m.players[k]
	if !ok {
		pl = &player{Player: Player{ID: id}}
		m.players[k] = pl
		changed = true
	} else if pl.Name != r.Name || pl.Kind != r.Kind || pl.ItemID != r.ItemID || pl.Paused != r.Paused {
		changed = true
	}
	pl.Name, pl.Kind = r.Name, r.Kind
	pl.ItemID, pl.PositionMS, pl.DurationMS = r.ItemID, r.PositionMS, r.DurationMS
	pl.Paused, pl.Volume = r.Paused, r.Volume
	pl.seen = now
	pl.dropStale(now)
	p = pl.view(now)
	gone := m.sweepLocked(now)
	m.mu.Unlock()

	m.announce(gone)
	return p, changed
}

// List is every live player belonging to user, by name.
func (m *Manager) List(user string) []Player {
	m.mu.Lock()
	now := m.now()
	gone := m.sweepLocked(now)
	out := []Player{}
	for k, pl := range m.players {
		if k.user == user {
			pl.dropStale(now)
			out = append(out, pl.view(now))
		}
	}
	m.mu.Unlock()
	m.announce(gone)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Name is a live player's name, or "" when user has no such player.
func (m *Manager) Name(user, id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	pl, ok := m.players[key{user, id}]
	if !ok || m.now().Sub(pl.seen) > PlayerTimeout {
		return ""
	}
	return pl.Name
}

/*
Send queues c for one of user's players.

When the player already holds MaxPending commands the oldest is dropped: the
newest is what the person pressing the button wants now.
*/
func (m *Manager) Send(user, id string, c Command) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	now := m.now()
	gone := m.sweepLocked(now)
	pl, ok := m.players[key{user, id}]
	if ok {
		c.SentAt = now.UnixMilli()
		pl.dropStale(now)
		pl.commands = append(pl.commands, c)
		if n := len(pl.commands); n > MaxPending {
			pl.commands = append([]Command(nil), pl.commands[n-MaxPending:]...)
		}
	}
	m.mu.Unlock()
	m.announce(gone)
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Take hands over every command waiting for one of user's players, oldest
// first, and clears them. Collecting counts as being seen.
func (m *Manager) Take(user, id string) ([]Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	pl, ok := m.players[key{user, id}]
	if !ok || now.Sub(pl.seen) > PlayerTimeout {
		return nil, ErrNotFound
	}
	pl.seen = now
	pl.dropStale(now)
	out := pl.commands
	pl.commands = nil
	if out == nil {
		out = []Command{}
	}
	return out, nil
}

// Remove takes one of user's players away at once, as a window does when it
// stops offering itself. Reports whether there was one.
func (m *Manager) Remove(user, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key{user, id}
	_, ok := m.players[k]
	delete(m.players, k)
	return ok
}

func (pl *player) view(now time.Time) Player {
	p := pl.Player
	p.AgeMS = now.Sub(pl.seen).Milliseconds()
	p.Pending = len(pl.commands)
	return p
}

// dropStale removes commands older than CommandTTL.
func (pl *player) dropStale(now time.Time) {
	cut := now.Add(-CommandTTL).UnixMilli()
	keep := pl.commands[:0]
	for _, c := range pl.commands {
		if c.SentAt >= cut {
			keep = append(keep, c)
		}
	}
	pl.commands = keep
	if len(pl.commands) == 0 {
		pl.commands = nil
	}
}

// sweepLocked drops players that stopped reporting and returns whose they were.
func (m *Manager) sweepLocked(now time.Time) []string {
	var gone []string
	for k, pl := range m.players {
		if now.Sub(pl.seen) > PlayerTimeout {
			delete(m.players, k)
			gone = append(gone, k.user)
		}
	}
	return gone
}

func (m *Manager) announce(users []string) {
	if m.OnGone == nil {
		return
	}
	seen := map[string]bool{}
	for _, u := range users {
		if !seen[u] {
			seen[u] = true
			m.OnGone(u)
		}
	}
}

// cleanName trims a player's name to something a list can show.
func cleanName(s string) string {
	r := []rune{}
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			continue
		}
		r = append(r, c)
	}
	// Trim spaces at either end.
	for len(r) > 0 && r[0] == ' ' {
		r = r[1:]
	}
	for len(r) > 0 && r[len(r)-1] == ' ' {
		r = r[:len(r)-1]
	}
	if len(r) > maxName {
		r = r[:maxName]
	}
	if len(r) == 0 {
		return "Unnamed device"
	}
	return string(r)
}
