package transcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNotInstalled is returned when ffmpeg cannot be found.
var ErrNotInstalled = errors.New("ffmpeg not found on PATH")

// ErrTooManySessions is returned when the concurrent transcode limit is hit.
var ErrTooManySessions = errors.New("too many concurrent transcodes")

// Manager owns transcode sessions and their scratch space.
type Manager struct {
	bin  string
	root string
	log  *slog.Logger

	// MaxSessions bounds concurrent transcodes. Each one is a full ffmpeg
	// process; without a ceiling, a handful of clients can bring a home server
	// to its knees and every stream stutters instead of one being refused.
	MaxSessions int
	/*
	 * IdleTimeout reaps sessions nobody is reading from. A closed browser tab
	 * does not tell the server it has gone.
	 *
	 * It cannot tell a closed tab from a paused film, though, and at 90 seconds
	 * it did not need to: pausing a progressive stream applies backpressure,
	 * the session stops being read, and a minute and a half later ffmpeg was
	 * killed underneath a viewer who was still sitting there. The client
	 * recovers from that now, but recovering is a reload, and a reload is not
	 * what pressing pause should cost.
	 *
	 * Ten minutes because a paused session is nearly free — ffmpeg is blocked
	 * writing into a pipe nobody is draining, so it holds a process and a
	 * session slot and burns no CPU. The slot is the real cost, and it is the
	 * one MaxSessions bounds.
	 */
	IdleTimeout time.Duration

	/*
	 * LiveIdleTimeout is the same idea for a channel, and it is much shorter
	 * (ADR 0065).
	 *
	 * IdleTimeout is a film's number: its comment below records being raised to
	 * ten minutes so a paused film keeps its ffmpeg, which is right, because
	 * waiting costs nothing and resuming is expensive.
	 *
	 * Every term of that differs for a channel. An abandoned one is pulled at
	 * full rate for the whole ten minutes from somebody else's server; nobody
	 * pauses five films in two minutes, where surfing five channels is one
	 * ordinary minute of use; and there is nothing to resume into, because
	 * coming back to live means joining at *now*.
	 *
	 * Observed costing exactly what that predicts: two minutes of channel
	 * surfing, and the server refusing to play anything because three session
	 * slots were held by channels already left.
	 *
	 * Thirty seconds is derived rather than picked. A viewer polls for a
	 * segment about every SegmentSeconds, so thirty is roughly five missed
	 * polls — not a slow network, but nobody there.
	 */
	LiveIdleTimeout time.Duration

	/*
	 * UnreadIdleTimeout is for an HLS session that has never handed over a
	 * single byte of picture.
	 *
	 * IdleTimeout's ten minutes buy one thing: a paused film keeps its ffmpeg,
	 * because waiting is nearly free and resuming is expensive. That bargain
	 * assumes somebody is *there* — it is a viewer's pause being protected.
	 * A session nobody ever attached to has no viewer to protect and no
	 * position to resume to, and it holds a session slot for ten minutes all
	 * the same.
	 *
	 * Which was observed costing exactly what that predicts. Skipping around an
	 * episode three times, over seven seconds, started five sessions: the client
	 * asks for a playlist, falls back to the progressive stream about six
	 * hundred milliseconds later, and abandons the HLS session where it stands.
	 * At the third seek the server refused to play anything at all —
	 * `running=3 max=3` — and the slots were not freed until ten minutes after
	 * the person had given up and gone away.
	 *
	 * Scoped to HLS on purpose. A progressive stream is one held request, so
	 * the transport itself reports the client leaving; an HLS session is a
	 * series of separate polls and has nothing that says "still here" except
	 * the polls, which is the same asymmetry ADR 0065 records for a channel.
	 *
	 * Thirty seconds is measured against what the path costs when it works: a
	 * playlist appears in about a second and the first segment immediately
	 * after. This is not a race with a slow encode, because any request for a
	 * segment touches the session and starts the clock again — it expires only
	 * where nothing has been asked for at all.
	 */
	UnreadIdleTimeout time.Duration

	// binMu guards bin, which is no longer written only at construction: the
	// media-tools installer can put ffmpeg on this machine while the server is
	// running, and requiring a restart to notice would make a working install
	// look like a failed one.
	binMu     sync.RWMutex
	encMu     sync.RWMutex
	available []Encoder
	selected  Encoder
	// colour is what this ffmpeg can do about HDR — a property of the build, not
	// of a job. Detected alongside the encoders and guarded by the same lock
	// because the same call refreshes both (ADR 0033).
	colour ColourCaps

	mu       sync.Mutex
	sessions map[string]*Session

	stopOnce sync.Once
	stopped  chan struct{}
}

