package marker

import (
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"lancast/internal/store"
)

// IntroStore is the persistence the intro pass needs.
type IntroStore interface {
	PendingIntroSeasons(ctx context.Context, minEpisodes, limit int) ([]store.Season, error)
	SaveMarkers(ctx context.Context, itemID int64, kinds []string, markers []store.Marker) error
	MarkIntrosExamined(ctx context.Context, episodeIDs []int64, at int64) error
}

// IntroSource names this detector on every marker it writes.
const IntroSource = "fingerprint"

// PeersPerEpisode is how many siblings each episode is compared against.
//
// Four, because a majority of four is three and that is the smallest number
// that can outvote a coincidence. Pairwise over a 26-episode season would be
// 325 comparisons to learn what four say, and the decode dominates the cost.
const PeersPerEpisode = 4

/*
 * RunIntros examines seasons whose episodes have not been compared.
 *
 * Reuses the credits worker's ffmpeg path and its statistics, because it is
 * the same kind of work under the same setting: an expensive optional pass
 * that nothing waits on. What differs is the unit — a season rather than a
 * file — and that difference is the reason it is a separate method rather
 * than another branch inside examine.
 */
func (w *Worker) RunIntros(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats.Running = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.running = false
		w.stats.Running = false
		w.stats.UpdatedAt = time.Now().Unix()
		w.mu.Unlock()
	}()

	st, ok := w.st.(IntroStore)
	if !ok {
		return nil
	}

	examine := w.examineSeason
	if w.examineSeasonFn != nil {
		examine = w.examineSeasonFn
	}

	/*
	 * Until nothing is pending, not one batch.
	 *
	 * A pass took five seasons and returned, and a pass only starts at startup
	 * or after a library scan. v0.9.16's revision 45 queued every season of a
	 * real library for re-examination: the startup pass did exactly five —
	 * Black Books S1–S3, Blue Mountain State S1–S2 — and the other sixty sat
	 * there, because nothing scanned. The batch stays small so each query is
	 * cheap and the setting is re-checked often; the loop is what finishes.
	 *
	 * A season that fails is not stamped, so it comes back on the next query.
	 * Seasons are remembered for this pass, and a batch holding nothing new
	 * ends it — otherwise one unreadable season would be retried for ever.
	 * It is retried on the next pass, as before.
	 */
	tried := map[[2]int64]bool{}
	for {
		seasons, err := st.PendingIntroSeasons(ctx, 2, introSeasonBatch)
		if err != nil {
			return err
		}
		fresh := 0
		for _, se := range seasons {
			key := [2]int64{se.ShowID, int64(se.Season)}
			if tried[key] {
				continue
			}
			tried[key] = true
			fresh++
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !w.stillWanted() {
				return nil
			}
			if err := examine(ctx, st, se); err != nil {
				// Our own shutdown is not a failure of the season: it is
				// unstamped and comes back on the next pass. Logged as one, an
				// installer's restart read as a broken show (v0.9.54).
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.log.Warn("intro detection failed",
					"show", se.ShowName, "season", se.Season, "error", err)
			}
		}
		if fresh == 0 {
			return nil
		}
	}
}

// introSeasonBatch is how many seasons one query fetches. RunIntros keeps
// fetching until none are pending; this only bounds the query.
const introSeasonBatch = 5

/*
 * examineSeason fingerprints a season and writes what its episodes share.
 *
 * Every episode is decoded once and fingerprinted at every phase, then held
 * for the whole season. Holding them costs a few megabytes and saves decoding
 * each episode once per comparison — which is the difference between a season
 * costing n decodes and n times PeersPerEpisode.
 */
