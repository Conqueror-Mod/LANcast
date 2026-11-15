package faces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"lancast/internal/childproc"
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
	// Guard, when set, makes this the service's lookup (R10-5, R12-1): the
	// worker only from beside the server's own executable, and the #1096
	// guard over it, its folder, and the ONNX runtime it loads, before every
	// start. Nil for a server a person started, which runs the worker with
	// that person's own rights.
	Guard Guard

	mu     sync.Mutex
	cached *Capabilities
	at     time.Time
	// inflight is non-nil while a probe is running, and is closed when it
	// finishes. It is what stops six callers becoming six subprocesses.
	inflight chan struct{}
	// gen rises whenever Forget is called, so a probe that was already running
	// when the models changed does not get to write its answer down.
	gen int

	// The warm text embedder (ADR 0060, amended). Lazily created, because most
	// servers never search photographs at all.
	textOnce sync.Once
	textEmb  *textEmbedder
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "lancast-faces.exe"
	}
	return "lancast-faces"
}

// Guard decides whether a folder, and a file in it, may be run by this
// process: mediatools.ServiceGuard as the service. Declared here so this
// package does not import the tools package for one interface.
type Guard interface {
	CheckDir(dir string) error
	CheckFile(path string) error
}

// errNotInstalled is a worker that is simply not there, as opposed to one
// that was found and refused.
var errNotInstalled = errors.New("the face worker is not installed")

/*
 * Path resolves the binary, or reports that it is absent or refused. Every
 * caller asks it immediately before starting the worker, so the guard runs
 * before every start.
 */
func (t *Tool) Path() (string, bool) {
	p, err := t.resolve()
	return p, err == nil
}

func (t *Tool) resolve() (string, error) {
	if t.Guard != nil {
		return t.guarded()
	}
	// A server a person started: the faces folder first, so a worker
	// installed by hand beside its models is found; then beside the server,
	// where the installer puts it; then PATH, for a developer's build.
	if t.Dir != "" {
		p := filepath.Join(t.Dir, exeName())
		if fileExists(p) {
			return p, nil
		}
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), exeName())
		if fileExists(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath(exeName()); err == nil {
		return p, nil
	}
	return "", errNotInstalled
}

/*
 * guarded is the service's lookup.
 *
 * The service runs as LocalSystem, so the worker it starts runs as SYSTEM.
 * It used to look in <data dir>\faces first, which inherited ProgramData's
 * grant letting every local account add files: a planted lancast-faces.exe
 * there was run as SYSTEM the next time anything asked the worker a question,
 * and opening Settings › Photos does. PATH was the third place, which is
 * R09-1's first case again.
 *
 * So only beside the server's own executable, where the installer puts it
 * (Program Files; the download carries models and the runtime, never the
 * worker). And the guard, every time, over the folder and the binary, and over
 * the runtime's folder and the runtime: the worker loads onnxruntime.dll from
 * the faces folder, and a DLL it loads runs as SYSTEM just the same.
 */
func (t *Tool) guarded() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", errNotInstalled
	}
	dir := filepath.Dir(self)
	p := filepath.Join(dir, exeName())
	if !fileExists(p) {
		return "", errNotInstalled
	}
	if err := t.Guard.CheckDir(dir); err != nil {
		return "", fmt.Errorf("the face worker's folder was refused: %w", err)
	}
	if err := t.Guard.CheckFile(p); err != nil {
		return "", fmt.Errorf("the face worker was refused: %w", err)
	}
	// The runtime's folder whether or not the runtime is in it yet: a folder
	// others can add to is where a missing DLL gets planted.
	dirs := []string{t.ModelsDir}
	if rd := runtimeDir(t.Runtime); rd != "" && filepath.Clean(rd) != filepath.Clean(t.ModelsDir) {
		dirs = append(dirs, rd)
	}
	for _, d := range dirs {
		if d != "" && dirExists(d) {
			if err := t.Guard.CheckDir(d); err != nil {
				return "", fmt.Errorf("the face models' folder was refused: %w", err)
			}
		}
	}
	if t.Runtime != "" && fileExists(t.Runtime) {
		if err := t.Guard.CheckFile(t.Runtime); err != nil {
			return "", fmt.Errorf("the ONNX runtime was refused: %w", err)
		}
	}
	return p, nil
}

