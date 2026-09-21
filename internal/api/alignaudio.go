package api

import (
	"net/http"
	"time"

	"lancast/internal/probe"
)

/*
 * Where the audio input should be seeked to, so a copied resume starts both
 * streams at the same point ([ADR 0072](../../docs/adr/0072-a-copied-resume-starts-both-streams-together.md)).
 *
 * A copied video begins at a keyframe; the re-encoded audio begins exactly
 * where it was asked. The picture then runs behind the sound by the distance
 * between them, and no choice of seek closes it -- ffmpeg will not take a
 * keyframe until the target is about 200ms past it, so asking for the keyframe
 * itself drops to the previous one and the gap grows from a third of a second
 * to five.
 *
 * So the transcoder takes the audio from its own input, and this is the number
 * it needs. Zero means no alignment, which is the ordinary answer for most
 * playback: a re-encoded video starts where it is asked, and a resume at zero
 * has nothing to be out of step with.
 */
func (s *Server) audioStartFor(r *http.Request, path string, decision probe.Decision, startAt float64) float64 {
	if decision.VideoAction != "copy" || startAt <= 0 {
		return 0
	}
	if s.probes == nil {
		return 0
	}
	/*
	 * The request's own context, so a viewer who gives up takes the probe with
	 * them. It runs before a conversion that takes minutes, and holding a
	 * process for somebody who has closed the tab is the shape of fault this
	 * project has already paid for twice in this file's neighbourhood.
	 */
	kf, ok := s.probes.KeyframeBefore(r.Context(), path, time.Duration(startAt*float64(time.Second)))
	if !ok {
		/*
		 * No keyframe in the window, or the probe would not answer. Convert
		 * exactly as before: a worse alignment is not a reason to refuse to
		 * play a film, and this is what everybody has been watching.
		 */
		return 0
	}
	return kf.Seconds()
}
