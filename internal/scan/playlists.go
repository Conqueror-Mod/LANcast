package scan

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"lancast/internal/playlist"
	"lancast/internal/store"
)

// Playlist import (ADR 0030).
//
// An .m3u on disk is an import, not a mirror: it seeds a playlist once, and the
// database is the truth afterwards. Everything below follows from that.

// membersLock is the locked field that marks a playlist a person has edited.
//
// Reusing item_lock rather than inventing a flag is the point: LANcast already
// has one mechanism for "a human decided this, do not overwrite it", the rule
// is enforced everywhere else, and a second mechanism would be a second thing
// to remember. This is that rule applied to membership — a rescan reconciles
// files, it does not re-litigate decisions.
const membersLock = "members"

// isPlaylistFile matches by extension only.
//
// .m3u8 is included and then usually rejected by the parser, which is
// deliberate: the extension cannot tell a UTF-8 playlist from an HLS one, and
// the only honest test is reading the #EXT-X- tags inside.
func isPlaylistFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u", ".m3u8":
		return true
	}
	return false
}

// importPlaylists turns the .m3u files found during the walk into playlists.
//
// Never fails the scan. A library is somebody's folder of files, and a
// malformed playlist in it is not a reason to abandon a scan that has just
// catalogued nine thousand tracks — each failure is an Issue on the report and
// the pass continues.
func (s *Scanner) importPlaylists(ctx context.Context, lib store.Library, p *Progress, paths []string) {
	for _, path := range paths {
		if s.skipLockedPlaylist(ctx, path) {
			continue
		}

		res, err := playlist.ImportFile(ctx, s.st, lib.ID, path)
		if err != nil {
			// Our own HLS output, or something else calling itself .m3u8. Not
			// an issue to report: nothing is wrong, and a user who sees
			// "could not import stream.m3u8" will reasonably go looking for a
			// problem that does not exist.
			if errors.Is(err, playlist.ErrHLS) {
				continue
			}
			s.log.Warn("playlist import failed", "path", path, "error", err)
			s.recordIssue(p, lib.Path, path, "could not read this playlist")
			continue
		}

		s.mu.Lock()
		p.PlaylistsImported++
		s.mu.Unlock()

		// What could not be found is reported, never silently dropped — the
		// difference between a playlist that is short and one that looks
		// complete and is not. It goes on the scan report rather than only into
		// the log, because the log is not where anyone looks after a scan.
		if res.MissingCount > 0 {
			s.recordIssue(p, lib.Path, path, missingReason(res))
		}
		s.log.Info("playlist imported", "library", lib.ID, "title", res.Title,
			"tracks", res.Imported, "missing", res.MissingCount, "skipped", res.Skipped)
	}
}

// skipLockedPlaylist reports whether this .m3u has already become a playlist
// that somebody has since edited.
//
// A lookup failure is treated as "not locked". The consequence of being wrong
// in that direction is re-importing a playlist, which is recoverable; being
// wrong in the other direction means a file on disk silently stops importing
// forever, which is not.
func (s *Scanner) skipLockedPlaylist(ctx context.Context, path string) bool {
	id, err := s.st.ItemIDByPath(ctx, path)
	if err != nil || id == 0 {
		return false
	}
	locked, err := s.st.LockedFields(ctx, id)
	if err != nil {
		return false
	}
	for _, f := range locked {
		if f == membersLock {
			s.log.Debug("playlist edited since import, leaving it alone", "path", path)
			return true
		}
	}
	return false
}

// missingReason phrases the count for a human reading the scan report.
func missingReason(res playlist.Result) string {
	total := res.Imported + res.MissingCount
	return fmt.Sprintf("imported %d of %d tracks; the rest are not in this library",
		res.Imported, total)
}
