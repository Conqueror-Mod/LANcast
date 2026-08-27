//go:build hlsharness

/*
 * Step 2 of the ADR 0013 live-TV amendment, as a throwaway.
 *
 * The amendment claims the server "already does" produce HLS that a real
 * player can consume, and therefore that adopting MSE for live TV needs no
 * server work. That claim had never been tested, and it gates everything else
 * in the amendment — so this exists to answer it and then be deleted.
 *
 * It is behind a build tag for the same reason devseed is: a diagnostic that
 * ships is a diagnostic somebody runs against a live system by accident.
 *
 * It deliberately drives transcode.Args rather than assembling its own ffmpeg
 * command line. The question is whether *the shipping argument construction*
 * produces a usable live playlist; a hand-rolled command would answer a
 * question nobody asked.
 *
 *   go build -tags hlsharness -o hlsharness.exe ./cmd/hlsharness
 *   ./hlsharness.exe -url "<channel url>" -seconds 90
 */
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"lancast/internal/probe"
	"lancast/internal/transcode"
)

func main() {
	url := flag.String("url", "", "channel URL to read (required)")
	seconds := flag.Int("seconds", 90, "how long to let it run")
	keep := flag.Bool("keep", false, "keep the output directory for inspection")
	control := flag.Bool("control", false, "swap the VOD playlist flags for live ones, to isolate the cause")
	flag.Parse()

	if *url == "" {
		fmt.Fprintln(os.Stderr, "need -url")
		os.Exit(2)
	}
	if err := run(*url, *seconds, *keep, *control); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED:", err)
		os.Exit(1)
	}
}

func run(url string, seconds int, keep, control bool) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not on PATH: %w", err)
	}

	dir, err := os.MkdirTemp("", "hlsharness")
	if err != nil {
		return err
	}
	if !keep {
		defer os.RemoveAll(dir)
	}
	fmt.Println("output dir:", dir)

	/*
	 * Live and HLSInput describe the source honestly — a channel that never
	 * ends, delivered as a playlist. Copy for both streams is what
	 * LiveDecision produces for the overwhelmingly common H.264/AAC channel,
	 * and it is the cheapest case: if HLS output is wrong even when ffmpeg is
	 * only rewriting the container, no encoder setting can be the cause.
	 */
	opts := transcode.Options{
		Input:      url,
		Output:     transcode.HLS,
		OutputDir:  dir,
		Live:       true,
		HLSInput:   true,
		AudioIndex: -1,
		Decision:   probe.Decision{VideoAction: "copy", AudioAction: "copy"},
	}
	/*
	 * The args are printed because they are half the point of the harness —
	 * but never with the URL in them. A channel list is credentialed, often
	 * with a token in the path or a password in the query, which is the whole
	 * reason channelStream is a proxy that cannot be pointed anywhere. A
	 * diagnostic that prints the subscription to a terminal, a scrollback or a
	 * pasted bug report undoes that at the last step.
	 */
	args := transcode.Args(opts)
	if control {
		args = asLive(args)
		fmt.Println("CONTROL RUN: playlist type swapped to event, window bounded")
	}
	fmt.Println("ffmpeg args:", strings.Join(redact(args, url), " "))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()

	cmd := exec.Command(ffmpeg, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	warnings := map[string]int{}
	done := make(chan struct{})
	go func() {
		scanStderr(stderr, warnings)
		close(done)
	}()

	obs := watch(ctx, dir)

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	<-done

	return report(ffmpeg, dir, obs, warnings, seconds)
}

// observation is what the playlist did while it was watched.
type observation struct {
	firstPlaylist time.Duration
	firstSegment  time.Duration
	polls         int
	maxSegments   int
	sawEndList    bool
	playlistType  string
	// maxOnDisk is segments *written*, counted independently of the playlist.
	// The two diverge, and the divergence is the whole finding: ffmpeg can be
	// producing perfectly good media that no player can discover.
	maxOnDisk      int
	firstOnDisk    time.Duration
	targetDuration string
	mediaSequence  string
	// segmentsOverTime records the count at each poll, which is what says
	// whether the playlist rolls or only grows.
	segmentsOverTime []int
}

