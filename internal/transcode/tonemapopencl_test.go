package transcode

import (
	"strings"
	"testing"
)

/*
 * HDR tone mapped on the GPU when the running server proved it can be
 * (ADR 0033, 2026-09-10 amendment).
 *
 * Measured on a 3840x1606 HDR10 film with the server's own chain: the CPU tone
 * map ran at 0.41x realtime — slower than the film plays, so it stuttered and
 * every audio change waited on a slow start — and tonemap_opencl at 3.86x.
 *
 * These hold the argument side, which is pure. Whether OpenCL works in the
 * service's session 0 is decided by a real probe at startup and can only be
 * verified as the installed service; nothing here can stand in for that.
 */

func openclHDRArgs(gpu bool) []string {
	return Args(Options{
		Input: "in.mkv", Output: Progressive, Decision: encodeHDR(), AudioIndex: -1,
		CanTonemap: true, CanTagSDR: true, CanTonemapOpenCL: gpu,
	})
}

func TestHDRUsesTheGPUToneMapWhenProven(t *testing.T) {
	args := openclHDRArgs(true)
	vf := argValue(args, "-vf")
	if !strings.Contains(vf, "tonemap_opencl=tonemap=hable:desat=0:t=bt709:m=bt709:p=bt709:peak=10:format=nv12") {
		t.Errorf("-vf %q does not tone map on the GPU", vf)
	}
	// Not both: a CPU tone map after a GPU one would spend the whole cost again.
	// Matched per filter, because the OpenCL filter's own options contain the
	// text "tonemap=hable" and a substring check reports it as the CPU one.
	for _, f := range strings.Split(vf, ",") {
		if strings.HasPrefix(f, "zscale") || strings.HasPrefix(f, "tonemap=") {
			t.Errorf("-vf %q still carries the CPU tone map filter %q", vf, f)
		}
	}
	if !hasSequence(args, "-init_hw_device", "opencl=ocl") || !hasSequence(args, "-filter_hw_device", "ocl") {
		t.Error("the OpenCL device the filter needs is never created")
	}
}

// The device is a global option and has to exist before the input is opened.
func TestTheOpenCLDeviceComesBeforeTheInput(t *testing.T) {
	args := openclHDRArgs(true)
	dev, in := argIndex(args, "-init_hw_device"), argIndex(args, "-i")
	if dev < 0 || in < 0 || dev > in {
		t.Errorf("-init_hw_device at %d, -i at %d; the device must be created first", dev, in)
	}
}

/*
 * A server that could not prove OpenCL keeps today's chain exactly.
 *
 * This is the half that keeps a failed probe from costing anything but speed:
 * no device is created, so a session-0 service without OpenCL never asks for
 * one and cannot fail opening it.
 */
func TestWithoutOpenCLTheCPUChainIsUnchanged(t *testing.T) {
	args := openclHDRArgs(false)
	vf := argValue(args, "-vf")
	if strings.Contains(vf, "opencl") || strings.Contains(vf, "hwupload") {
		t.Errorf("-vf %q uses the GPU without it being proven", vf)
	}
	if !strings.Contains(vf, "tonemap=hable:desat=0") {
		t.Errorf("-vf %q lost the CPU tone map", vf)
	}
	if argIndex(args, "-init_hw_device") >= 0 {
		t.Error("an OpenCL device is created on a server that could not prove one works")
	}
}

// The GPU chain is as honest about its output as the CPU one: both halves of
// ADR 0033 must survive the move.
func TestTheGPUToneMapIsStillTaggedSDR(t *testing.T) {
	if !hasColourTags(openclHDRArgs(true)) {
		t.Error("a GPU tone mapped output is not tagged bt709")
	}
}

// SDR sources never touch OpenCL, proven or not.
func TestSDRNeverCreatesAnOpenCLDevice(t *testing.T) {
	d := encodeHDR()
	d.TonemapHDR = false
	args := Args(Options{
		Input: "in.mkv", Output: Progressive, Decision: d, AudioIndex: -1,
		CanTonemap: true, CanTagSDR: true, CanTonemapOpenCL: true,
	})
	if argIndex(args, "-init_hw_device") >= 0 {
		t.Error("an SDR file creates an OpenCL device it never uses")
	}
	if vf := argValue(args, "-vf"); strings.Contains(vf, "opencl") {
		t.Errorf("-vf %q tone maps an SDR file", vf)
	}
}

/*
 * A quality ceiling scales after a GPU tone map, and still in one -vf.
 *
 * There is no OpenCL scaler in the builds this runs on, and scaling the 10-bit
 * 4K frame on the CPU first would spend the cost the move exists to avoid — so
 * the downloaded 8-bit frame is scaled instead.
 */
func TestACeilingScalesAfterTheGPUToneMap(t *testing.T) {
	d := encodeHDR()
	d.TargetHeight = 1080
	args := Args(Options{
		Input: "in.mkv", Output: Progressive, Decision: d, AudioIndex: -1,
		CanTonemap: true, CanTagSDR: true, CanTonemapOpenCL: true,
	})
	vf := argValue(args, "-vf")
	tone, scale := strings.Index(vf, "tonemap_opencl="), strings.Index(vf, "scale=-2:1080")
	if tone < 0 || scale < 0 {
		t.Fatalf("-vf %q lost the tone map or the ceiling", vf)
	}
	if scale < tone {
		t.Errorf("-vf %q scales the 10-bit frame on the CPU before the GPU tone map", vf)
	}
	if strings.Count(strings.Join(args, " "), "-vf") != 1 {
		t.Error("more than one -vf: ffmpeg keeps only the last and the ceiling would be lost")
	}
}

/*
 * The startup probe runs the exact chain jobs will use.
 *
 * A probe that tested something easier than the real filter would pass in
 * session 0 and leave every HDR film to fail at playback — the shape of the
 * v0.8.0 regression. So its filter must be the jobs' filter, verbatim, on a
 * frame tagged PQ so the conversion is actually exercised.
 */
func TestTheProbeRunsTheRealChain(t *testing.T) {
	args := openclProbeArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, strings.Join(openclTonemapFilters, ",")) {
		t.Errorf("probe does not run the jobs' GPU chain: %v", args)
	}
	if !hasSequence(args, "-init_hw_device", "opencl=ocl") {
		t.Error("probe never creates the device, so it cannot find out whether one exists")
	}
	if !strings.Contains(joined, "color_trc=smpte2084") {
		t.Error("probe frame is not tagged PQ, so the tone map is not exercised")
	}
	if argIndex(args, "-init_hw_device") > argIndex(args, "-i") {
		t.Error("probe creates the device after opening the input")
	}
}
