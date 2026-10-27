package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"lancast/internal/childproc"
)

/*
 * Chapters: the marks most MKV and MP4 files already carry.
 *
 * Read on demand rather than by the probe worker, for the reason lyrics are
 * (ReadRawTags): the worker's result answers "can this client play this file",
 * and a list of chapter names has no part in that answer. Nothing is stored;
 * the API caches the answer per file for as long as the file is unchanged.
 *
 * Parsing is a pure function of ffprobe's JSON, so the rules below are tested
 * against fixtures with no ffmpeg installed, like every other probe rule.
 */

// Chapter is one mark. EndMS is where the next one starts, or the end of the
// file for the last.
type Chapter struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Title   string `json:"title"`
}

type chaptersDoc struct {
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

/*
 * ParseChapters turns `ffprobe -show_chapters` output into marks.
 *
 * Three things real files do that the scrub bar should not show:
 *
 *   - A single chapter spanning the whole file. Some muxers write one so the
 *     list is never empty; one mark at zero tells nobody anything.
 *   - Chapters out of order, or two at the same instant. Sorted, and the
 *     second of a pair dropped, so "next chapter" always moves forward.
 *   - No titles. Named "Chapter N" by position, which is what every player
 *     shows and what the file would say if it said anything.
 *
 * A title that is only whitespace counts as none. Times ffprobe could not
 * read are skipped rather than placed at zero.
 */
func ParseChapters(raw []byte) ([]Chapter, error) {
	var doc chaptersDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse chapters: %w", err)
	}
	var out []Chapter
	for _, c := range doc.Chapters {
		start, err1 := strconv.ParseFloat(c.StartTime, 64)
		end, err2 := strconv.ParseFloat(c.EndTime, 64)
		if err1 != nil || err2 != nil || start < 0 || end < start {
			continue
		}
		title := ""
		for k, v := range c.Tags {
			if strings.EqualFold(k, "title") {
				title = strings.TrimSpace(v)
			}
		}
		out = append(out, Chapter{StartMS: int64(start * 1000), EndMS: int64(end * 1000), Title: title})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartMS < out[j].StartMS })

	dedup := out[:0]
	for _, c := range out {
		if len(dedup) > 0 && c.StartMS == dedup[len(dedup)-1].StartMS {
			continue
		}
		dedup = append(dedup, c)
	}
	out = dedup
	if len(out) < 2 {
		return []Chapter{}, nil
	}
	for i := range out {
		if out[i].Title == "" {
			out[i].Title = fmt.Sprintf("Chapter %d", i+1)
		}
		if i+1 < len(out) && out[i].EndMS > out[i+1].StartMS {
			out[i].EndMS = out[i+1].StartMS
		}
	}
	return out, nil
}

// ReadChapters asks ffprobe for a file's chapters. A container read, with no
// stream decoding: the cheap half of probing.
func (p *Prober) ReadChapters(ctx context.Context, path string) ([]Chapter, error) {
	bin, err := p.binary()
	if err != nil {
		return nil, err
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_chapters",
		// A separate argument, never interpolated into a shell string.
		path,
	)
	childproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read chapters %s: %w", path, err)
	}
	return ParseChapters(out)
}
