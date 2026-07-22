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
	// IdleTimeout reaps sessions nobody is reading from. A closed browser tab
	// does not tell the server it has gone.
	IdleTimeout time.Duration

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
		IdleTimeout: 90 * time.Second,
		sessions:    map[string]*Session{},
		stopped:     make(chan struct{}),
	}
	if bin, err := exec.LookPath("ffmpeg"); err == nil {
		m.bin = bin
	}
	return m
}

// Available reports whether transcoding is possible.
func (m *Manager) Available() bool { return m.bin != "" }

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
			StartAt: s.StartAt, IdleSeconds: int(s.Idle().Seconds()),
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
	ID             string  `json:"id"`
	ItemID         int64   `json:"item_id"`
	Output         string  `json:"output"`
	StartAt        float64 `json:"start_at"`
	IdleSeconds    int     `json:"idle_seconds"`
	RunningSeconds int     `json:"running_seconds"`
	Finished       bool    `json:"finished"`
	Error          string  `json:"error,omitempty"`
}

// Progressive starts a transcode streaming fragmented MP4 to the returned
// reader. The caller must close it, which stops ffmpeg.
func (m *Manager) Progressive(ctx context.Context, itemID int64, o Options) (io.ReadCloser, error) {
	if !m.Available() {
		return nil, ErrNotInstalled
	}
	if err := m.reserve(); err != nil {
		return nil, err
	}

	o.Output = Progressive
	s, stdout, err := startProgressive(ctx, m.bin, o)
	if err != nil {
		m.release()
		return nil, err
	}
	s.ID, s.ItemID = newID(), itemID

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
	o.OutputDir = filepath.Join(m.root, id)

	s, err := startHLS(ctx, m.bin, o)
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

func (m *Manager) reserve() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) >= m.MaxSessions {
		return ErrTooManySessions
	}
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
	}
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
