package transcode

import (
	"strings"
	"testing"

	"lancast/internal/probe"
)

func TestSoftwareEncoderArgs(t *testing.T) {
	args := Software.EncoderArgs(23)
	if !hasArgPair(args, "-c:v", "libx264") {
		t.Errorf("args = %v", args)
	}
	if !hasArgPair(args, "-crf", "23") {
		t.Error("software encoder does not use -crf")
	}
	// yuv420p, or a 10-bit source re-encodes to a profile browsers refuse.
	if !hasArgPair(args, "-pix_fmt", "yuv420p") {
		t.Error("no yuv420p on the software encoder")
	}
}

// Each encoder spells quality differently. Using -crf on NVENC is silently
// ignored, producing whatever default bitrate the driver picks.
func TestHardwareEncodersUseTheirOwnQualityFlag(t *testing.T) {
	tests := map[string]string{
		"h264_nvenc": "-cq",
		"h264_qsv":   "-global_quality",
		"h264_amf":   "-qp_i",
	}
	for name, flag := range tests {
		var enc Encoder
		for _, c := range candidates {
			if c.Name == name {
				enc = c
			}
		}
		if enc.Name == "" {
			t.Fatalf("candidate %s missing", name)
		}

		args := enc.EncoderArgs(23)
		if !hasArgPair(args, flag, "23") {
			t.Errorf("%s: args = %v, want %s 23", name, args, flag)
		}
		if hasArgPair(args, "-crf", "23") {
			t.Errorf("%s used -crf, which hardware encoders ignore", name)
		}
		// Hardware encoders default to profiles browsers refuse unless told.
		if !hasArgPair(args, "-profile:v", "high") {
			t.Errorf("%s does not state a profile", name)
		}
	}
}

// AMF ignores -qp_i unless constant QP rate control is requested.
func TestAMFSetsRateControl(t *testing.T) {
	var amf Encoder
	for _, c := range candidates {
		if c.Name == "h264_amf" {
			amf = c
		}
	}
	args := amf.EncoderArgs(23)
	if !hasArgPair(args, "-rc", "cqp") {
		t.Errorf("AMF args = %v, want constant QP rate control", args)
	}
}

func TestSelectEncoder(t *testing.T) {
	nvenc := candidates[0]
	available := []Encoder{nvenc, Software}

	if got := SelectEncoder(available, "auto", quiet()); got.Name != nvenc.Name {
		t.Errorf("auto = %s, want the fastest verified encoder", got.Name)
	}
	if got := SelectEncoder(available, "", quiet()); got.Name != nvenc.Name {
		t.Errorf("empty preference = %s, want auto behavior", got.Name)
	}
	if got := SelectEncoder(available, "off", quiet()); got.Name != "libx264" {
		t.Errorf("off = %s, want software", got.Name)
	}
	if got := SelectEncoder(available, "h264_nvenc", quiet()); got.Name != "h264_nvenc" {
		t.Errorf("explicit = %s", got.Name)
	}

	// Hardware can disappear between runs — a driver update, a moved disk.
	// Falling back beats refusing to transcode.
	if got := SelectEncoder(available, "h264_qsv", quiet()); got.Name != "libx264" {
		t.Errorf("unavailable encoder = %s, want a software fallback", got.Name)
	}
	if got := SelectEncoder(nil, "auto", quiet()); got.Name != "libx264" {
		t.Errorf("empty list = %s, want software", got.Name)
	}
}

// Detection must always offer software, so a machine with no usable hardware
// still transcodes.
func TestDetectWithoutFFmpegStillOffersSoftware(t *testing.T) {
	got := DetectEncoders(t.Context(), "", quiet())
	if len(got) != 1 || got[0].Name != "libx264" {
		t.Errorf("encoders = %+v, want software only", got)
	}
}

func TestArgsUseSelectedEncoder(t *testing.T) {
	nvenc := candidates[0]
	args := Args(Options{
		Input: "in.mkv", Output: Progressive, Encoder: nvenc,
		Decision: probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "encode"},
	})
	if !hasArgPair(args, "-c:v", "h264_nvenc") {
		t.Errorf("args = %v, want the selected encoder", args)
	}
	if strings.Contains(strings.Join(args, " "), "libx264") {
		t.Error("libx264 leaked into a hardware transcode")
	}
}

// A copy decision must not invoke any encoder — that is the whole point of the
// cheap path.
func TestEncoderIgnoredWhenCopying(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: Progressive, Encoder: candidates[0],
		Decision: probe.Decision{Method: probe.Remux, VideoAction: "copy", AudioAction: "copy"},
	})
	if !hasArgPair(args, "-c:v", "copy") {
		t.Errorf("args = %v, want a stream copy", args)
	}
	if strings.Contains(strings.Join(args, " "), "nvenc") {
		t.Error("a hardware encoder was invoked for a stream copy")
	}
}

func TestZeroEncoderDefaultsToSoftware(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: Progressive,
		Decision: probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "encode"},
	})
	if !hasArgPair(args, "-c:v", "libx264") {
		t.Errorf("args = %v, want the software default", args)
	}
}