func (w *Worker) examineSeason(ctx context.Context, st IntroStore, se store.Season) error {
	n := len(se.Episodes)
	if n < 2 {
		return nil
	}

	head, tailAudio := w.decodeHead, w.decodeTail
	if w.headFn != nil {
		head = w.headFn
	}
	if w.tailAudioFn != nil {
		tailAudio = w.tailAudioFn
	}

	type fp struct {
		phases [][]uint32
		single []uint32
		ok     bool
		// The same for the last EndingTailSeconds, and the second of the
		// episode that tail begins at.
		endPhases [][]uint32
		endSingle []uint32
		endFrom   float64
		endOK     bool
	}
	prints := make([]fp, n)
	for i, ep := range se.Episodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !w.stillWanted() {
			return nil
		}
		samples, err := head(ctx, ep.Path, IntroHeadSeconds)
		if err != nil {
			// One unreadable episode does not spoil the season: it simply
			// takes no part in the comparison, and the others still have each
			// other.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Warn("intro decode failed", "item", ep.ID, "error", err)
			continue
		}
		prints[i] = fp{
			phases: FingerprintPhases(samples),
			single: Fingerprint(samples),
			ok:     true,
		}
		if ep.DurationMS == nil || *ep.DurationMS <= 0 {
			continue
		}
		end, err := tailAudio(ctx, ep.Path, EndingTailSeconds)
		if err != nil {
			// No ending to compare, and the black run still decides alone.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Warn("ending decode failed", "item", ep.ID, "error", err)
			continue
		}
		prints[i].endPhases = FingerprintPhases(end)
		prints[i].endSingle = Fingerprint(end)
		prints[i].endFrom = float64(*ep.DurationMS)/1000 - float64(len(end))/SampleRate
		prints[i].endOK = true
	}

	now := time.Now().Unix()
	examined := make([]int64, 0, n)
	for i, ep := range se.Episodes {
		examined = append(examined, ep.ID)
		if !prints[i].ok {
			continue
		}
		var cands []Candidate
		for _, p := range IntroPeers(n, i, PeersPerEpisode) {
			if !prints[p].ok {
				continue
			}
			m := BestCommonRunBridging(prints[i].phases, prints[p].single, IntroTolerance, IntroGapFrames)
			if m.Frames == 0 {
				// A comparison that found nothing is still a comparison, and
				// the majority rule counts it. Dropping it would let one
				// agreeing pair out of six look unanimous.
				cands = append(cands, Candidate{})
				continue
			}
			cands = append(cands, Candidate{
				StartSec: Seconds(m.OffsetA),
				EndSec:   Seconds(m.OffsetA + m.Frames),
			})
		}

		in := IntroFrom(cands)
		/*
		 * An ident is not the intro, and may be standing in front of it. The
		 * search runs again from just after it, on this episode's side only:
		 * a sibling's ident has nothing left on this side to align with.
		 */
		if in.IsIdent() {
			in = w.introAfter(prints[i].phases, func(p int) ([]uint32, bool) {
				return prints[p].single, prints[p].ok
			}, n, i, in.EndSec)
		}

		/*
		 * The closing theme, found the same way: what this episode's last
		 * minutes share with its siblings'. Times are moved into this
		 * episode's own timeline, since each tail began at a different second.
		 */
		var endCands []Candidate
		if prints[i].endOK {
			for _, p := range IntroPeers(n, i, PeersPerEpisode) {
				if !prints[p].endOK {
					continue
				}
				m := BestCommonRunBridging(prints[i].endPhases, prints[p].endSingle, IntroTolerance, IntroGapFrames)
				if m.Frames == 0 {
					endCands = append(endCands, Candidate{})
					continue
				}
				from := prints[i].endFrom
				endCands = append(endCands, Candidate{
					StartSec: from + Seconds(m.OffsetA),
					EndSec:   from + Seconds(m.OffsetA+m.Frames),
				})
			}
		}
		credits, decided := w.episodeCredits(ctx, ep, IntroFrom(endCands))
		if ctx.Err() != nil {
			// Stopped mid-season: a frame read cut short reads as a refusal,
			// and recording what followed from it would be recording the
			// shutdown.
			return ctx.Err()
		}

		var markers []store.Marker
		if in.Found {
			end := int64(in.EndSec * 1000)
			markers = append(markers, store.Marker{
				Kind:       store.MarkerIntro,
				StartMS:    int64(in.StartSec * 1000),
				EndMS:      &end,
				Source:     IntroSource,
				Confidence: in.Confidence,
			})
			w.mu.Lock()
			w.stats.Found++
			w.mu.Unlock()
		}
		/*
		 * Credits are written with the intro, in one call, and only when the
		 * black run could be read; an unreadable file keeps whatever it had.
		 *
		 * Writing credits stamps markers_at, which is the point: the per-file
		 * pass then leaves this episode alone, and cannot replace a decision
		 * made from two kinds of evidence with one made from one. A one-episode
		 * season never comes here, so it keeps the per-file pass's answer.
		 */
		kinds := []string{store.MarkerIntro}
		if decided {
			kinds = append(kinds, store.MarkerCredits)
			if credits.Found {
				markers = append(markers, store.Marker{
					Kind:       store.MarkerCredits,
					StartMS:    credits.StartMS,
					Source:     credits.Source,
					Confidence: credits.Confidence,
				})
			}
		}
		if err := st.SaveMarkers(ctx, ep.ID, kinds, markers); err != nil {
			return err
		}
		w.mu.Lock()
		w.stats.Examined++
		w.mu.Unlock()
	}

	// Stamped whether or not anything was found, so a season with no shared
	// audio is not re-decoded on every pass for ever.
	return st.MarkIntrosExamined(ctx, examined, now)
}

/*
 * introAfter decides an intro from the comparisons again, ignoring everything
 * in this episode before afterSec. Its answer is in this episode's timeline,
 * and an ident it finds again is refused rather than returned.
 */