// NewManager builds a manager rooted at dir for scratch space.
func NewManager(dir string, log *slog.Logger) *Manager {
	m := &Manager{
		root:              dir,
		log:               log,
		MaxSessions:       3,
		IdleTimeout:       10 * time.Minute,
		LiveIdleTimeout:   30 * time.Second,
		UnreadIdleTimeout: 30 * time.Second,
		sessions:          map[string]*Session{},
		stopped:           make(chan struct{}),
		available:         []Encoder{Software},
		selected:          Software,
	}
	m.Rescan()
	return m
}

/*
 * Rescan re-resolves ffmpeg, and reports whether it is now available.
 *
 * Called at construction and again after the media-tools installer finishes.
 * Prober does this on every call and so needs no equivalent; the transcode
 * manager resolves once and holds the path, because every session start would
 * otherwise pay for a PATH walk.
 */
func (m *Manager) Rescan() bool {
	found, err := exec.LookPath("ffmpeg")
	if err != nil {
		found = ""
	}
	m.binMu.Lock()
	m.bin = found
	m.binMu.Unlock()
	return found != ""
}

// binary returns the resolved ffmpeg path, empty when there is none.
func (m *Manager) binary() string {
	m.binMu.RLock()
	defer m.binMu.RUnlock()
	return m.bin
}

// DetectHardware probes for usable encoders and applies a preference.
//
// Called at startup and again when the setting changes. Detection runs a real
// test encode per candidate, so it costs a second or two — worth paying once
// rather than discovering at playback time that the encoder ffmpeg advertised
// does not work on this machine.
func (m *Manager) DetectHardware(ctx context.Context, preference string) {
	bin := m.binary()
	available := DetectEncoders(ctx, bin, m.log)
	selected := SelectEncoder(available, preference, m.log)
	colour := DetectColourCaps(ctx, bin, m.log)

	m.encMu.Lock()
	m.available, m.selected, m.colour = available, selected, colour
	m.encMu.Unlock()

	m.log.Info("video encoder selected", "encoder", selected.Name,
		"hardware", selected.Hardware, "preference", preference)
}

// Encoder returns the encoder in use.
func (m *Manager) Encoder() Encoder {
	m.encMu.RLock()
	defer m.encMu.RUnlock()
	return m.selected
}

// colourFor unpacks the colour capabilities into the two Options fields, so the
// call site reads as what it sets rather than as a struct copy.
func (m *Manager) colourFor() (tonemap, tagSDR bool) {
	c := m.ColourCaps()
	return c.Tonemap, c.TagSDR
}

// ColourCaps reports what this ffmpeg build can do about HDR (ADR 0033).
func (m *Manager) ColourCaps() ColourCaps {
	m.encMu.RLock()
	defer m.encMu.RUnlock()
	return m.colour
}

// AvailableEncoders returns every verified encoder, best first.
func (m *Manager) AvailableEncoders() []Encoder {
	m.encMu.RLock()
	defer m.encMu.RUnlock()
	out := make([]Encoder, len(m.available))
	copy(out, m.available)
	return out
}

// Available reports whether transcoding is possible.
func (m *Manager) Available() bool { return m.binary() != "" }

