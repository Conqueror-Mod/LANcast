// Command introlab measures cross-episode intro detection against a real
// library. It is an instrument, not a feature: nothing in LANcast runs it, and
// goreleaser does not build it, so it cannot reach a release binary.
//
//	go run ./internal/marker/introlab <ffmpeg> <lancast.db> "<show>" <season> [episodes]
//
// What it found first time out is why it is kept. It's Always Sunny season 3
// returns a ~30s run in all five episodes at five positions between 44s and
// 193s — the length agrees to within a second while the position varies by two
// and a half minutes, which is what a real intro behind a variable cold open
// looks like, and why no rule may assume a fixed timestamp.
//
// It measures whatever question is open. The gap allowance was settled in
// v0.9.16 and is now fixed at the shipping value; what is open after the
// library-wide re-check is why some seasons still answer on only half their
// episodes. Two candidate explanations are measured side by side against the
// shipping rule, with seasons that already work as controls:
//
//   - more peers per episode, since Star Trek: TNG S4 answered 12 of 12 when
//     compared over twelve episodes and 11 of 25 as shipped over the whole
//     season, which is a difference in who each episode was compared against;
//   - a "two strong agreeing" allowance, since Sunny S15 E02 returned
//     115s+20.6s, 115s+19.9s, 18s+3.9s, 18s+3.9s — two comparisons finding the
//     real intro and two finding a four-second network ident, which is 2 of 4
//     and so no majority. The two short ones never reach the clustering step at
//     all: they are under the eight-second floor.
//
// A rule that rescues a failing season by inventing intros in a working one is
// not an improvement, which is what the controls are for.
package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"lancast/internal/marker"

	_ "modernc.org/sqlite"
)

