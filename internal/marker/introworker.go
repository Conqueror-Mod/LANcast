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

	seasons, err := st.PendingIntroSeasons(ctx, 2, 5)
	if err != nil {
		return err
	}
	for _, se := range seasons {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !w.stillWanted() {
			return nil
		}
		if err := w.examineSeason(ctx, st, se); err != nil {
			w.log.Warn("intro detection failed",
				"show", se.ShowName, "season", se.Season, "error", err)
		}
	}
	return nil
}

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

	type fp struct {
		phases [][]uint32
		single []uint32
		ok     bool
	}
	prints := make([]fp, n)
	for i, ep := range se.Episodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !w.stillWanted() {
			return nil
		}
		samples, err := w.decodeHead(ctx, ep.Path, IntroHeadSeconds)
		if err != nil {
			// One unreadable episode does not spoil the season: it simply
			// takes no part in the comparison, and the others still have each
			// other.
			w.log.Warn("intro decode failed", "item", ep.ID, "error", err)
			continue
		}
		prints[i] = fp{
			phases: FingerprintPhases(samples),
			single: Fingerprint(samples),
			ok:     true,
		}
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
			m := BestCommonRun(prints[i].phases, prints[p].single, IntroTolerance)
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
		if err := st.SaveMarkers(ctx, ep.ID, []string{store.MarkerIntro}, markers); err != nil {
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