// Start begins reaping idle sessions. Cancelling ctx stops everything.
func (m *Manager) Start(ctx context.Context) {
	// Leftover scratch from a previous run is dead: the sessions that owned
	// those directories died with the process.
	if err := os.RemoveAll(m.root); err != nil && !os.IsNotExist(err) {
		m.log.Warn("could not clear transcode scratch", "dir", m.root, "error", err)
	}

	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				m.StopAll()
				return
			case <-m.stopped:
				return
			case <-t.C:
				m.reap()
			}
		}
	}()
}

// StopAll kills every session and clears scratch space.
func (m *Manager) StopAll() {
	m.stopOnce.Do(func() { close(m.stopped) })

	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()

	for _, s := range sessions {
		s.Stop()
	}
	_ = os.RemoveAll(m.root)
}

// reap stops sessions nobody has read from recently.
func (m *Manager) reap() {
	m.mu.Lock()
	var dead []*Session
	for id, s := range m.sessions {
		finished, _ := s.Done()
		// A finished session still serves its segments — the file is fully
		// transcoded and seeking through it is exactly what a user does next.
		// Only idleness reaps.
		if s.Idle() > m.idleLimit(s) {
			dead = append(dead, s)
			delete(m.sessions, id)
			continue
		}
		_ = finished
	}
	m.mu.Unlock()

	for _, s := range dead {
		/*
		 * Info, and carrying what the session did with its life.
		 *
		 * This is the third birth-at-Info, death-at-Debug pair found in this
		 * file, and they keep costing the same thing: debug logging is off on a
		 * normal server, so a run of sessions on one item records every start
		 * and no ending, and the question "why did this stream stop" has no
		 * answer anywhere.
		 *
		 * It was reached while trying to explain a queue that did not advance
		 * after a long pause. A thirteen-minute pause against a ten-minute
		 * timeout produced no reap and no new session, which either means the
		 * reaper did not take it or something kept touching it — and neither
		 * could be told from the log, because the only line that distinguishes
		 * them is this one.
		 *
		 * `idle_seconds` rather than a bare notice: the timeout is
		 * configurable and a session reaped at 601 seconds and one reaped at
		 * 4,000 are different stories about what the client was doing.
		 */
		/*
		 * A channel is logged as a channel.
		 *
		 * ItemID holds a negated channel id for a live session, so this line
		 * used to report `item=-30598` — a number that matches nothing in the
		 * library and reads as corruption to anybody grepping for an item.
		 */
		if s.IsLive() {
			m.log.Info("reaping idle live channel", "session", s.ID,
				"channel", -s.ItemID, "idle_seconds", int(s.Idle().Seconds()),
				"served_bytes", s.Served())
		} else {
			m.log.Info("reaping idle transcode", "session", s.ID, "item", s.ItemID,
				"idle_seconds", int(s.Idle().Seconds()),
				"served_bytes", s.Served())
		}
		s.Stop()
		// A session can go idle *because* ffmpeg stopped producing. Whatever it
		// said on the way out is the explanation, and this is the other path a
		// session ends by.
		m.reportStderr(s)
	}
}

/*
 * idleLimit is how long this session may go unread before it is reaped.
 *
 * Two numbers rather than one, because they answer different questions — see
 * LiveIdleTimeout. A zero LiveIdleTimeout falls back to the film's, so a
 * Manager built without setting it behaves exactly as it did before this
 * existed rather than reaping everything in no time.
 */
