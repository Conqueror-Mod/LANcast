package marker

import "testing"

/*
 * One named case per rule, asserting the decision and why it was made.
 *
 * Every number in the fixtures below is from a real film in the samples ADR
 * 0054 records, because a rule tuned on real endings and tested against
 * invented ones is not tested.
 */

func TestParseShiftsTimesByTheSeekPoint(t *testing.T) {
	// ffmpeg reports relative to -ss. A run 100s into a scan that began at
	// 4,500s is at 4,600s in the film, and forgetting that is how a boundary
	// lands a quarter of the way through.
	out := ParseBlackDetect(
		"[blackdetect @ 0x1] black_start:100.5 black_end:112.3\n", 4500)
	if len(out) != 1 {
		t.Fatalf("got %d runs, want 1", len(out))
	}
	if out[0].Start != 4600.5 || out[0].End != 4612.3 {
		t.Errorf("run = %+v, want start 4600.5 end 4612.3", out[0])
	}
}

func TestParseIgnoresEverythingElseInStderr(t *testing.T) {
	noise := `ffmpeg version 7.1 Copyright (c) 2000-2024
  Stream #0:0: Video: h264, yuv420p, 1920x1080
[blackdetect @ 0x55] black_start:5011.2 black_end:5029.9
frame= 1200 fps=240 q=-0.0 size=N/A time=00:00:50.00
`
	if got := ParseBlackDetect(noise, 0); len(got) != 1 {
		t.Errorf("got %d runs, want 1 — only blackdetect lines are data", len(got))
	}
}

func TestParseSkipsAMalformedRun(t *testing.T) {
	// An end before its start is not a run. Dropping it beats propagating a
	// negative length into the length comparison.
	got := ParseBlackDetect("black_start:90.0 black_end:80.0\n", 0)
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

// FernGully: an 18.7s run at 93.0% is the credits, and it is the first
// qualifying one rather than the longest — a 31s run follows it at 94.1%.
func TestCreditsTakeTheEarliestQualifyingRun(t *testing.T) {
	dur := 4554.0
	runs := []Run{
		{Start: 4235.0, End: 4253.7}, // 93.0%, 18.7s
		{Start: 4282.0, End: 4313.0}, // 94.1%, 31.0s — longer, and later
	}
	got := CreditsFrom(runs, dur)
	if !got.Found {
		t.Fatal("Found = false, want the 93.0% run")
	}
	if got.StartMS != 4_235_000 {
		t.Errorf("StartMS = %d, want 4235000 — the earliest qualifying run", got.StartMS)
	}
	if got.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9 for a run over the preferred length", got.Confidence)
	}
}

// The Beastmaster: a 77.6% fade is a scene transition, and the rule that
// ignored the window picked it. The real boundary is at 93.9%.
func TestCreditsIgnoreAThirdActFadeToBlack(t *testing.T) {
	dur := 6000.0
	runs := []Run{
		{Start: 4656.0, End: 4670.0}, // 77.6% — a scene fade, and 14s long
		{Start: 5634.0, End: 5650.0}, // 93.9%
	}
	got := CreditsFrom(runs, dur)
	if !got.Found || got.StartMS != 5_634_000 {
		t.Errorf("got %+v, want the 93.9%% run — below 88%% is a scene fade", got)
	}
}

// The rule that took the last long run put 20 of 33 answers here.
func TestCreditsRefuseTheFileEnding(t *testing.T) {
	dur := 6000.0
	runs := []Run{{Start: 5994.0, End: 6000.0}} // 99.9%
	if got := CreditsFrom(runs, dur); got.Found {
		t.Errorf("got %+v, want no answer — that is the file running out", got)
	}
}

// At World's End: one black run in a 159-minute tail, at 99.9%. Its credits
// begin on a cut. This is the method's ceiling, and abstaining is correct.
func TestCreditsAbstainWhenTheCreditsBeginOnACut(t *testing.T) {
	dur := 9540.0
	runs := []Run{{Start: 9530.0, End: 9540.0}}
	if got := CreditsFrom(runs, dur); got.Found {
		t.Errorf("got %+v, want no answer", got)
	}
}

// Jackass 2.5: nothing in the window reaches even the fallback length.
func TestCreditsAbstainWhenNoRunIsLongEnough(t *testing.T) {
	dur := 3864.0
	runs := []Run{
		{Start: 3585.0, End: 3586.8}, // 92.8%, 1.8s — under the 2s fallback
		{Start: 3662.0, End: 3663.1}, // 94.8%, 1.1s
	}
	if got := CreditsFrom(runs, dur); got.Found {
		t.Errorf("got %+v, want no answer — 1.8s is under the fallback", got)
	}
}

// A short run inside the window is accepted only when no long one is, and it
// says so through its confidence rather than by looking identical.
func TestCreditsFallBackToAShorterRunAndSayItIsWeaker(t *testing.T) {
	dur := 6000.0
	runs := []Run{{Start: 5640.0, End: 5643.0}} // 94.0%, 3.0s
	got := CreditsFrom(runs, dur)
	if !got.Found || got.StartMS != 5_640_000 {
		t.Fatalf("got %+v, want the 94.0%% run", got)
	}
	if got.Confidence != 0.5 {
		t.Errorf("Confidence = %v, want 0.5 — this is the fallback tier", got.Confidence)
	}
}

// A confident run later in the window beats an earlier weak one: the tiers are
// tried in order, not merged into one pass ordered by time.
func TestCreditsPreferALongRunOverAnEarlierShortOne(t *testing.T) {
	dur := 6000.0
	runs := []Run{
		{Start: 5300.0, End: 5302.5}, // 88.3%, 2.5s
		{Start: 5640.0, End: 5652.0}, // 94.0%, 12.0s
	}
	got := CreditsFrom(runs, dur)
	if got.StartMS != 5_640_000 {
		t.Errorf("StartMS = %d, want the confident 94.0%% run", got.StartMS)
	}
}

// The Outsiders read against TMDB's runtime put its black frames at 120% of
// "its" length. A duration that is not the file's is not a fact about it.
func TestCreditsRefuseAnUnknownDuration(t *testing.T) {
	runs := []Run{{Start: 5000.0, End: 5020.0}}
	if got := CreditsFrom(runs, 0); got.Found {
		t.Errorf("got %+v, want no answer without a real duration", got)
	}
}

func TestScanFromLeavesMarginBelowTheWindow(t *testing.T) {
	// The window starts at 88%; scanning from 75% means an unusually long
	// credit roll is visible rather than assumed away.
	if got := ScanFrom(6000); got != 4500 {
		t.Errorf("ScanFrom(6000) = %v, want 4500", got)
	}
	if got := ScanFrom(6000) / 6000; got >= WindowLo {
		t.Errorf("scan starts at %.2f, which is not below the window at %.2f", got, WindowLo)
	}
}
