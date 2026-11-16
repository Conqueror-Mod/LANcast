package subtitle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"lancast/internal/childproc"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ErrNotInstalled is returned when ffmpeg is needed and missing.
var ErrNotInstalled = errors.New("ffmpeg not found on PATH")

// ErrUnsupported is returned for tracks that cannot become WebVTT.
var ErrUnsupported = errors.New("subtitle format cannot be converted to WebVTT")

// Extractor turns embedded and sidecar subtitles into WebVTT, caching results.
type Extractor struct {
	bin   string
	cache string

	// Timeout bounds one extraction of a small file. A damaged track can
	// otherwise hang. A big file is given longer (extractTimeout), because
	// extracting a track reads the whole container.
	Timeout time.Duration

	mu   sync.Mutex
	jobs map[string]*job // in flight, by cache path
}

// job is one extraction running in the background, shared by every request
// that wants its result while it runs.
type job struct {
	done chan struct{}
	body []byte
	err  error
}

// NewExtractor builds an extractor caching under dir.
func NewExtractor(dir string) *Extractor {
	e := &Extractor{
		cache:   dir,
		Timeout: 60 * time.Second,
		jobs:    map[string]*job{},
	}
	if bin, err := exec.LookPath("ffmpeg"); err == nil {
		e.bin = bin
	}
	return e
}

// Available reports whether embedded extraction is possible. Sidecar .srt
// conversion works regardless, since that path is pure Go.
func (e *Extractor) Available() bool { return e.bin != "" }

// Embedded extracts one subtitle stream from a container as WebVTT.
func (e *Extractor) Embedded(ctx context.Context, videoPath string, streamIndex int, codec string) ([]byte, error) {
	if ClassifyCodec(codec) != Text {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, codec)
	}
	if !e.Available() {
		return nil, ErrNotInstalled
	}

	return e.cached(ctx, videoPath, "embedded-"+strconv.Itoa(streamIndex), func(ctx context.Context) ([]byte, error) {
		var size int64
		if info, err := os.Stat(videoPath); err == nil {
			size = info.Size()
		}
		ctx, cancel := context.WithTimeout(ctx, extractTimeout(e.Timeout, size))
		defer cancel()

		cmd := exec.CommandContext(ctx, e.bin,
			"-hide_banner", "-loglevel", "error", "-nostdin",
			"-i", videoPath,
			// Absolute stream index, not the nth subtitle: probe reports
			// absolute indices and translating between the two is a reliable
			// source of off-by-one track selection.
			"-map", "0:"+strconv.Itoa(streamIndex),
			"-c:s", "webvtt",
			"-f", "webvtt",
			"pipe:1",
		)
		childproc.Hide(cmd)
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return nil, fmt.Errorf("extract subtitle %d: %s", streamIndex, exitErr.Stderr)
			}
			return nil, fmt.Errorf("extract subtitle %d: %w", streamIndex, err)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("subtitle stream %d produced no cues", streamIndex)
		}
		return out, nil
	})
}

// Sidecar converts an external subtitle file to WebVTT.
func (e *Extractor) Sidecar(ctx context.Context, path, format string) ([]byte, error) {
	return e.cached(ctx, path, "sidecar", func(ctx context.Context) ([]byte, error) {
		switch format {
		case "srt", "sub":
			f, err := os.Open(path)
			if err != nil {
				return nil, fmt.Errorf("open subtitle: %w", err)
			}
			defer f.Close()

			head := make([]byte, 512)
			n, _ := f.Read(head)
			if !LooksLikeText(head[:n]) {
				return nil, fmt.Errorf("%w: %s is not text", ErrUnsupported, filepath.Base(path))
			}
			if _, err := f.Seek(0, 0); err != nil {
				return nil, err
			}
			return SRTToVTT(f)

		case "vtt":
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read subtitle: %w", err)
			}
			// Files named .vtt are often SRT with the extension changed, and
			// a missing header makes a browser drop the track silently.
			return EnsureVTTHeader(body), nil

		case "ass", "ssa":
			// ASS carries positioning and styling that has no pure-Go
			// converter worth writing; ffmpeg already does it well.
			if !e.Available() {
				return nil, ErrNotInstalled
			}
			ctx, cancel := context.WithTimeout(ctx, e.Timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, e.bin,
				"-hide_banner", "-loglevel", "error", "-nostdin",
				"-i", path, "-c:s", "webvtt", "-f", "webvtt", "pipe:1")
			childproc.Hide(cmd)
			out, err := cmd.Output()
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return nil, fmt.Errorf("convert subtitle: %s", exitErr.Stderr)
				}
				return nil, fmt.Errorf("convert subtitle: %w", err)
			}
			return out, nil

		default:
			return nil, fmt.Errorf("%w: %s", ErrUnsupported, format)
		}
	})
}