func decodeHead(ffmpeg, path string) ([]float64, error) {
	cmd := exec.Command(ffmpeg,
		"-hide_banner", "-nostats", "-v", "error",
		"-t", strconv.Itoa(marker.IntroHeadSeconds),
		"-i", path,
		"-vn",
		"-ac", "1",
		"-ar", strconv.Itoa(marker.SampleRate),
		"-f", "s16le", "-",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	n := len(out) / 2
	s := make([]float64, n)
	for i := 0; i < n; i++ {
		s[i] = float64(int16(binary.LittleEndian.Uint16(out[i*2:]))) / 32768
	}
	return s, nil
}

type ep struct {
	number int
	title  string
	path   string
	fp     []uint32
	phases [][]uint32
}

/*
 * A variant is a way of deciding, measured against the shipping one: how many
 * siblings each episode is compared against, and which rule reads the result.
 */
type variant struct {
	name  string
	peers int
	rule  func([]marker.Candidate) marker.Intro
}

/*
 * The old rule, spelled out rather than called.
 *
 * `marker.IntroFrom` is the *current* rule, so once a change lands there every
 * variant that calls it measures the same thing — which is how a before-and-
 * after table came to be produced from five identical columns. The comparison
 * this instrument exists to make needs the previous rule written down.
 */
func oldRule(cands []marker.Candidate) marker.Intro {
	if in := marker.IntroFromRule(cands, marker.IntroMinSeconds, false); in.Found {
		return in
	}
	return marker.IntroFromRule(cands, marker.IntroCardMinSeconds, true)
}

var variants = []variant{
	{"old", marker.PeersPerEpisode, oldRule},
	{"old6", 6, oldRule},
	{"old8", 8, oldRule},
	{"new", marker.PeersPerEpisode, marker.IntroFrom},
	{"new6", 6, marker.IntroFrom},
}

/*
 * twoStrong accepts two comparisons that agree far more tightly than the
 * shipping rule asks, on a run comfortably longer than the floor.
 *
 * The shipping rule needs a majority of *all* comparisons, which is right when
 * the minority found nothing: three of eight agreeing is three agreeing and
 * five saying nothing. It is arguably wrong when the minority found something
 * else and too short to be an intro at all — a network ident — which is what
 * Sunny S15 looks like. Measured here rather than argued: the guard is a
 * one-second start spread and a twelve-second run, both much stricter than
 * IntroStartSlack and IntroMinSeconds, so it cannot promote the scattered
 * near-misses that the majority rule refuses for good reason.
 */
const (
	tightSlack = 1.0
	strongMin  = 12.0
)

func twoStrong(cands []marker.Candidate) marker.Intro {
	if in := marker.IntroFrom(cands); in.Found {
		return in
	}
	var strong []marker.Candidate
	for _, c := range cands {
		if c.StartSec >= 0 && c.Len() >= strongMin && c.Len() <= marker.IntroMaxSeconds {
			strong = append(strong, c)
		}
	}
	if len(strong) < 2 {
		return marker.Intro{Compared: len(cands)}
	}
	sort.Slice(strong, func(i, j int) bool { return strong[i].StartSec < strong[j].StartSec })
	bestAt, bestN := 0, 0
	for i := range strong {
		j := i
		for j < len(strong) && strong[j].StartSec-strong[i].StartSec <= tightSlack {
			j++
		}
		if j-i > bestN {
			bestAt, bestN = i, j-i
		}
	}
	if bestN < 2 {
		return marker.Intro{Compared: len(cands)}
	}
	group := strong[bestAt : bestAt+bestN]
	starts := make([]float64, len(group))
	ends := make([]float64, len(group))
	for i, c := range group {
		starts[i], ends[i] = c.StartSec, c.EndSec
	}
	sort.Float64s(starts)
	sort.Float64s(ends)
	return marker.Intro{
		Found: true, StartSec: starts[len(starts)/2], EndSec: ends[len(ends)/2],
		Agreed: bestN, Compared: len(cands),
		Confidence: float64(bestN) / float64(len(cands)),
	}
}

func main() {
	ffmpeg := os.Args[1]
	dbPath := os.Args[2]
	show := os.Args[3]
	season, _ := strconv.Atoi(os.Args[4])
	limit := 30
	if len(os.Args) > 5 {
		limit, _ = strconv.Atoi(os.Args[5])
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		panic(err)
	}
	rows, err := db.Query(`
		SELECT e.episode, e.title, e.path
		FROM media_item e
		LEFT JOIN media_item se ON se.id = e.parent_id
		JOIN media_item sh ON sh.id = COALESCE(se.parent_id, e.parent_id)
		WHERE e.kind='episode' AND e.missing=0 AND sh.kind='show'
		  AND sh.title = ? AND COALESCE(e.season, 0) = ?
		  AND e.path IS NOT NULL AND e.probed_at IS NOT NULL
		ORDER BY e.episode, e.id LIMIT ?`, show, season, limit)
	if err != nil {
		panic(err)
	}
	var eps []*ep
	for rows.Next() {
		e := &ep{}
		var num sql.NullInt64
		if err := rows.Scan(&num, &e.title, &e.path); err != nil {
			panic(err)
		}
		e.number = int(num.Int64)
		eps = append(eps, e)
	}
	rows.Close()
	if len(eps) < 2 {
		fmt.Println("need at least two episodes")
		return
	}

	fmt.Printf("%s S%d — %d episodes\n", show, season, len(eps))
	for _, e := range eps {
		s, err := decodeHead(ffmpeg, e.path)
		if err != nil || len(s) < marker.FrameSize {
			fmt.Printf("  E%02d decode failed: %v\n", e.number, err)
			continue
		}
		e.fp = marker.Fingerprint(s)
		e.phases = marker.FingerprintPhases(s)
	}

	found := make([]int, len(variants))
	for i, a := range eps {
		if a.fp == nil {
			continue
		}
		line := fmt.Sprintf("  E%02d %-22s", a.number, trunc(a.title, 22))
		var shippedRaw []string
		for vi, v := range variants {
			cands, raw := compare(eps, i, v.peers)
			if vi == 0 {
				shippedRaw = raw
			}
			in := v.rule(cands)
			verdict := "none"
			if in.Found {
				found[vi]++
				verdict = fmt.Sprintf("%.0f-%.0f", in.StartSec, in.EndSec)
			}
			line += fmt.Sprintf(" | %s %-9s", v.name, verdict)
		}
		fmt.Printf("%s  [%s]\n", line, strings.Join(shippedRaw, " "))
	}
	fmt.Printf("  FOUND")
	for vi, v := range variants {
		fmt.Printf("  %s=%d/%d", v.name, found[vi], len(eps))
	}
	fmt.Println()
}

// compare builds one episode's candidates against the peers it is given, using
// the package's own comparison at the shipping tolerance and gap.
func compare(eps []*ep, self, peers int) ([]marker.Candidate, []string) {
	var cands []marker.Candidate
	var raw []string
	for _, p := range marker.IntroPeers(len(eps), self, peers) {
		b := eps[p]
		if b.fp == nil {
			continue
		}
		m := marker.BestCommonRunBridging(eps[self].phases, b.fp,
			marker.IntroTolerance, marker.IntroGapFrames)
		if m.Frames == 0 {
			cands = append(cands, marker.Candidate{})
			raw = append(raw, "-")
			continue
		}
		c := marker.Candidate{
			StartSec: marker.Seconds(m.OffsetA),
			EndSec:   marker.Seconds(m.OffsetA + m.Frames),
		}
		cands = append(cands, c)
		raw = append(raw, fmt.Sprintf("%.0f+%.1f", c.StartSec, c.Len()))
	}
	return cands, raw
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
