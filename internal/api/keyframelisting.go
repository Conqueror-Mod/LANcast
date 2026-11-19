package api

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"lancast/internal/matroska"
	"lancast/internal/probe"
	"lancast/internal/transcode"
)

/*
 * A copied film listed whole from its own keyframe index, for a Roku (ADR 0125,
 * amended).
 *
 * A Roku abandons a playlist request after about thirty seconds, and a copied
 * video track was only listed once ffmpeg had finished cutting it: on the TV
 * that was up to four tries and two minutes for one film, and a black screen
 * for another. A Matroska file indexes its keyframes, and from them where
 * ffmpeg cuts is arithmetic (transcode.KeyframeCuts, checked against ffmpeg
 * itself on real films, from the start and from resumes, audio copied and
 * converted). So the playlist goes out at once and each segment is waited for
 * when it is asked for, as an encode's are.
 *
 * Only for MPEG-TS segments, which only the roku profile asks for. Every other
 * client keeps the wait it was measured with. Anything this cannot read, a
 * file that is not Matroska included, is listed the way it was before.
 */

// keyframeListing is where a copied session will start and cut.
type keyframeListing struct {
	seek float64   // the session's start: just past its keyframe on a resume
	cuts []float64 // where each segment begins, on the source's clock
}

func keyframeListingFor(path string, d probe.Decision, startAt, duration float64) (keyframeListing, bool) {
	if d.Segments != probe.SegmentsMPEGTS || d.VideoAction != "copy" || d.AudioOnly || duration <= 0 {
		return keyframeListing{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return keyframeListing{}, false
	}
	keys, err := matroska.Keyframes(f)
	f.Close()
	if err != nil {
		return keyframeListing{}, false
	}
	seek := 0.0
	if startAt > 0 {
		var ok bool
		if _, seek, ok = transcode.CopyStart(keys, startAt); !ok {
			return keyframeListing{}, false
		}
	}
	cuts := transcode.KeyframeCuts(keys, seek, transcode.SegmentSeconds)
	if len(cuts) == 0 || !(duration > cuts[len(cuts)-1]) {
		return keyframeListing{}, false
	}
	return keyframeListing{seek: seek, cuts: cuts}, true
}

/*
 * checkKeyframeListing compares, once the remux has finished, the segments a
 * keyframe listing named with the ones ffmpeg wrote, and says which in the log.
 *
 * A wrong count is quiet from the front: the film ends a segment early, or the
 * last request fails. So the comparison is made on every session and logged
 * either way, which is what the next look at a log needs to trust it.
 */
func (s *Server) checkKeyframeListing(sess *transcode.Session, itemID int64, listed int) {
	if _, seen := s.listingChecked.LoadOrStore(sess.ID, true); seen {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		done, why := s.trans.WaitForEndlist(ctx, sess,
			transcode.Patience{Stall: 2 * time.Minute, Cap: 30 * time.Minute})
		if !done {
			s.log.Debug("keyframe listing not checked: the remux did not finish",
				"item", itemID, "session", sess.ID, "why", why)
			return
		}
		raw, err := os.ReadFile(filepath.Join(sess.Dir, "index.m3u8"))
		if err != nil {
			return
		}
		written := transcode.ListedCount(string(raw))
		if written == listed {
			s.log.Info("keyframe listing matched ffmpeg",
				"item", itemID, "session", sess.ID, "segments", listed)
			return
		}
		s.log.Warn("keyframe listing differs from what ffmpeg wrote",
			"item", itemID, "session", sess.ID, "listed", listed, "written", written)
	}()
}
