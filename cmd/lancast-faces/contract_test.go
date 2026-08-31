package main

import (
	"encoding/json"
	"strings"
	"testing"
)

/*
 * The worker's contract with the server (ADR 0052).
 *
 * The inference is not here yet, so these are about the two things that are:
 * that a build which cannot work says so precisely, and that the wire shape is
 * the one the server will parse. Both are cheap to get wrong and expensive to
 * discover later — a worker that reports readiness it does not have would have
 * the server queue every photograph in the library against nothing.
 */

func TestCapabilitiesAreNeverReadyWithoutAModel(t *testing.T) {
	c := caps("")
	if c.Ready {
		t.Error("reported ready with no model bundled — the server would queue " +
			"the whole library against a worker that cannot answer")
	}
	if c.Reason == "" {
		t.Error("not ready and would not say why; that cannot be told apart " +
			"from a missing worker, a wrong version, or a deleted model")
	}
}

// Version, OS and arch are how the server decides whether it is talking to a
// worker it understands, so they are never blank.
func TestCapabilitiesIdentifyTheBuild(t *testing.T) {
	c := caps("")
	for _, f := range []struct{ name, v string }{
		{"version", c.Version}, {"os", c.OS}, {"arch", c.Arch}, {"native", c.Native},
	} {
		if strings.TrimSpace(f.v) == "" {
			t.Errorf("%s is empty", f.name)
		}
	}
}

/*
 * The native line is the one fact a cross-compiled artefact cannot fake.
 *
 * A build that silently lost its cgo still compiles and still runs — it simply
 * never detects a face. So the two builds must describe themselves differently,
 * and CI greps this to prove the release artefact is the native one.
 */
func TestTheNativeLineDistinguishesTheTwoBuilds(t *testing.T) {
	got := nativeInfo()
	if hasCGO && strings.Contains(got, "without cgo") {
		t.Errorf("a cgo build described itself as %q", got)
	}
	if !hasCGO && !strings.Contains(got, "without cgo") {
		t.Errorf("a non-cgo build described itself as %q", got)
	}
}

// The wire shape, so a change to it is a decision rather than an accident: the
// server reads one JSON object per line and keys results by path.
func TestAResultRoundTripsAsOneLine(t *testing.T) {
	in := Result{
		Path:  `C:\pics\a.jpg`,
		Faces: []Face{{Path: `C:\pics\a.jpg`, X: 1, Y: 2, W: 3, H: 4, Score: 0.9, Embedding: []float32{0.1, -0.2}}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\n") {
		t.Error("a result contains a newline; the protocol is one object per line")
	}
	var out Result
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != in.Path || len(out.Faces) != 1 || out.Faces[0].W != 3 {
		t.Errorf("round trip lost data: %+v", out)
	}
}

// A photograph that cannot be read is reported per-path, not by failing the
// batch: a library will contain a truncated JPEG eventually, and stopping there
// means the pass never finishes.
func TestAFailedPhotographIsReportedNotFatal(t *testing.T) {
	b, err := json.Marshal(Result{Path: "bad.jpg", Error: "not an image"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"error"`) {
		t.Error("a per-path error has nowhere to go in the wire shape")
	}
	if strings.Contains(string(b), `"faces"`) {
		t.Error("an errored result carried an empty faces array, which reads as " +
			"a photograph with no faces in it")
	}
}
