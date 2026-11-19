package transcode

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"lancast/internal/matroska"
	"lancast/internal/probe"
)

/*
TestKeyframeCutsMatchFFmpeg runs the server's own remux command on real files
and checks that the playlist worked out from the index names exactly the
segments ffmpeg wrote, from the start and from a resume. Set LANCAST_MKV to
paths separated by "|"; it needs ffmpeg and ffprobe on the PATH, and writes a
copy of each film to the temp folder while it runs.
*/
func TestKeyframeCutsMatchFFmpeg(t *testing.T) {
	list := os.Getenv("LANCAST_MKV")
	if list == "" {
		t.Skip("LANCAST_MKV not set")
	}
	// LANCAST_MKV_AUDIO=encode checks the DTS case: video copied, audio to AAC.
	audioAction := "copy"
	if os.Getenv("LANCAST_MKV_AUDIO") == "encode" {
		audioAction = "encode"
	}
	for _, path := range strings.Split(list, "|") {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		keys, err := matroska.Keyframes(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
			"-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		end, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)

		for _, asked := range []float64{0, 1234.5, 3000} {
			// What the server does: a resume is moved just past its keyframe.
			startAt := 0.0
			if asked > 0 {
				_, seek, ok := CopyStart(keys, asked)
				if !ok {
					t.Fatalf("%s: no start for %v", path, asked)
				}
				startAt = seek
			}
			cuts := KeyframeCuts(keys, startAt, SegmentSeconds)
			want, ok := KeyframePlaylist(cuts, end, "", probe.SegmentsMPEGTS)
			if !ok {
				t.Fatalf("%s at %v: no playlist", path, startAt)
			}

			dir := t.TempDir()
			d := probe.Decision{
				Method: probe.Remux, VideoAction: "copy", AudioAction: audioAction,
				TargetFormat: "mp4", Segments: probe.SegmentsMPEGTS, SourceContainer: "matroska",
			}
			o := Options{Input: path, Output: HLS, Decision: d, OutputDir: filepath.ToSlash(dir), StartAt: startAt, AudioIndex: -1}
			if startAt > 0 {
				o.AudioStartAt = startAt
			}
			args := Args(o)
			if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: ffmpeg: %v\n%s", path, err, b)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
			if err != nil {
				t.Fatal(err)
			}
			got := extinfs(string(raw))
			exp := extinfs(want)
			t.Logf("%s from %.3fs: predicted %d segments, ffmpeg wrote %d", filepath.Base(path), startAt, len(exp), len(got))
			if len(got) != len(exp) {
				t.Errorf("%s from %.3fs: predicted %d segments, ffmpeg wrote %d", path, startAt, len(exp), len(got))
				continue
			}
			for i := range got {
				tol := 0.01
				if i == len(got)-1 {
					tol = 0.5 // the container's length against the last packet's
				}
				if math.Abs(got[i]-exp[i]) > tol {
					t.Errorf("%s from %.3fs: segment %d is %.3fs, predicted %.3fs", path, startAt, i, got[i], exp[i])
					break
				}
			}
		}
	}
}

func extinfs(playlist string) []float64 {
	var d []float64
	for _, line := range strings.Split(playlist, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "#EXTINF:"); ok {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(v, ","), 64)
			d = append(d, f)
		}
	}
	return d
}
