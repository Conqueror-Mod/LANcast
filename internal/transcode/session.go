package transcode

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Session is one running ffmpeg process.
type Session struct {
	ID      string
	ItemID  int64
	Output  Output
	StartAt float64
	Dir     string // HLS only

	cmd    *exec.Cmd
	cancel context.CancelFunc
	stderr *ringBuffer

	mu        sync.Mutex
	started   time.Time
	lastTouch time.Time
	done      bool
	err       error
}

// Started reports when the session began.
func (s *Session) Started() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// Touch records activity so an in-use session is not reaped.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastTouch = time.Now()
}

// Idle reports how long since the session was last used.
func (s *Session) Idle() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastTouch)
}

// Done reports whether ffmpeg has exited, and why.
func (s *Session) Done() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done, s.err
}

// Stop kills ffmpeg and releases the session's scratch directory.
func (s *Session) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		// Wait briefly for the context cancellation to land before forcing it;
		// ffmpeg flushes its output on SIGTERM and a half-written segment is
		// worse than a slightly slower shutdown.
		done := make(chan struct{})
		go func() { s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = s.cmd.Process.Kill()
		}
	}
	if s.Dir != "" {
		_ = os.RemoveAll(s.Dir)
	}
}

// Stderr returns what ffmpeg complained about, for diagnostics.
func (s *Session) Stderr() string {
	if s.stderr == nil {
		return ""
	}
	return s.stderr.String()
}

// ringBuffer keeps the last N bytes written to it.
//
// ffmpeg on a damaged file can emit megabytes of errors. Keeping the tail
// bounded means a broken file cannot exhaust memory, and the tail is the part
// that says what actually went wrong.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer { return &ringBuffer{size: size} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(string(r.buf))
}

// startProgressive launches ffmpeg writing a fragmented MP4 to stdout.
func startProgressive(ctx context.Context, bin string, o Options) (*Session, io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(ctx, bin, Args(o)...)
	stderr := newRingBuffer(8 << 10)
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("transcode: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("transcode: start ffmpeg: %w", err)
	}

	s := &Session{
		Output: Progressive, StartAt: o.StartAt,
		cmd: cmd, cancel: cancel, stderr: stderr,
		started: time.Now(), lastTouch: time.Now(),
	}
	return s, stdout, nil
}

// startHLS launches ffmpeg writing segments into a scratch directory.
func startHLS(ctx context.Context, bin string, o Options) (*Session, error) {
	if err := os.MkdirAll(o.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("transcode: create output dir: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, bin, Args(o)...)
	stderr := newRingBuffer(8 << 10)
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		os.RemoveAll(o.OutputDir)
		return nil, fmt.Errorf("transcode: start ffmpeg: %w", err)
	}

	s := &Session{
		Output: HLS, StartAt: o.StartAt, Dir: o.OutputDir,
		cmd: cmd, cancel: cancel, stderr: stderr,
		started: time.Now(), lastTouch: time.Now(),
	}

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.done = true
		// A cancelled context is an ordinary stop, not a failure.
		if err != nil && ctx.Err() == nil {
			s.err = fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
		}
		s.mu.Unlock()
	}()

	return s, nil
}