func watch(ctx context.Context, dir string) observation {
	var obs observation
	start := time.Now()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return obs
		case <-tick.C:
		}
		obs.polls++

		if onDisk, _ := filepath.Glob(filepath.Join(dir, "seg*.m4s")); len(onDisk) > 0 {
			if obs.firstOnDisk == 0 {
				obs.firstOnDisk = time.Since(start)
			}
			if len(onDisk) > obs.maxOnDisk {
				obs.maxOnDisk = len(onDisk)
			}
		}

		body, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
		if err != nil {
			continue
		}
		if obs.firstPlaylist == 0 {
			obs.firstPlaylist = time.Since(start)
		}

		segs := 0
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasSuffix(line, ".m4s"):
				segs++
			case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
				obs.sawEndList = true
			case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:"):
				obs.playlistType = after(line)
			case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
				obs.targetDuration = after(line)
			case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
				obs.mediaSequence = after(line)
			}
		}
		if segs > 0 && obs.firstSegment == 0 {
			obs.firstSegment = time.Since(start)
		}
		if segs > obs.maxSegments {
			obs.maxSegments = segs
		}
		obs.segmentsOverTime = append(obs.segmentsOverTime, segs)
	}
}

/*
 * asLive is the control, and it is not a proposed patch.
 *
 * It changes exactly two things about the shipping arguments — the playlist
 * type and the window size — so that a difference in outcome can only be
 * attributed to those. If the control produces a playlist and the shipping
 * arguments do not, the cause is the VOD flags and nothing else in a long
 * command line.
 *
 * What the real fix should be is a decision for whoever takes it: `event`
 * keeps every segment listed and grows without bound, which suits a channel
 * only if something prunes; a bounded sliding window discards history a viewer
 * might want. That is a design question, and this harness deliberately does
 * not answer it.
 */
func asLive(args []string) []string {
	out := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-hls_playlist_type":
			out = append(out, "-hls_playlist_type", "event")
			i++
		case "-hls_list_size":
			out = append(out, "-hls_list_size", "6")
			i++
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// redact replaces the channel URL wherever it appears in the argument list.
// Matching on the value rather than on a pattern is deliberate: a guess at what
// a credential looks like is a guess that eventually misses one.
func redact(args []string, url string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, url, "<channel url withheld>")
	}
	return out
}

func after(s string) string {
	_, rest, _ := strings.Cut(s, ":")
	return strings.TrimSpace(rest)
}

/*
 * scanStderr counts ffmpeg's complaints by kind.
 *
 * The exact counts matter rather than their presence: ADR 0013's
 * frag_every_frame fault was found by 2,192 duplicate-DTS warnings against
 * zero, not by anything failing. ffmpeg exits fine either way.
 */
func scanStderr(r io.Reader, into map[string]int) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		for _, kind := range []string{
			"Non-monotonous DTS", "Non-monotonic DTS", "duplicate",
			"Invalid timestamp", "corrupt", "Error", "error",
		} {
			if strings.Contains(line, kind) {
				into[kind]++
				break
			}
		}
	}
}

