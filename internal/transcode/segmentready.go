package transcode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Complete reports whether this session's playlist is listed whole up front.
func (s *Session) Complete() bool {
	return s.MediaSeconds > 0
}

/*
 * WaitForSegment blocks until ffmpeg has *finished* a segment, not merely begun
 * it.
 *
 * WaitForFile's test — the file exists with bytes in it — was enough while the
 * player only ever asked for segments ffmpeg's growing playlist had already
 * listed. A complete playlist lists segments the encode has not reached, and the
 * player asks for them about a segment ahead of the picture, which is exactly
 * when a file can exist and still be mid-write: ffmpeg writes segments in place.
 * Served that way in the experiment behind CompletePlaylist, init.mp4 went out
 * at 0 bytes and the element failed with APPEND_FAILED.
 *
 * So a segment is ready once ffmpeg's own playlist names it, which ffmpeg does
 * only after closing it; and the init segment once that playlist exists at all,
 * since ffmpeg writes init before the first segment. A finished ffmpeg has
 * closed everything it will ever write, so then existing is enough.
 *
 * Used for growing playlists too, where it changes nothing a player can see: a
 * player reading one only asks for what it lists.
 */
func (m *Manager) WaitForSegment(ctx context.Context, s *Session, name string, timeout time.Duration) (string, error) {
	path := filepath.Join(s.Dir, name)
	playlist := filepath.Join(s.Dir, "index.m3u8")
	deadline := time.Now().Add(timeout)

	for {
		if body, err := os.ReadFile(playlist); err == nil && len(body) > 0 {
			if name == "init.mp4" || Listed(string(body), name) {
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
			}
		}
		if done, ffErr := s.Done(); done {
			if ffErr != nil {
				return "", ffErr
			}
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
			return "", fmt.Errorf("transcode finished without producing %s: %s", name, s.Stderr())
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
