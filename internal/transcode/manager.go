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
		root:        dir,
		log:         log,
		MaxSessions: 3,
		IdleTimeout: 10 * time.Minute,
		sessions:    map[string]*Session{},
		stopped:     make(chan struct{}),
		available:   []Encoder{Software},
		selected:    Software,
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
		if s.Idle() > m.IdleTimeout {
			dead = append(dead, s)
			delete(m.sessions, id)
			continue
		}
		_ = finished
	}
	m.mu.Unlock()

	for _, s := range dead {
		m.log.Debug("reaping idle transcode", "session", s.ID, "item", s.ItemID)
		s.Stop()
		// A session can go idle *because* ffmpeg stopped producing. Whatever it
		// said on the way out is the explanation, and this is the other path a
		// session ends by.
		m.reportStderr(s)
	}
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

	m.log.Info("transcode started", "session", s.ID, "item", itemID,
		"output", "progressive", "video", o.Decision.VideoAction,
		"audio", o.Decision.AudioAction, "reason", o.Decision.Reason)

	return &sessionReader{ReadCloser: stdout, m: m, s: s}, nil
}

// EnsureHLS returns a session producing HLS for this item at this offset,
// starting one if needed.
func (m *Manager) EnsureHLS(ctx context.Context, itemID int64, o Options) (*Session, error) {
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
	s.ID, s.ItemID = id, itemID

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
 * Only progressive: HLS is segment-addressed and already reuses its session by
 * offset, so a second HLS request for the same item is the same player asking
 * for the next segment, not a replacement.
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
		m.log.Debug("superseding transcode", "session", s.ID, "item", itemID)
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
 * ffmpeg runs at `-loglevel error`, so anything here is worth a line — this does
 * not need a level or a filter to stay quiet on a healthy stream. A viewer who
 * simply closes the tab produces nothing, because being killed is not an error
 * ffmpeg reports.
 */
func (m *Manager) reportStderr(s *Session) {
	if m.log == nil {
		return
	}
	msg := strings.TrimSpace(s.Stderr())
	if msg == "" {
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