func report(ffmpeg, dir string, obs observation, warnings map[string]int, seconds int) error {
	fmt.Println()
	fmt.Println("== what the playlist did ==")
	fmt.Printf("polls:                %d over %ds\n", obs.polls, seconds)
	fmt.Printf("first playlist:       %s\n", dur(obs.firstPlaylist))
	fmt.Printf("first segment on disk:%s\n", " "+dur(obs.firstOnDisk))
	fmt.Printf("segments on disk:     %d\n", obs.maxOnDisk)
	fmt.Printf("first segment listed: %s\n", dur(obs.firstSegment))
	fmt.Printf("segments at end:      %d (max seen %d)\n", last(obs.segmentsOverTime), obs.maxSegments)
	fmt.Printf("EXT-X-PLAYLIST-TYPE:  %s\n", orAbsent(obs.playlistType))
	fmt.Printf("EXT-X-TARGETDURATION: %s\n", orAbsent(obs.targetDuration))
	fmt.Printf("EXT-X-MEDIA-SEQUENCE: %s\n", orAbsent(obs.mediaSequence))
	fmt.Printf("saw ENDLIST:          %v\n", obs.sawEndList)

	fmt.Println()
	fmt.Println("== ffmpeg complaints ==")
	if len(warnings) == 0 {
		fmt.Println("none")
	}
	kinds := make([]string, 0, len(warnings))
	for k := range warnings {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Printf("  %-20s %d\n", k, warnings[k])
	}

	fmt.Println()
	fmt.Println("== are the segments decodable ==")
	probeSegments(ffmpeg, dir)

	fmt.Println()
	fmt.Println("== verdict ==")
	var faults []string
	/*
	 * Segments on disk and segments a player can find are different facts, and
	 * separating them is what this harness turned out to be for. ffmpeg
	 * producing good media proves the encode; only the playlist proves the
	 * stream is *consumable*, and a player has no other way in.
	 */
	switch {
	case obs.maxOnDisk == 0:
		faults = append(faults, "no segment was ever written: ffmpeg produced nothing")
	case obs.firstPlaylist == 0:
		faults = append(faults, fmt.Sprintf(
			"%d segments written but no playlist ever appeared — nothing for a player to fetch",
			obs.maxOnDisk))
	case obs.firstSegment == 0:
		faults = append(faults, "a playlist appeared but never listed a segment")
	}
	/*
	 * The two that decide the amendment.
	 *
	 * A live playlist must roll and must not declare itself finished-and-whole.
	 * PLAYLIST-TYPE:VOD tells a player the stream is complete and seekable end
	 * to end, which for a channel is a lie that costs the live edge; and a list
	 * that only grows is an unbounded playlist and an unbounded directory.
	 */
	if strings.EqualFold(obs.playlistType, "vod") {
		faults = append(faults, "playlist declares itself VOD, which is untrue of a channel")
	}
	/*
	 * A growing list is only a fault where the playlist claims to be a bounded
	 * window. Under EVENT it is the spec: an event playlist keeps every segment
	 * and ffmpeg ignores hls_list_size for it. So this is reported rather than
	 * failed — the unbounded directory is a real operational cost and a real
	 * decision, but it is not evidence the output is unconsumable.
	 */
	if len(obs.segmentsOverTime) > 4 && !rolls(obs.segmentsOverTime) &&
		!strings.EqualFold(obs.playlistType, "event") {
		faults = append(faults, "segment count only grows: the window never rolls")
	}
	if len(faults) == 0 {
		if strings.EqualFold(obs.playlistType, "event") && !rolls(obs.segmentsOverTime) {
			fmt.Println("NOTE: an EVENT playlist keeps every segment, so both the")
			fmt.Println("playlist and the segment directory grow without bound. That")
			fmt.Println("is correct for the type and still needs an answer before it")
			fmt.Println("runs for days on a channel nobody closed.")
			fmt.Println()
		}
		fmt.Println("PASS — the existing HLS output looks consumable for live.")
		fmt.Println("Step 2 of the ADR 0013 amendment is satisfied on this channel.")
		return nil
	}
	fmt.Println("FAIL — the existing HLS output is not a live playlist:")
	for _, f := range faults {
		fmt.Println("  -", f)
	}
	fmt.Println()
	fmt.Println("Per the amendment, the fault is on the server side and the")
	fmt.Println("amendment is premature until it is fixed. No dependency is taken.")
	return nil
}

// rolls reports whether the segment count ever fell, which is what a sliding
// window does and what an ever-growing list never does.
func rolls(counts []int) bool {
	for i := 1; i < len(counts); i++ {
		if counts[i] < counts[i-1] {
			return true
		}
	}
	return false
}

func probeSegments(ffmpeg, dir string) {
	ffprobe := filepath.Join(filepath.Dir(ffmpeg), "ffprobe")

	segs, _ := filepath.Glob(filepath.Join(dir, "seg*.m4s"))
	sort.Strings(segs)
	if len(segs) == 0 {
		fmt.Println("no segments on disk to probe")
		return
	}
	fmt.Printf("%d segments on disk; probing the first\n", len(segs))

	// A segment is fMP4 and needs its init to be decodable at all.
	// Concatenating init + first segment is what a player does, so it is what
	// gets probed.
	joined := filepath.Join(dir, "probe.mp4")
	if err := concat(joined, filepath.Join(dir, "init.mp4"), segs[0]); err != nil {
		fmt.Println("could not join init+segment:", err)
		return
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-show_streams",
		"-print_format", "json", joined).Output()
	if err != nil {
		fmt.Println("ffprobe refused the segment:", err)
		return
	}
	var parsed struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		fmt.Println("unreadable ffprobe output:", err)
		return
	}
	if len(parsed.Streams) == 0 {
		fmt.Println("  no streams — the segment is not independently decodable")
		return
	}
	for _, s := range parsed.Streams {
		fmt.Printf("  %s: %s\n", s.CodecType, s.CodecName)
	}
}

func concat(dst string, parts ...string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, p := range parts {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if _, err := out.Write(b); err != nil {
			return err
		}
	}
	return nil
}

func dur(d time.Duration) string {
	if d == 0 {
		return "never"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
}

func last(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	return xs[len(xs)-1]
}

func orAbsent(s string) string {
	if s == "" {
		return "(absent)"
	}
	return s
}