func (m *Manager) idleLimit(s *Session) time.Duration {
	if s.IsLive() && m.LiveIdleTimeout > 0 {
		return m.LiveIdleTimeout
	}
	/*
	 * An HLS session that has never served a byte of picture is not somebody's
	 * paused film, so it does not get the allowance that exists for one.
	 *
	 * The served count is what makes this safe, and it only became true bytes
	 * when the segment route started reporting them: before that every HLS
	 * session read as zero, working or not, and this test would have reaped a
	 * film somebody was watching. A paused film has served megabytes and keeps
	 * the full ten minutes.
	 */
	/*
	 * Live is excluded explicitly rather than by luck. A channel is served
	 * through the HLS path and its bytes are not counted either, so without
	 * this a channel would match — and a Manager with LiveIdleTimeout unset,
	 * which is documented to behave as it did before that field existed, would
	 * silently get this thirty seconds instead of the film's ten minutes.
	 */
	if s.Output == HLS && !s.IsLive() && m.UnreadIdleTimeout > 0 && s.Served() == 0 {
		/*
		 * The shorter of the two, never simply the unread one.
		 *
		 * This rule exists to make an abandoned session die sooner; a session
		 * nobody ever watched outliving one somebody paused is the rule
		 * working backwards. Returning UnreadIdleTimeout flatly does exactly
		 * that whenever IdleTimeout is the smaller number, which is not only a
		 * test's configuration — a server tuned down to reap aggressively would
		 * have found its abandoned sessions the most durable thing on it.
		 */
		if m.UnreadIdleTimeout < m.IdleTimeout {
			return m.UnreadIdleTimeout
		}
	}
	return m.IdleTimeout
}

/*
 * StopLive ends the session for a channel, if one is running.
 *
 * The exact signal the HLS path otherwise lacks (ADR 0065). A poll of a
 * playlist is not a lifetime, so the request context cannot end the encode —
 * which leaves the client's own knowledge that it has stopped watching as the
 * only precise information available, and until now it was thrown away.
 *
 * Reports whether it stopped anything so the caller can log it, but stopping a
 * channel that is not running is success: the caller asked for it to not be
 * running, and it is not.
 */
func (m *Manager) StopLive(channelID int64) bool {
	key := -channelID

	m.mu.Lock()
	var found *Session
	for id, s := range m.sessions {
		if s.ItemID == key && s.Output == HLS {
			found = s
			delete(m.sessions, id)
			break
		}
	}
	m.mu.Unlock()

	if found == nil {
		return false
	}
	m.log.Info("stopping live channel: the viewer left", "session", found.ID,
		"channel", channelID, "served_bytes", found.Served())
	found.Stop()
	return true
}

// Sessions returns a snapshot for diagnostics.
func (m *Manager) Sessions() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		done, err := s.Done()
		info := SessionInfo{
			ID: s.ID, ItemID: s.ItemID, Output: string(s.Output),
			Encoding: s.Encoding,
			StartAt:  s.StartAt, IdleSeconds: int(s.Idle().Seconds()),
			RunningSeconds: int(time.Since(s.Started()).Seconds()), Finished: done,
		}
		if err != nil {
			info.Error = err.Error()
		}
		out = append(out, info)
	}
	return out
}

// SessionInfo is a serializable view of a session.
type SessionInfo struct {
	ID     string `json:"id"`
	ItemID int64  `json:"item_id"`
	Output string `json:"output"`
	// Encoding distinguishes a real re-encode from a remux. See Session.
	Encoding       bool    `json:"encoding"`
	StartAt        float64 `json:"start_at"`
	IdleSeconds    int     `json:"idle_seconds"`
	RunningSeconds int     `json:"running_seconds"`
	Finished       bool    `json:"finished"`
	Error          string  `json:"error,omitempty"`
}

