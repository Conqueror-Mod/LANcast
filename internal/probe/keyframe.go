package probe

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"lancast/internal/childproc"
)

/*
 * Where the video can actually start, for a resume that copies the picture
 * ([ADR 0072](../../docs/adr/0072-a-copied-resume-starts-both-streams-together.md)).
 *
 * A copied track has to begin at a keyframe, because nothing is decoded and
 * there is nothing else to begin at. The audio is re-encoded and begins
 * exactly where it was asked, so the two start at different points in the film
 * and the picture runs behind the sound by the distance between them.
 *
 * Nothing can align them without knowing where that keyframe is, and ffmpeg
 * will not say in advance. So it is asked here, before the conversion starts.
 *
 * Split the way this package is always split ([ADR 0012](../../docs/adr/0012-probe-before-transcode.md)):
 * ParseKeyframes is pure and carries the rule, Keyframes is the process call.
 * The rule is the part that can be wrong, and it is the part that would
 * otherwise need a film on disk to test.
 */

/*
 * KeyframeWindow is how far back to look.
 *
 * Ten seconds, measured rather than guessed. At three points in a 105-minute
 * film the keyframe before the resume was 0.351s, 2.214s and 2.384s back, and
 * a ten-second window cost 67-136ms. Five seconds would have been enough for
 * all three and leaves no room for a file with longer groups of pictures;
 * fifteen bought nothing and cost more.
 */
const KeyframeWindow = 10 * time.Second

/*
 * KeyframeBefore returns the latest keyframe at or before at, looking back
 * KeyframeWindow.
 *
 * Returns ok=false when there is none to be had — a window with no keyframe in
 * it, a format that will not answer, a probe that fails. Every caller treats
 * that as "convert the way we always did", because an alignment that cannot be
 * improved is not a reason to refuse to play a film.
 */
func (p *Prober) KeyframeBefore(ctx context.Context, path string, at time.Duration) (time.Duration, bool) {
	if at <= 0 {
		return 0, false
	}
	bin, err := p.binary()
	if err != nil {
		return 0, false
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	from := at - KeyframeWindow
	if from < 0 {
		from = 0
	}

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-select_streams", "v:0",
		// Decode keyframes only. This is what makes a ten-second window cost
		// a tenth of a second rather than a full decode of it.
		"-skip_frame", "nokey",
		"-show_entries", "frame=pts_time",
		"-of", "csv=p=0",
		"-read_intervals", fmt.Sprintf("%s%%%s", seconds(from), seconds(at)),
		// A separate argument, never interpolated into a shell string.
		path,
	)
	childproc.Hide(cmd)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return 0, false
		}
		return 0, false
	}
	return ParseKeyframeBefore(string(out), at)
}

/*
 * ParseKeyframeBefore picks the latest keyframe at or before at from ffprobe's
 * csv output. The testable half.
 *
 * ffprobe's csv writer leaves a trailing separator on a single field, so every
 * line arrives as "399.649000," — parsing that as a float fails, and failing
 * quietly here would mean silently never aligning anything. Lines that are not
 * numbers are skipped rather than refused, because the same output carries
 * blank lines and the occasional "N/A" for a frame with no timestamp.
 */
func ParseKeyframeBefore(out string, at time.Duration) (time.Duration, bool) {
	var best time.Duration
	found := false

	for _, line := range strings.Split(out, "\n") {
		field := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if field == "" {
			continue
		}
		secs, err := strconv.ParseFloat(field, 64)
		if err != nil || secs < 0 {
			continue
		}
		t := time.Duration(secs * float64(time.Second))
		if t > at {
			continue
		}
		if !found || t > best {
			best = t
			found = true
		}
	}
	return best, found
}

// seconds formats a duration the way ffprobe's -read_intervals wants it.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}
