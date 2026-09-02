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
package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"

	"lancast/internal/marker"

	_ "modernc.org/sqlite"
)

const headSeconds = 420 // 7 minutes: an intro is never later than this

func decodeHead(ffmpeg, path string) ([]float64, error) {
	cmd := exec.Command(ffmpeg,
		"-hide_banner", "-nostats", "-v", "error",
		"-t", strconv.Itoa(headSeconds),
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
	id      int64
	season  int
	number  int
	title   string
	path    string
	fp      []uint32
	phases  [][]uint32
	introAt float64
	introTo float64
	votes   int
}

func main() {
	ffmpeg := os.Args[1]
	dbPath := os.Args[2]
	show := os.Args[3]
	season, _ := strconv.Atoi(os.Args[4])
	limit := 6
	if len(os.Args) > 5 {
		limit, _ = strconv.Atoi(os.Args[5])
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		panic(err)
	}
	rows, err := db.Query(`
		SELECT e.id, e.season, e.episode, e.title, e.path
		FROM media_item e
		LEFT JOIN media_item se ON se.id = e.parent_id
		JOIN media_item sh ON sh.id = COALESCE(se.parent_id, e.parent_id)
		WHERE e.kind='episode' AND e.missing=0 AND sh.kind='show'
		  AND sh.title = ? AND e.season = ?
		ORDER BY e.episode LIMIT ?`, show, season, limit)
	if err != nil {
		panic(err)
	}
	var eps []*ep
	for rows.Next() {
		e := &ep{}
		if err := rows.Scan(&e.id, &e.season, &e.number, &e.title, &e.path); err != nil {
			panic(err)
		}
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
		if err != nil {
			fmt.Printf("  E%02d decode failed: %v\n", e.number, err)
			continue
		}
		e.fp = marker.Fingerprint(s)
		e.phases = marker.FingerprintPhases(s)
	}

	// Every episode against every other. Expensive and fine here: the point is
	// to see what the signal looks like, not to be the shipping algorithm.
	for i, a := range eps {
		if a.fp == nil {
			continue
		}
		type cand struct{ at, to, run float64 }
		var cands []cand
		for j, b := range eps {
			if i == j || b.fp == nil {
				continue
			}
			m := marker.BestCommonRun(a.phases, b.fp, 3)
			if marker.Seconds(m.Frames) < 5 {
				continue
			}
			cands = append(cands, cand{
				at:  marker.Seconds(m.OffsetA),
				to:  marker.Seconds(m.OffsetA + m.Frames),
				run: marker.Seconds(m.Frames),
			})
		}
		// Every candidate, not the median: the spread within one episode is
		// what the aggregation rule has to survive, and it is not the same
		// quantity as the spread of medians across episodes.
		fmt.Printf("  E%02d %-28s ", a.number, trunc(a.title, 28))
		for _, c := range cands {
			fmt.Printf("[%.0f-%.0fs %.1fs] ", c.at, c.to, c.run)
		}
		fmt.Println()

		if len(cands) == 0 {
			fmt.Printf("  E%02d %-34s no shared stretch\n", a.number, trunc(a.title, 34))
			continue
		}
		// The median candidate, so one odd pairing cannot decide it.
		sort.Slice(cands, func(x, y int) bool { return cands[x].run < cands[y].run })
		med := cands[len(cands)/2]
		a.introAt, a.introTo, a.votes = med.at, med.to, len(cands)
		fmt.Printf("  E%02d %-34s intro %6.1fs → %6.1fs  (%.1fs, agreed by %d/%d)\n",
			a.number, trunc(a.title, 34), med.at, med.to, med.run, len(cands), len(eps)-1)
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