// cached runs produce once per source version, storing the result on disk.
//
// A subtitle track is requested repeatedly as a player reloads or a viewer
// toggles tracks; re-running ffmpeg each time would be slow and pointless.
//
// The entry is keyed on the source's size and modification time as well as
// its path (R09-5): a re-synced .srt, or a film replaced by another release
// under the same name, must not keep serving the conversion of the file that
// used to be there. Each source keeps one entry, so replacing a file does not
// leave the old conversion behind for ever.
//
// produce runs in the background, detached from the request that started it
// (R09-11). Extracting a track from a 50 GB remux reads all 50 GB, which is
// minutes; tied to the request, a player that gave up — or the old flat
// 60-second limit — killed it every time, nothing was cached, and the next
// attempt started again from nothing, so that film never had subtitles. Now a
// request that stops waiting leaves the extraction running, and it lands in
// the cache for the next one. Requests that arrive meanwhile wait on the same
// job rather than starting another ffmpeg.
func (e *Extractor) cached(ctx context.Context, source, variant string, produce func(context.Context) ([]byte, error)) ([]byte, error) {
	info, err := os.Stat(source)
	if err != nil {
		// Nothing to key a version on. produce reports the real error (a
		// missing file, a permission problem), and nothing is cached.
		return produce(ctx)
	}

	key := cacheKey(source, variant)
	dir := filepath.Join(e.cache, key[:2])
	path := filepath.Join(dir, key+"-"+versionStamp(info)+".vtt")

	if body, err := os.ReadFile(path); err == nil && len(body) > 0 {
		return body, nil
	}

	e.mu.Lock()
	j, running := e.jobs[path]
	if !running {
		// Looked at again under the lock: a job that finished since the
		// read above has written the file and left the table.
		if body, err := os.ReadFile(path); err == nil && len(body) > 0 {
			e.mu.Unlock()
			return body, nil
		}
		j = &job{done: make(chan struct{})}
		e.jobs[path] = j
		go e.run(context.WithoutCancel(ctx), j, dir, key, path, produce)
	}
	e.mu.Unlock()

	select {
	case <-j.done:
		return j.body, j.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// run is one background extraction: produce, store, then leave the table, in
// that order, so a request that misses the table finds the file.
func (e *Extractor) run(ctx context.Context, j *job, dir, key, path string, produce func(context.Context) ([]byte, error)) {
	defer close(j.done)
	j.body, j.err = produce(ctx)
	if j.err == nil {
		storeConversion(dir, key, path, j.body)
	}
	e.mu.Lock()
	delete(e.jobs, path)
	e.mu.Unlock()
}

// storeConversion writes one conversion to the cache. A failure is not an error: the
// caller already has the bytes.
func storeConversion(dir, key, path string, body []byte) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, writeErr := tmp.Write(body)
	tmp.Close()
	if writeErr != nil {
		os.Remove(name)
		return
	}
	if os.Rename(name, path) == nil {
		pruneOtherVersions(dir, key, path)
	} else {
		os.Remove(name)
	}
}

// extractMinRate is the slowest read a big file is allowed: 10 MB/s, well
// under a spinning disk or a gigabit share. A track that takes longer than its
// file's size at this rate is taken to be hung.
const extractMinRate = 10 << 20

// extractTimeout is how long one extraction of a file this size may run: the
// base for a small file, longer for one whose whole container takes longer
// than that to read (R09-11). A 50 GB remux gets about 85 minutes.
func extractTimeout(base time.Duration, size int64) time.Duration {
	if byRate := time.Duration(size/extractMinRate) * time.Second; byRate > base {
		return byRate
	}
	return base
}

// versionStamp names one version of a source file. Size catches most
// replacements on its own; the modification time catches an edit that keeps
// the length, such as a re-timed cue.
func versionStamp(info os.FileInfo) string {
	return strconv.FormatInt(info.Size(), 16) + "-" + strconv.FormatInt(info.ModTime().UnixNano(), 16)
}

// pruneOtherVersions removes the conversions of earlier versions of a source,
// so the cache holds one entry per source rather than one per edit. Entries
// from before versioned keys (named key+".vtt") go too. Failures are ignored:
// a file still open by a concurrent read on Windows is removed next time.
func pruneOtherVersions(dir, key, keep string) {
	matches, _ := filepath.Glob(filepath.Join(dir, key+"*.vtt"))
	for _, m := range matches {
		if m != keep {
			_ = os.Remove(m)
		}
	}
}

// cacheKey derives a stable filename prefix from a source path and a variant.
// The source's version is appended separately (see cached), so all versions of
// one source share this prefix and can be pruned together.
func cacheKey(path, variant string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + variant))
	return hex.EncodeToString(sum[:16])
}