func runtimeDir(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
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
/*
 * How long a probe's answer is trusted, and how long it is *served* while a
 * fresh one is fetched behind it.
 *
 * These were one minute and no second value, which was fine while a probe was
 * two small face models. It is not fine now: since semantic search landed, the
 * worker also loads 600MB of CLIP to answer the question, and a probe measured
 * **2.6 seconds**. Every minute, whichever page asked first wore that.
 */
const (
	capsFresh = 10 * time.Minute
	capsStale = 24 * time.Hour
)

/*
 * Capabilities answers what the worker can do, from cache wherever it can.
 *
 * THIS SHIPPED BROKEN IN v0.9.0 AND IS WHY THE APP WENT SLUGGISH
 *
 * The old version released the lock, probed, and took it again. That is fine
 * for a cheap probe and ruinous for this one: `capabilities` now loads the CLIP
 * models, so it costs 2.6s and about 700MB, and **nothing was shared between
 * callers**. Opening a page that asks — Settings, People, the search screen —
 * spawned one 700MB subprocess *per request*. Six at once measured 6.6s and
 * several gigabytes of transient allocation, which is a machine that stops
 * answering, and the client reported it the only way it can: `Failed to fetch`,
 * on whichever requests happened to be in flight. Then it recurred every minute
 * when the cache expired.
 *
 * Two changes, and both matter.
 *
 * **One probe at a time.** A caller that finds a probe already running waits
 * for its answer instead of starting another. That is the difference between a
 * cost and a multiplier.
 *
 * **A stale answer is served while a fresh one is fetched.** Readiness changes
 * when somebody installs or removes models, and `Forget` is called on exactly
 * that event — so between those events the previous answer is not merely
 * probably right, it is right. Blocking a page load for 2.6s to re-confirm it
 * is a cost with nothing bought.
 *
 * Only a genuinely cold cache blocks, and it blocks once per server lifetime.
 */
func (t *Tool) Capabilities(ctx context.Context) Capabilities {
	t.mu.Lock()

	if t.cached != nil && time.Since(t.at) < capsFresh {
		c := *t.cached
		t.mu.Unlock()
		return c
	}

	if t.cached != nil && time.Since(t.at) < capsStale {
		// Stale but usable: answer now, refresh behind. The caller gets the
		// previous answer, which is the right one unless the models changed —
		// and if they changed, Forget already cleared this.
		c := *t.cached
		t.startProbeLocked()
		t.mu.Unlock()
		return c
	}

	// Cold, or so old it is worth waiting for. Share whatever probe is running.
	t.startProbeLocked()
	wait := t.inflight
	t.mu.Unlock()

	select {
	case <-wait:
	case <-ctx.Done():
		/*
		 * The caller gave up; the probe has not. It is still running for
		 * whoever else is waiting, and its answer will be cached — so a request
		 * that times out here does not also waste the work it started.
		 */
		return Capabilities{Reason: "still checking the photograph worker"}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cached != nil {
		return *t.cached
	}
	return Capabilities{Reason: "the photograph worker could not be checked"}
}

/*
 * startProbeLocked begins a probe unless one is already running. The caller
 * holds mu.
 *
 * The probe runs on context.Background with its own timeout, never on a
 * request's context. It is shared, so letting one caller's cancellation kill it
 * would abort work several other callers are waiting on — and on the request
 * that cancels, which is usually a page being navigated away from, that would
 * mean the next page starts the whole 2.6s again.
 */
func (t *Tool) startProbeLocked() {
	if t.inflight != nil {
		return
	}
	done := make(chan struct{})
	t.inflight = done

	gen := t.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c := t.probe(ctx)

		t.mu.Lock()
		/*
		 * Discarded if the models changed while this ran.
		 *
		 * An install finishing calls Forget, and a probe that started before it
		 * finished saw a half-downloaded directory. Storing that would cache
		 * "not installed" over a completed install for ten minutes — which is
		 * the report somebody watching a progress bar would be left reading,
		 * and the exact failure Forget exists to prevent.
		 */
		if t.gen == gen {
			t.cached, t.at = &c, time.Now()
		}
		t.inflight = nil
		t.mu.Unlock()
		close(done)
	}()
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
	t.gen++
	t.mu.Unlock()

	/*
	 * The warm text worker goes too, and this is the one call that must not be
	 * forgotten.
	 *
	 * Forget is called when the models on disk change — an install finishing is
	 * the only caller today. A worker started before that change holds the
	 * *previous* models in memory and would go on answering in their coordinate
	 * system, against a database that had just moved to another. Every search
	 * would rank, and sort, and be wrong, and report nothing.
	 */
	t.StopText()
}

func (t *Tool) probe(ctx context.Context) Capabilities {
	path, err := t.resolve()
	if err != nil {
		// A refusal says why, so Settings never shows "not installed" beside a
		// worker the person can see in Program Files.
		return Capabilities{Reason: err.Error()}
	}
	args := []string{"capabilities"}
	if t.ModelsDir != "" {
		args = append(args, "-models", t.ModelsDir)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = t.env()
	childproc.Hide(cmd)
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
