package transcode

import (
	"context"
	"lancast/internal/childproc"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Encoder is one way to produce H.264.
type Encoder struct {
	// Name is the ffmpeg encoder, e.g. h264_nvenc.
	Name string `json:"name"`
	// Label is what a person reads.
	Label string `json:"label"`
	// Hardware is false only for libx264.
	Hardware bool `json:"hardware"`

	// qualityFlag differs per encoder: x264 uses -crf, NVENC -cq, QSV
	// -global_quality, AMF -qp_i/-qp_p. Same intent, four spellings.
	qualityFlag string
	presetFlag  string
	presetValue string
}

/*
 * decodeAccel is the -hwaccel this encoder's driver stack can be trusted with,
 * or "" for software decoding.
 *
 * It exists because `-hwaccel auto` shipped in v0.8.0 and broke HEVC playback
 * outright. `auto` chose DXVA2, DXVA2 needs a Direct3D device, and **LANcast
 * runs as a Windows service in session 0**, which has no desktop and no D3D. It
 * did not fall back — ffmpeg exited with "Failed to create Direct3D device"
 * before producing a byte, so the film span forever on a spinner.
 *
 * The lesson is in where it was tested. Every measurement behind that change
 * launched ffmpeg from an interactive user session, where DXVA2 works fine. The
 * one environment never exercised was the one that ships.
 *
 * So the decoder is tied to the encoder rather than guessed. The encoder is
 * already chosen by a real encode in this process — if NVENC ran here, the
 * NVIDIA driver stack is present and usable *in this session*, and CUDA/NVDEC
 * is the same stack with no D3D dependency. QSV and VideoToolbox are paired the
 * same way.
 *
 * AMF is deliberately left on software decode: its decode path on Windows is
 * D3D-backed, which is the thing that broke, and there is no AMD machine here
 * to prove otherwise on. An unverified guess is what caused this.
 */
func (e Encoder) decodeAccel() string {
	switch e.Name {
	case "h264_nvenc":
		return "cuda"
	case "h264_qsv":
		return "qsv"
	case "h264_videotoolbox":
		return "videotoolbox"
	default:
		return ""
	}
}

// Software is the always-available fallback.
var Software = Encoder{
	Name: "libx264", Label: "CPU (libx264)", Hardware: false,
	qualityFlag: "-crf", presetFlag: "-preset", presetValue: "veryfast",
}

// candidates are tried in order of typical throughput. Each is verified by a
// real encode before being offered.
var candidates = []Encoder{
	{Name: "h264_nvenc", Label: "NVIDIA NVENC", Hardware: true,
		qualityFlag: "-cq", presetFlag: "-preset", presetValue: "p4"},
	{Name: "h264_qsv", Label: "Intel Quick Sync", Hardware: true,
		qualityFlag: "-global_quality", presetFlag: "-preset", presetValue: "veryfast"},
	{Name: "h264_amf", Label: "AMD AMF", Hardware: true,
		qualityFlag: "-qp_i", presetFlag: "-quality", presetValue: "balanced"},
	{Name: "h264_videotoolbox", Label: "Apple VideoToolbox", Hardware: true,
		qualityFlag: "-q:v", presetFlag: "", presetValue: ""},
}

// EncoderArgs returns the codec flags for this encoder at the given quality.
func (e Encoder) EncoderArgs(quality int) []string {
	args := []string{"-c:v", e.Name}
	if e.presetFlag != "" {
		args = append(args, e.presetFlag, e.presetValue)
	}
	if e.qualityFlag != "" {
		args = append(args, e.qualityFlag, strconv.Itoa(quality))
	}

	// Force 8-bit 4:2:0 for every encoder. A 10-bit source — common in HEVC
	// Main10 — otherwise reaches an H.264 encoder that cannot accept 10-bit, and
	// the hardware encoders answer that with a black frame rather than an error.
	// There is no -hwaccel decode, so frames are in system memory and this is a
	// plain swscale conversion the encoder accepts; on an already-8-bit source it
	// is a no-op. Profile and level are stated explicitly for the same reason —
	// several hardware encoders default to settings browsers refuse.
	args = append(args, "-pix_fmt", "yuv420p", "-profile:v", "high", "-level", "4.1")

	// AMF defaults to a rate control that ignores -qp_i unless told to use
	// constant QP.
	if e.Name == "h264_amf" {
		args = append(args, "-rc", "cqp", "-qp_p", strconv.Itoa(quality))
	}

	return args
}

// DetectEncoders returns the usable encoders, best first, always ending with
// software.
//
// Each candidate is verified by encoding a handful of generated frames.
// ffmpeg lists encoders that the machine cannot actually run — h264_nvenc
// appears with no NVIDIA card present and fails at runtime — so a capability
// listing would produce a server that claims hardware acceleration and then
// fails every transcode.
func DetectEncoders(ctx context.Context, bin string, log *slog.Logger) []Encoder {
	if bin == "" {
		return []Encoder{Software}
	}

	listed := listEncoders(ctx, bin)
	out := make([]Encoder, 0, len(candidates)+1)

	for _, c := range candidates {
		if !listed[c.Name] {
			continue
		}
		if err := testEncode(ctx, bin, c); err != nil {
			log.Debug("hardware encoder unusable", "encoder", c.Name, "error", err)
			continue
		}
		log.Info("hardware encoder available", "encoder", c.Name, "label", c.Label)
		out = append(out, c)
	}

	return append(out, Software)
}

// listEncoders reads what ffmpeg was built with. Necessary but not sufficient.
func listEncoders(ctx context.Context, bin string) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-encoders")
	childproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	found := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			found[fields[1]] = true
		}
	}
	return found
}