func (w *Worker) introAfter(phases [][]uint32, peer func(int) ([]uint32, bool), n, i int, afterSec float64) Intro {
	cut := int(afterSec/Seconds(1)) + 1
	trimmed := make([][]uint32, 0, len(phases))
	for _, ph := range phases {
		if cut >= len(ph) {
			return Intro{}
		}
		trimmed = append(trimmed, ph[cut:])
	}
	var cands []Candidate
	for _, p := range IntroPeers(n, i, PeersPerEpisode) {
		b, ok := peer(p)
		if !ok {
			continue
		}
		m := BestCommonRunBridging(trimmed, b, IntroTolerance, IntroGapFrames)
		if m.Frames == 0 {
			cands = append(cands, Candidate{})
			continue
		}
		cands = append(cands, Candidate{
			StartSec: Seconds(m.OffsetA + cut),
			EndSec:   Seconds(m.OffsetA + cut + m.Frames),
		})
	}
	in := IntroFrom(cands)
	if in.IsIdent() {
		return Intro{}
	}
	return in
}

/*
 * episodeCredits decides one episode's credits from its black runs and the
 * ending it shares with its season (EpisodeCreditsFrom). decided is false when
 * the black run could not be read, and then nothing should be written.
 *
 * The black scan starts just below the window rather than at ScanFrom's 75%:
 * nothing below 88% is a candidate, and an episode has no use for the margin a
 * film's long credit roll is given. It is half the decode.
 */
func (w *Worker) episodeCredits(ctx context.Context, ep store.Item, ending Intro) (EpisodeCredits, bool) {
	if ep.DurationMS == nil || *ep.DurationMS <= 0 {
		return EpisodeCredits{}, false
	}
	dur := float64(*ep.DurationMS) / 1000
	tail, shapeAt := w.scanTail, w.frameShape
	if w.tailFn != nil {
		tail = w.tailFn
	}
	if w.shapeFn != nil {
		shapeAt = w.shapeFn
	}
	from := dur * EpisodeScanFrom
	stderr, err := tail(ctx, ep.Path, from)
	if err != nil {
		// A scan killed by our own shutdown is not a broken file, the same
		// distinction the per-file pass draws (examine).
		if ctx.Err() == nil {
			w.log.Warn("episode credits scan failed", "item", ep.ID, "error", err)
		}
		return EpisodeCredits{}, false
	}
	black := CreditsFrom(ParseBlackDetect(stderr, from), dur, nil)
	gate := func(start float64) bool {
		var shapes []Shape
		for _, off := range GateOffsets {
			if at := start + off; at < dur-1 {
				shapes = append(shapes, shapeAt(ctx, ep.Path, at))
			}
		}
		return LooksLikeCredits(shapes)
	}
	return EpisodeCreditsFrom(black, ending, dur, gate), true
}

const (
	// EndingTailSeconds is how much of an episode's end is fingerprinted.
	// Cowboy Bebop's closing song and preview run two minutes; five is room.
	EndingTailSeconds = 300
	// EpisodeScanFrom is where an episode's black-run scan begins.
	EpisodeScanFrom = 0.87
)

/*
 * decodeTail is decodeHead for the last seconds of a file.
 *
 * -sseof seeks from the end, so the samples finish where the episode does and
 * the caller places them by subtracting their length from the duration.
 */
func (w *Worker) decodeTail(ctx context.Context, path string, secs int) ([]float64, error) {
	out, err := exec.CommandContext(ctx, w.bin(),
		"-hide_banner", "-nostats", "-v", "error",
		"-threads", strconv.Itoa(w.threads()),
		"-sseof", "-"+strconv.Itoa(secs),
		"-i", path,
		"-vn",
		"-ac", "1",
		"-ar", strconv.Itoa(SampleRate),
		"-f", "s16le", "-",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	return samplesOf(out)
}

/*
 * decodeHead returns the first seconds of an episode as mono 8 kHz samples.
 *
 * -vn and a low sample rate because the fingerprint reads nothing above 3.5
 * kHz: decoding video or 48 kHz stereo would be several times the work to
 * produce the same hashes.
 *
 * No -hwaccel, for the reason the credits scan gives: this runs as a service
 * in session 0 where there is no D3D device.
 */
func (w *Worker) decodeHead(ctx context.Context, path string, secs int) ([]float64, error) {
	out, err := exec.CommandContext(ctx, w.bin(),
		"-hide_banner", "-nostats", "-v", "error",
		// Capped for the same reason the credits scan is: one ffmpeg with a
		// filter attached will take the whole machine, and nothing waits on a
		// marker.
		"-threads", strconv.Itoa(w.threads()),
		"-t", strconv.Itoa(secs),
		"-i", path,
		"-vn",
		"-ac", "1",
		"-ar", strconv.Itoa(SampleRate),
		"-f", "s16le", "-",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	return samplesOf(out)
}

// samplesOf turns ffmpeg's s16le output into samples in [-1, 1).
func samplesOf(out []byte) ([]float64, error) {
	n := len(out) / 2
	if n < FrameSize {
		return nil, fmt.Errorf("only %d samples decoded", n)
	}
	s := make([]float64, n)
	for i := 0; i < n; i++ {
		s[i] = float64(int16(binary.LittleEndian.Uint16(out[i*2:]))) / 32768
	}
	return s, nil
}
