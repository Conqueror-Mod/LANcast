package transcode

/*
 * Leave stops this viewer's HLS encode for an item they have stopped watching
 * (T-5).
 *
 * HLS has no backpressure. A progressive stream stalls when nobody reads it,
 * so the ten minutes IdleTimeout gives a paused film costs nothing; an HLS
 * encode writes segments to disk whether anyone asks for them or not, so the
 * same ten minutes after the viewer has *left* was a full encode running for
 * nobody. Seen on the installed service: The Fifth Element kept its ffmpeg
 * until it was reaped at idle_seconds=614, and every title opened in the
 * meantime was slow.
 *
 * The client knows when it leaves, the same signal StopLive takes for a
 * channel. Leaving is not seeking, so a finished conversion is spared: it
 * costs no CPU, and coming back to the film reuses it rather than encoding it
 * again. reserve still evicts it if a slot is wanted.
 *
 * Keyed on (owner, item) like every other stop, so leaving a film never ends
 * somebody else's viewing of it. The idle timeout stays as the backstop for a
 * client that never says it left.
 */
func (m *Manager) Leave(owner string, itemID int64) {
	m.supersedeOutput(owner, itemID, HLS, spareFinished)
}

/*
 * StopItem stops every session reading an item, whoever is watching it, and
 * reports how many it stopped.
 *
 * For deleting the file. ffmpeg holds the source open, and Windows will not
 * remove a file another process has open, so a delete during a conversion
 * failed and kept the row (R03-4) until the session was reaped. Removed under
 * the lock and stopped outside it, as StopSession does: Stop blocks until
 * ffmpeg has exited, which is what lets the remove that follows succeed.
 */
func (m *Manager) StopItem(itemID int64) int {
	m.mu.Lock()
	var dead []*Session
	for id, s := range m.sessions {
		if s.ItemID == itemID {
			dead = append(dead, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range dead {
		m.log.Info("stopping a transcode: its file is being deleted", "session", s.ID,
			"item", itemID, "served_bytes", s.Served())
		s.Stop()
	}
	return len(dead)
}
