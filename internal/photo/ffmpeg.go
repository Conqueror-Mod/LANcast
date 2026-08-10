package photo

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"lancast/internal/childproc"
)

// FFmpeg converts the formats Go cannot decode — today HEIC and HEIF — into PNG
// bytes the rest of this package handles normally.
//
// The same split the probe package keeps: process execution lives here, alone,
// so every decision made about a decoded image is testable without it.
type FFmpeg struct {
	// Path is the configured media-tools binary, set from settings the same way
	// the cover-art extractor's is. Empty falls back to PATH.
	Path    string
	Timeout time.Duration
}

func NewFFmpeg() *FFmpeg { return &FFmpeg{Timeout: 30 * time.Second} }

// Available reports whether ffmpeg can be found. False is survivable: HEIC
// files are still scanned and listed, and the worker records why they have no
// thumbnail rather than hiding them.
func (f *FFmpeg) Available() bool {
	_, err := f.binary()
	return err == nil
}

func (f *FFmpeg) binary() (string, error) {
	if f.Path != "" {
		return f.Path, nil
	}
	return exec.LookPath("ffmpeg")
}

// ToPNG decodes one picture to PNG on stdout.
//
// PNG rather than JPEG because this is an intermediate: the caller may rotate
// and will certainly resize, and starting that chain from a lossy re-encode
// throws away quality for no gain. The one JPEG encode happens at the end.
func (f *FFmpeg) ToPNG(ctx context.Context, path string) ([]byte, error) {
	bin, err := f.binary()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// -frames:v 1 because a HEIC can be a burst or a live photo, and only the
	// still is wanted. -an drops any audio track a live photo carries.
	cmd := exec.CommandContext(ctx, bin,
		"-hide_banner", "-loglevel", "error",
		"-i", path,
		"-frames:v", "1", "-an",
		"-f", "image2pipe", "-vcodec", "png", "-")
	childproc.Hide(cmd)

	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, bytes.TrimSpace(errb.Bytes()))
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced no image")
	}
	return out.Bytes(), nil
}