// testEncode proves an encoder actually runs on this machine.
func testEncode(ctx context.Context, bin string, e Encoder) error {
	// A few generated frames: enough for the encoder to initialize hardware
	// and fail if it cannot, fast enough to run at every startup.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "lavfi", "-i", "testsrc=duration=0.2:size=320x240:rate=10",
	}
	args = append(args, e.EncoderArgs(23)...)
	args = append(args, "-f", "null", "-")

	cmd := exec.CommandContext(ctx, bin, args...)
	childproc.Hide(cmd)
	return cmd.Run()
}

// SelectEncoder picks from the detected list according to a preference.
//
// "auto" takes the fastest verified encoder; "off" forces software; a specific
// name is honored when available and falls back with a warning rather than
// failing, since hardware can disappear between runs.
func SelectEncoder(available []Encoder, preference string, log *slog.Logger) Encoder {
	if len(available) == 0 {
		return Software
	}

	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "", "auto":
		return available[0]
	case "off", "software", "cpu", "libx264":
		return Software
	default:
		for _, e := range available {
			if strings.EqualFold(e.Name, preference) {
				return e
			}
		}
		log.Warn("requested encoder is not usable on this machine; using software",
			"requested", preference)
		return Software
	}
}

/*
 * ColourCaps is what an ffmpeg build can do about HDR (ADR 0033).
 *
 * Two capabilities rather than one, because they fail independently and the
 * fallback matters. Tonemap needs `zscale` (libzimg) and `tonemap`, neither
 * guaranteed in a build LANcast did not choose (ADR 0016). TagSDR needs only
 * `setparams`, which is core libavfilter — so the relabel path survives in
 * builds where the conversion does not.
 */
type ColourCaps struct {
	// Tonemap: the full HDR-to-SDR conversion can run.
	Tonemap bool
	// TagSDR: frame colour properties can be relabelled without converting.
	// Required for the output tags to be coherent — see sdrRelabel.
	TagSDR bool
}

/*
 * DetectColourCaps probes what this ffmpeg can do about HDR.
 *
 * A filter listing is enough here, unlike the encoder probe above. That one
 * runs a real test encode because ffmpeg advertises encoders the machine cannot
 * run — h264_nvenc is listed with no NVIDIA card present and fails at playback
 * time. A filter has no hardware behind it to be absent, so being listed and
 * being usable are the same thing.
 */
func DetectColourCaps(ctx context.Context, bin string, log *slog.Logger) ColourCaps {
	if bin == "" {
		return ColourCaps{}
	}
	filters := listFilters(ctx, bin)
	caps := ColourCaps{
		Tonemap: filters["tonemap"] && filters["zscale"],
		TagSDR:  filters["setparams"],
	}

	// Worth saying once at startup rather than leaving someone to wonder why HDR
	// still looks flat, and worth distinguishing the two degraded states: one
	// ships a coherent SDR file, the other cannot and leaves the output as it
	// has always been.
	switch {
	case caps.Tonemap:
	case caps.TagSDR:
		log.Info("hdr tonemapping unavailable; HDR output will be labelled SDR but not converted",
			"zscale", filters["zscale"], "tonemap", filters["tonemap"])
	default:
		log.Warn("this ffmpeg can neither tone map nor relabel HDR; HDR output keeps the source's colour tags",
			"zscale", filters["zscale"], "tonemap", filters["tonemap"],
			"setparams", filters["setparams"])
	}
	return caps
}

// listFilters returns the filter names this ffmpeg advertises.
//
// `-filters` prints a flags column first, so the name is the second field —
// the same shape as `-encoders`, and parsed the same way.
func listFilters(ctx context.Context, bin string) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-filters")
	childproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	found := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			found[fields[1]] = true
		}
	}
	return found
}
