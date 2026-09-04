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

/*
 * Semantic readiness answers separately, and never silently (ADR 0060).
 *
 * Two independent downloads: a server may have face models, CLIP models,
 * either, or neither. One boolean covering both would mean face grouping
 * refusing to start because a *different* feature is missing, reported as
 * neither working.
 *
 * The no-reason case is the one worth the test. This block sat below the face
 * verdict's early returns at first, so `capabilities` with no model directory
 * answered `semantic_ready: false` with nothing beside it — the uninspectable
 * report the file's own comment objects to, and indistinguishable from a
 * missing worker, a wrong version, or a deleted model. Running the binary is
 * what found it.
 */
func TestSemanticReadinessAlwaysCarriesItsReason(t *testing.T) {
	for _, dir := range []string{"", "/nonexistent/models"} {
		c := caps(dir)
		if c.SemanticReady {
			t.Errorf("caps(%q) reported semantic search ready with no model", dir)
		}
		if c.SemanticReason == "" {
			t.Errorf("caps(%q) reported semantic_ready:false with no reason — "+
				"that cannot be told from a missing worker or a deleted model", dir)
		}
		if c.SemanticModel == "" {
			t.Errorf("caps(%q) did not name the model space; the server compares "+
				"it against what is stored", dir)
		}
	}
}

// The two readiness verdicts are independent, so neither may be derived from
// the other. A build with one set of models and not the other must say exactly
// that.
func TestTheTwoReadinessVerdictsAreIndependent(t *testing.T) {
	c := caps("")
	if c.Ready != c.SemanticReady {
		return // already distinct in this build; nothing to prove
	}
	// Both false here. What must not happen is one being *reported* from the
	// other's reason.
	if c.Reason != "" && c.Reason == c.SemanticReason {
		t.Error("both features reported the same reason; one is being derived " +
			"from the other rather than answered")
	}
}

// An Embedding survives the wire the way a Result does — the server reads these
// one line at a time as a pass produces them.
func TestAnEmbeddingRoundTripsAsOneLine(t *testing.T) {
	in := Embedding{Path: `W:\pics\a.jpg`, Vector: []float32{0.5, -0.5}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\n") {
		t.Error("an embedding encoded with a newline in it; the protocol is one per line")
	}
	var out Embedding
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != in.Path || len(out.Vector) != 2 || out.Vector[0] != 0.5 {
		t.Errorf("round trip changed the embedding: %+v", out)
	}
}

// A photograph that cannot be embedded is that photograph's error, not the
// batch's — the same rule detect follows, for the same reason.
func TestAFailedEmbeddingIsReportedNotFatal(t *testing.T) {
	b, err := json.Marshal(Embedding{Path: "bad.jpg", Error: "decode: unexpected EOF"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "error") {
		t.Error("an embedding failure did not survive encoding")
	}
	if strings.Contains(string(b), "vector") {
		t.Error("a failed embedding carried a vector field; an empty one would " +
			"be stored as though it meant something")
	}
}