/*
 * Progressive starts a transcode streaming fragmented MP4 to the returned
 * reader. The caller must close it, which stops ffmpeg.
 *
 * Superseding: a backstop, not a hot path.
 *
 * EnsureHLS reuses a session for the same item and offset; this has no
 * equivalent, so every request starts a fresh ffmpeg.
 *
 * On the ordinary seek path that costs nothing, and it is worth being exact
 * about why, because the obvious reading is wrong. Seeking a transcode
 * re-requests the stream, but the client aborts the request it is replacing,
 * the response body closes, and sessionReader.Close stops ffmpeg immediately.
 * Measured in the running app: six sessions started across four seeks, one
 * ffmpeg process alive at the end, and this function found nothing to stop on
 * any of them.
 *
 * What it covers is the case where that teardown does not happen — two requests
 * for the same item arriving together, neither yet aborted. That is not
 * hypothetical: the server log has two starts on one item six milliseconds
 * apart, which no sequence of seeks can produce. Both would then be live
 * against a MaxSessions of 3, and duplicates of the film being watched are the
 * worst possible thing to spend the ceiling on.
 *
 * So this makes "one progressive stream per viewer per item" a guarantee rather
 * than something the client's abort behaviour happens to provide. Keyed on
 * (owner, item) and not on item alone, because two people watching the same
 * film at once is a thing a media server must do — superseding by item would
 * have them killing each other's playback. One account re-requesting one film
 * is a player replacing its own stream, which is the case worth collapsing.
 */
func (m *Manager) Progressive(ctx context.Context, itemID int64, owner string, o Options) (io.ReadCloser, error) {
	if !m.Available() {
		return nil, ErrNotInstalled
	}

	// Before reserve, so replacing a stream cannot fail on a ceiling that the
	// stream being replaced is what filled.
	m.supersede(owner, itemID)

	if err := m.reserve(); err != nil {
		return nil, err
	}

	o.Output = Progressive
	o.Encoder = m.Encoder()
	o.CanTonemap, o.CanTagSDR = m.colourFor()
	s, stdout, err := startProgressive(ctx, m.binary(), o)
	if err != nil {
		m.release()
		return nil, err
	}
	s.ID, s.ItemID, s.Owner = newID(), itemID, owner

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()

	/*
	 * `start_at` matters more here than on the HLS line that already carries it,
	 * because this path starts a fresh ffmpeg per request and nothing else
	 * records where each one began.
	 *
	 * Without it, a run of sessions on one item is unreadable: a client
	 * reconnecting at the position it had reached and a client re-requesting
	 * the same offset over and over produce identical lines, and those are
	 * different faults with different fixes. An evening was spent narrowing
	 * that by elimination — polling the log and screenshotting the player to
	 * prove the timecode had not reset — which this field answers outright.
	 */
	m.log.Info("transcode started", "session", s.ID, "item", itemID,
		"output", "progressive", "start_at", o.StartAt,
		"video", o.Decision.VideoAction,
		"audio", o.Decision.AudioAction, "reason", o.Decision.Reason)

	return &sessionReader{ReadCloser: stdout, m: m, s: s}, nil
}

// EnsureHLS returns a session producing HLS for this item at this offset,
// starting one if needed.
func (m *Manager) EnsureHLS(ctx context.Context, itemID int64, owner string, o Options) (*Session, error) {
	if !m.Available() {
		return nil, ErrNotInstalled
	}

	// Reuse an existing session for the same item and offset. A player
	// requesting segment after segment must not spawn an ffmpeg per request.
	m.mu.Lock()
	for _, s := range m.sessions {
		if s.ItemID == itemID && s.Output == HLS && sameOffset(s.StartAt, o.StartAt) {
			s.Touch()
			m.mu.Unlock()
			return s, nil
		}
	}
	m.mu.Unlock()

	/*
	 * Anything of this viewer's still running for this item is a position they
	 * have left, and it is stopped here rather than waited out.
	 *
	 * The reuse loop above has already returned for the offset being asked for,
	 * so every session reaching this line is at a *different* one — which is
	 * what a seek is. Nothing will ever request its segments again.
	 *
	 * Before reserve, so a seek cannot be refused on a ceiling that the
	 * position being left is what filled. That is not a theoretical ordering:
	 * skipping around an episode three times over seven seconds started five
	 * sessions and the third seek was refused outright, `running=3 max=3`, with
	 * the server then unable to play anything at all.
	 */
	m.supersedeHLS(owner, itemID)

	if err := m.reserve(); err != nil {
		return nil, err
	}

	id := newID()
	o.Output = HLS
	o.Encoder = m.Encoder()
	o.CanTonemap, o.CanTagSDR = m.colourFor()
	o.OutputDir = filepath.Join(m.root, id)

	s, err := startHLS(ctx, m.binary(), o)
	if err != nil {
		m.release()
		return nil, err
	}
	s.ID, s.ItemID, s.Owner = id, itemID, owner

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	m.log.Info("transcode started", "session", id, "item", itemID,
		"output", "hls", "start_at", o.StartAt,
		"video", o.Decision.VideoAction, "audio", o.Decision.AudioAction,
		"reason", o.Decision.Reason)

	return s, nil
}

