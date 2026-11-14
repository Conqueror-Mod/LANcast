package transcode

import (
	"os"
	"path/filepath"
	"time"
)

/*
 * AdoptForTest registers an HLS session with no ffmpeg behind it, for another
 * package's tests.
 *
 * The handlers that serve a session's files decide who may read it, and that
 * decision is what needs testing — not ffmpeg. Without this the only way to
 * get a session into a Manager from outside the package is a real encode,
 * which is why the segment route went untested against a second caller and
 * shipped serving anybody who named a session id (review finding R03-1).
 *
 * The session's directory is created and left empty; the test writes the
 * playlist and segments it wants served. It is finished as far as Done is
 * concerned, so a missing file answers at once rather than after a wait.
 * Never called outside tests, the same convention as release.SetKeyForTest.
 */
func (m *Manager) AdoptForTest(itemID int64, owner string) (*Session, error) {
	id := newID()
	dir := filepath.Join(m.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	now := time.Now()
	s := &Session{
		ID: id, ItemID: itemID, Owner: owner, Output: HLS, Dir: dir,
		started: now, lastTouch: now, done: true,
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}
