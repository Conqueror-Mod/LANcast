package subtitle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

	// Timeout bounds one extraction. A damaged track can otherwise hang.
	Timeout time.Duration

	mu       sync.Mutex
	inFlight map[string]*sync.Mutex
}

// NewExtractor builds an extractor caching under dir.
func NewExtractor(dir string) *Extractor {
	e := &Extractor{
		cache:    dir,
		Timeout:  60 * time.Second,
		inFlight: map[string]*sync.Mutex{},
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

	key := cacheKey(videoPath, "embedded-"+strconv.Itoa(streamIndex))
	return e.cached(ctx, key, func(ctx context.Context) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, e.Timeout)
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
	key := cacheKey(path, "sidecar")

	return e.cached(ctx, key, func(ctx context.Context) ([]byte, error) {
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

// cached runs produce once per key, storing the result on disk.
//
// A subtitle track is requested repeatedly as a player reloads or a viewer
// toggles tracks; re-running ffmpeg each time would be slow and pointless.
func (e *Extractor) cached(ctx context.Context, key string, produce func(context.Context) ([]byte, error)) ([]byte, error) {
	path := filepath.Join(e.cache, key[:2], key+".vtt")

	if body, err := os.ReadFile(path); err == nil && len(body) > 0 {
		return body, nil
	}

	// One extraction per key at a time; concurrent requests for the same track
	// would otherwise each spawn ffmpeg.
	lock := e.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if body, err := os.ReadFile(path); err == nil && len(body) > 0 {
		return body, nil
	}

	body, err := produce(ctx)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		tmp, tmpErr := os.CreateTemp(filepath.Dir(path), ".tmp-*")
		if tmpErr == nil {
			name := tmp.Name()
			_, writeErr := tmp.Write(body)
			tmp.Close()
			if writeErr == nil {
				// A cache write failure must not fail the request; the caller
				// already has the bytes.
				_ = os.Rename(name, path)
			} else {
				os.Remove(name)
			}
		}
	}
	return body, nil
}

func (e *Extractor) lockFor(key string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	if l, ok := e.inFlight[key]; ok {
		return l
	}
	l := &sync.Mutex{}
	e.inFlight[key] = l
	return l
}

// cacheKey derives a stable filename from a source path and a variant.
func cacheKey(path, variant string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + variant))
	return hex.EncodeToString(sum[:16])
}
