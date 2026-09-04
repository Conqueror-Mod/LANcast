package faces

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

/*
 * Finding the face worker (ADR 0052).
 *
 * `lancast-faces` is a separate binary and an optional download, exactly like
 * ffmpeg (ADR 0048): the server works without it, says so plainly, and never
 * pretends a library has no faces in it because the tool is missing. That last
 * distinction is the whole reason this file reports a *reason* rather than a
 * boolean — "no faces found" and "nothing looked" are different sentences, and
 * a UI that cannot tell them apart teaches people to distrust the feature.
 */

// Capabilities is what the worker binary reports about itself.
type Capabilities struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Native  string `json:"native"`
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason,omitempty"`

	/*
	 * Semantic search reports separately (ADR 0060).
	 *
	 * Two independent downloads: a household may have face grouping and not
	 * search, or the reverse. Folding them into one boolean would mean one
	 * feature reporting itself broken because the *other* is not installed —
	 * and the reason field exists in this file precisely so a UI can say which
	 * of several situations it is in rather than "no".
	 */
	SemanticReady  bool   `json:"semantic_ready"`
	SemanticReason string `json:"semantic_reason,omitempty"`
	// SemanticModel names the coordinate system the worker produces vectors
	// in. Stored beside every embedding and compared, so a swapped model is
	// something a pass notices rather than a library ranked against a mixture
	// of two spaces.
	SemanticModel string `json:"semantic_model,omitempty"`
}

// Tool locates and interrogates the worker binary.
type Tool struct {
	// Dir is where to look. Empty means beside this executable and then on
	// PATH — the two places a tool that shipped with LANcast or was installed
	// by hand actually lives.
	Dir string
	// ModelsDir holds the ONNX files. Separate from Dir because the models are
	// the large half of the download and a person may reasonably keep them on a
	// different disk from a 6MB binary.
	ModelsDir string
	// Runtime is the ONNX shared library. Optional only because the worker
	// falls back to ModelsDir, which is where the download puts it — *not*
	// because an empty value is safe to leave to the loader. On Windows 11 the
	// bare name resolves to the OS's own Windows ML copy in System32, which is
	// too old, so "let the loader find it" means binding to a different
	// library than the one that was downloaded and verified.
	Runtime string

	mu     sync.Mutex
	cached *Capabilities
	at     time.Time
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "lancast-faces.exe"
	}
	return "lancast-faces"
}

/*
 * Path resolves the binary, or reports that it is absent.
 *
 * Beside the server first. A downloaded worker sits next to the executable that
 * downloaded it, and preferring PATH would let a different copy — an older one
 * a developer built, say — win silently over the one this install manages.
 */
func (t *Tool) Path() (string, bool) {
	if t.Dir != "" {
		p := filepath.Join(t.Dir, exeName())
		if fileExists(p) {
			return p, true
		}
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), exeName())
		if fileExists(p) {
			return p, true
		}
	}
	if p, err := exec.LookPath(exeName()); err == nil {
		return p, true
	}
	return "", false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

/*
 * Capabilities asks the binary what it can do, and caches the answer briefly.
 *
 * Asking costs a process launch and a model load, which is not something to do
 * on every settings page render — but caching for ever would mean a person who
 * has just installed the models has to restart the server to be told so. A
 * minute is short enough to feel immediate and long enough to stop a polling
 * client launching a process a second.
 */
func (t *Tool) Capabilities(ctx context.Context) Capabilities {
	t.mu.Lock()
	if t.cached != nil && time.Since(t.at) < time.Minute {
		c := *t.cached
		t.mu.Unlock()
		return c
	}
	t.mu.Unlock()

	c := t.probe(ctx)

	t.mu.Lock()
	t.cached, t.at = &c, time.Now()
	t.mu.Unlock()
	return c
}

/*
 * Forget drops the cached answer, so the next caller asks the worker again.
 *
 * Capabilities are cached for a minute because probing spawns a process, and
 * for a minute that is exactly right — nothing about a worker changes on its
 * own. An install is the one moment it does, and it changes the *only* thing
 * this cache holds.
 *
 * Without this, finishing a 113MB download left the screen saying the runtime
 * could not be loaded for up to a minute afterwards — the answer from a probe
 * taken while the file was still arriving, which is the reading somebody is
 * most likely to be staring at, because they were watching the progress bar.
 */
func (t *Tool) Forget() {
	t.mu.Lock()
	t.cached, t.at = nil, time.Time{}
	t.mu.Unlock()
}

func (t *Tool) probe(ctx context.Context) Capabilities {
	path, ok := t.Path()
	if !ok {
		return Capabilities{Reason: "the face worker is not installed"}
	}
	args := []string{"capabilities"}
	if t.ModelsDir != "" {
		args = append(args, "-models", t.ModelsDir)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = t.env()
	out, err := cmd.Output()
	if err != nil {
		return Capabilities{Reason: "the face worker did not answer: " + err.Error()}
	}
	var c Capabilities
	if err := json.Unmarshal(out, &c); err != nil {
		return Capabilities{Reason: "the face worker gave an answer this server could not read"}
	}
	return c
}

// env passes the runtime location down, and nothing else. The worker is a
// subprocess of a media server: it has no business inheriting an environment
// full of API keys.
func (t *Tool) env() []string {
	env := os.Environ()
	if t.Runtime != "" {
		env = append(env, "LANCAST_ONNXRUNTIME="+t.Runtime)
	}
	return env
}

// Available is the short question, for callers that only need to decide whether
// to offer the feature at all.
func (t *Tool) Available(ctx context.Context) bool {
	return t.Capabilities(ctx).Ready
}
