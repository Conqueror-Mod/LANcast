// Command introlab measures cross-episode intro detection against a real
// library. It is an instrument, not a feature: nothing in LANcast runs it, and
// goreleaser does not build it, so it cannot reach a release binary.
//
// It exists because the detector has tuning constants — the tolerance, the
// minimum run, the head window — and changing one of those is a claim about
// real television that should be checked against real television:
//
//	go run ./internal/marker/introlab <ffmpeg> <lancast.db> "<show>" <season> [episodes]
//
// What it found first time out is why it is kept. It's Always Sunny season 3
// returns a ~30s run in all five episodes at five positions between 44s and
// 193s — the length agrees to within a second while the position varies by two
// and a half minutes, which is what a real intro behind a variable cold open
// looks like, and why no rule may assume a fixed timestamp.
//
// It runs the shipping decision — IntroPeers, BestCommonRun, IntroFrom — beside
// variants of the run measurement, so a proposed change is judged by what it
// does to the answer rather than to one candidate.
package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
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
 * A variant of the run measurement. gap is how many consecutive disagreeing
 * frames a run may cross and continue; 0 is the shipping CommonRun.
 */
type variant struct {
	name string
	gap  int
}

var variants = []variant{{"strict", 0}, {"gap0.5s", 5}, {"gap2s", 20}}

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
		line := fmt.Sprintf("  E%02d %-24s", a.number, trunc(a.title, 24))
		for vi, v := range variants {
			var cands []marker.Candidate
			var raw []string
			for _, p := range marker.IntroPeers(len(eps), i, marker.PeersPerEpisode) {
				b := eps[p]
				if b.fp == nil {
					continue
				}
				m := bestBridged(a.phases, b.fp, marker.IntroTolerance, v.gap)
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
			in := marker.IntroFrom(cands)
			verdict := "none"
			if in.Found {
				found[vi]++
				verdict = fmt.Sprintf("%.1f-%.1f", in.StartSec, in.EndSec)
			}
			line += fmt.Sprintf(" | %s %-11s [%s]", v.name, verdict, strings.Join(raw, " "))
		}
		fmt.Println(line)
	}
	fmt.Printf("  FOUND")
	for vi, v := range variants {
		fmt.Printf("  %s=%d/%d", v.name, found[vi], len(eps))
	}
	fmt.Println()
}

// bestBridged is the package's own comparison, so the lab measures what ships.
func bestBridged(aPhases [][]uint32, b []uint32, maxTol, maxGap int) marker.Match {
	return marker.BestCommonRunBridging(aPhases, b, maxTol, maxGap)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