// WaitForFile blocks until a session produces the named file, or ctx expires.
//
// ffmpeg writes the playlist and the first segment a moment after starting, so
// a request that arrives immediately would otherwise 404 on a session that is
// working perfectly well.
func (m *Manager) WaitForFile(ctx context.Context, s *Session, name string, timeout time.Duration) (string, error) {
	path := filepath.Join(s.Dir, name)
	deadline := time.Now().Add(timeout)

	for {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return path, nil
		}
		if done, ffErr := s.Done(); done {
			// ffmpeg exited without producing it: the file is never coming.
			if ffErr != nil {
				return "", ffErr
			}
			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("transcode finished without producing %s: %s", name, s.Stderr())
			}
			return path, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for %s", name)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

/*
 * supersede stops this owner's existing progressive session for this item.
 *
 * Only progressive; supersedeHLS is the other half. This used to say that HLS
 * needed no equivalent, "because a second HLS request for the same item is the
 * same player asking for the next segment, not a replacement". That is true of
 * the next segment and false of a seek, and the difference is the offset — a
 * request at the same offset never reaches either function, because EnsureHLS
 * has already reused the session for it.
 *
 * An owner of "" is not collapsed. That is the unconfigured loopback state
 * where every request is anonymous, and treating those as one player would let
 * a second viewer end the first one's film.
 */
func (m *Manager) supersede(owner string, itemID int64) {
	if owner == "" {
		return
	}
	m.mu.Lock()
	var dead []*Session
	for id, s := range m.sessions {
		if s.Output == Progressive && s.ItemID == itemID && s.Owner == owner {
			dead = append(dead, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range dead {
		/*
		 * Info, not Debug, and it carries what the session did with its life.
		 *
		 * The log records births at Info and recorded this death at Debug, so
		 * a run of sessions on one item showed every start and no ending —
		 * which is the exact unreadability the `start_at` field was added to
		 * fix, left half-fixed. Debug logging is off on a normal server, so
		 * the one line that explains a doubled start was the one line nobody
		 * ever had.
		 *
		 * `served` is the question. A stream superseded after zero bytes and a
		 * few milliseconds was never really watched — that is a media stack
		 * opening the source twice, and no amount of client effect-wrangling
		 * will change it. A stream superseded after real bytes is a player
		 * that asked again, which is a client fault and fixable there.
		 */
		m.log.Info("superseding transcode", "session", s.ID, "item", itemID,
			"age_ms", time.Since(s.Started()).Milliseconds(),
			"served_bytes", s.Served())
		s.Stop()
	}
}

/*
 * supersedeHLS stops this owner's HLS sessions for this item at other offsets.
 *
 * Every one of them is a position this viewer has moved away from. EnsureHLS
 * reuses the session for the offset being requested before this is called, so
 * what is left is only ever somewhere they used to be, and no client will ask
 * for those segments again.
 *
 * This is the half of the leak the reaper cannot reach in time. An abandoned
 * HLS session is now reaped in thirty seconds rather than ten minutes, but
 * three seeks take about seven seconds — so waiting was still enough to fill a
 * ceiling of three and refuse the film outright.
 *
 * Keyed on (owner, item) for the same reason superseding a progressive stream
 * is: two people watching one thing at once is a thing a media server must do,
 * and collapsing by item alone would have them ending each other's playback.
 * An owner of "" is the unconfigured loopback state where every request is
 * anonymous, and is not collapsed for exactly that reason.
 *
 * A live channel cannot match: its ItemID is a negated channel id, so it never
 * equals a library item's.
 */
func (m *Manager) supersedeHLS(owner string, itemID int64) {
	if owner == "" {
		return
	}
	m.mu.Lock()
	var dead []*Session
	for id, s := range m.sessions {
		if s.Output == HLS && s.ItemID == itemID && s.Owner == owner {
			dead = append(dead, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range dead {
		// Info and carrying the same two figures as its progressive sibling:
		// a run of sessions on one item is unreadable without an ending for
		// each start, and `served_bytes` says whether the position being left
		// was ever actually watched.
		m.log.Info("superseding hls transcode", "session", s.ID, "item", itemID,
			"start_at", s.StartAt, "age_ms", time.Since(s.Started()).Milliseconds(),
			"served_bytes", s.Served())
		s.Stop()
	}
}

/*
 * EvictionGrace is how long a session must have gone unread before a new
 * viewer may take its slot.
 *
 * It is the old IdleTimeout, and that is not a coincidence: 90 seconds was
 * already the project's answer to "long enough that nothing still playing could
 * look idle", and nothing about that judgement changed when the timeout grew.
 */
const EvictionGrace = 90 * time.Second

/*
 * reserve takes a session slot, evicting one that is merely being held.
 *
 * Raising IdleTimeout to ten minutes so a paused film keeps its ffmpeg had a
 * cost that refusing outright made worse: three paused films hold every slot,
 * and a fourth viewer was told "too many transcodes are already running; try
 * again shortly" where shortly had quietly become ten minutes. Before the
 * timeout grew that resolved itself in ninety seconds. The message was true
 * and had stopped being.
 *
 * So a slot that is only being *held* is yielded to somebody who wants to
 * *use* it. The longest-idle session goes, and only if it has been unread for
 * EvictionGrace — a session still feeding a player is never taken for a new
 * one, which is the whole distinction that matters.
 *
 * This is only safe because a cut progressive stream is now recoverable: the
 * client detects the truncation, re-requests from where it stopped, and the
 * viewer sees a reload rather than a film that ends early. Evicting a paused
 * session before that would have skipped to the next title, which is the bug
 * the recovery was written for.
 *
 * Stop() blocks for up to three seconds, so the victim is removed under the
 * lock and stopped outside it — the same order reap() uses, and for the same
 * reason.
 */
func (m *Manager) reserve() error {
	m.mu.Lock()
	if len(m.sessions) < m.MaxSessions {
		m.mu.Unlock()
		return nil
	}

	var victim *Session
	for _, s := range m.sessions {
		if s.Idle() < EvictionGrace {
			continue
		}
		if victim == nil || s.Idle() > victim.Idle() {
			victim = s
		}
	}
	if victim == nil {
		// Every slot is feeding a player. This is the case the ceiling exists
		// for, and refusing is the right answer to it.
		m.mu.Unlock()
		return ErrTooManySessions
	}
	delete(m.sessions, victim.ID)
	m.mu.Unlock()

	m.log.Info("evicting an idle transcode to free a slot",
		"session", victim.ID, "item", victim.ItemID,
		"idle_seconds", int(victim.Idle().Seconds()))
	victim.Stop()
	return nil
}

func (m *Manager) release() {}

// Session looks up a running session by id, or nil.
func (m *Manager) Session(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Stop ends one session by id.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		s.Stop()
		m.reportStderr(s)
	}
}

/*
 * reportStderr logs what ffmpeg complained about, once, when a session ends.
 *
 * It was already captured — every session gives ffmpeg a bounded ring buffer for
 * stderr — and then nothing ever read it for a stream. So a channel that failed
 * left "live transcode started" in the log, no other line, and the reason sitting
 * in memory until the process exited.
 *
 * That is not a small gap on the live path. A browser reports a failed channel as
 * `DEMUXER_ERROR_COULD_NOT_OPEN`, which says only that what arrived was not
 * openable; ffmpeg knows whether the source refused the connection, sent a codec
 * the mux rejected, or died three seconds in, and it had already written that
 * down. Diagnosing live playback without it is inference over a silent log.
 *
 * ffmpeg runs at `-loglevel error`, so anything here is worth a line.
 *
 * # Why the level depends on how the session ended
 *
 * This used to say that "a viewer who simply closes the tab produces nothing,
 * because being killed is not an error ffmpeg reports". That was reasoning
 * presented as behaviour, and watching it disproved it: killing ffmpeg mid-
 * stream reliably produces `Error submitting a packet to the muxer: Broken
 * pipe`, and on the live fMP4 path a muxer error with a `PATCHWELCOME` return
 * code. So **every ordinary channel stop logged `WARN "ffmpeg reported
 * errors"`** with a stack of alarming text under it.
 *
 * That is not merely untidy. During a real investigation into a frozen channel
 * it sent two separate diagnoses down the wrong path — first "the probe misread
 * the codec", then "ffmpeg died mid-stream" — because a warning that fires on
 * every success is indistinguishable from one that fires on a failure. The
 * actual fault was a muxer flag corrupting timestamps, and this line was the
 * loudest thing in the log pointing away from it.
 *
 * A log that cries wolf on every stop costs more than it saves, and this one
 * had been measured doing exactly that.
 *
 * So: ffmpeg ending **by itself** is a fact about the media and stays a
 * warning; ffmpeg being **killed** while it was still running is a fact about
 * us, and goes to debug. `EndedItself` is set from the reader seeing EOF, and
 * `Done`'s error covers the HLS path, which has no reader and does keep an exit
 * status. Neither is a guess about what a message means.
 */
func (m *Manager) reportStderr(s *Session) {
	if m.log == nil {
		return
	}
	msg := strings.TrimSpace(s.Stderr())
	if msg == "" {
		return
	}
	_, err := s.Done()
	if !s.EndedItself() && err == nil {
		m.log.Debug("ffmpeg output while shutting down", "session", s.ID, "item", s.ItemID,
			"output", s.Output, "stderr", msg)
		return
	}
	m.log.Warn("ffmpeg reported errors", "session", s.ID, "item", s.ItemID, "output", s.Output,
		"stderr", msg)
}

// sessionReader ties the lifetime of a progressive stream to its session, so
// closing the HTTP response kills ffmpeg. Without this, a client that closes
// the tab leaves a transcode running until the idle reaper notices.
type sessionReader struct {
	io.ReadCloser
	m    *Manager
	s    *Session
	once sync.Once
}

func (r *sessionReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.s.Touch()
		r.s.NoteServed(n)
	}
	// EOF here means ffmpeg's stdout closed, which means ffmpeg exited without
	// being asked to. It is the difference between a complaint worth a warning
	// and the noise a killed process makes on the way out — see reportStderr.
	if errors.Is(err, io.EOF) {
		r.s.NoteEnded()
	}
	return n, err
}

func (r *sessionReader) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(func() { r.m.Stop(r.s.ID) })
	return err
}

// Stderr exposes what ffmpeg complained about to the caller holding the stream.
// A handler that got no bytes needs to say *why* to the person watching, and the
// answer is in the session it cannot otherwise reach.
func (r *sessionReader) Stderr() string { return r.s.Stderr() }

// sameOffset treats nearby start points as the same session. A seek of under a
// segment lands inside what is already being produced, and restarting ffmpeg
// for it would be slower than letting the player buffer.
func sameOffset(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < SegmentSeconds
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("s%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
