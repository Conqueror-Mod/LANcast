package matroska

import (
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

/*
TestKeyframesMatchFFprobe checks the index against the keyframes ffprobe finds
by reading every packet, on real files. Set LANCAST_MKV to a list of paths
separated by "|"; it needs ffprobe on the PATH.
*/
func TestKeyframesMatchFFprobe(t *testing.T) {
	list := os.Getenv("LANCAST_MKV")
	if list == "" {
		t.Skip("LANCAST_MKV not set")
	}
	for _, path := range strings.Split(list, "|") {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Keyframes(f)
		f.Close()
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
			"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var want []float64
		for _, line := range strings.Split(string(out), "\n") {
			p := strings.Split(strings.TrimSpace(line), ",")
			if len(p) < 2 || !strings.Contains(p[1], "K") {
				continue
			}
			v, err := strconv.ParseFloat(p[0], 64)
			if err == nil {
				want = append(want, v)
			}
		}
		missing := 0
		j := 0
		for _, w := range want {
			for j < len(got) && got[j] < w-0.0005 {
				j++
			}
			if j >= len(got) || math.Abs(got[j]-w) > 0.0005 {
				missing++
			}
		}
		t.Logf("%s: index %d keyframes, packets %d, packet keyframes not in the index %d",
			path, len(got), len(want), missing)
		if missing > 0 || len(got) != len(want) {
			t.Errorf("%s: the index is not every keyframe", path)
		}
	}
}
