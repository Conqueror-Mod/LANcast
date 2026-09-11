package transcode

import (
	"context"
	"fmt"
	"io"
	"lancast/internal/childproc"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Session is one running ffmpeg process.
type Session struct {
	ID     string
	ItemID int64
	// Owner is the account this session was started for. It is what makes
	// "the same player asking again" distinguishable from "somebody else
	// watching the same film" — see Manager.Progressive.
	Owner   string
	Output  Output
	StartAt float64
	Dir     string // HLS only
	/*
	 * Encoding is whether either stream is genuinely being re-encoded, as
	 * opposed to copied into a different container.
	 *
	 * It exists because the two cost wildly different things and were being
	 * reported as the same thing. A remux is a few percent of one core; a
	 * transcode is most of one. The activity panel called every session
	 * "Transcoding for playback", so a live channel being copied — the
	 * overwhelmingly common case — announced itself as a transcode.
	 *
	 * That is not a cosmetic complaint. It sent this project's own Live TV
	 * investigation down the wrong path: the pacing fault was assumed to be an
	 * encoder failing to keep up, on the evidence of a badge, while the server
	 * log two lines away said `video=copy audio=copy`. A status that misreports
	 * what the machine is doing is worse than no status, because it is trusted.
	 */
	Encoding bool

	/*
	 * MediaSeconds and SegmentLength are set when the server lists this
	 * session's playlist whole, up front, instead of passing on ffmpeg's growing
	 * one — see CompletePlaylist for why, and completeFor for when. Zero means
	 * ffmpeg's playlist is served as it grows.
	 */
	MediaSeconds  float64
	SegmentLength float64

	cmd    *exec.Cmd
	cancel context.CancelFunc
	stderr *ringBuffer

	mu        sync.Mutex
	started   time.Time
	lastTouch time.Time
	done      bool
	err       error
	ended     bool
	served    int64
}

/*
 * NoteEnded records that ffmpeg finished on its own, rather than being stopped.
 *
 * Set from the reader when it sees EOF, because on the progressive path that is
 * the only place the truth is available: stdout is a `cmd.StdoutPipe()`, and
 * calling `Wait` on one before reads have completed is documented as incorrect
 * — it closes the pipe under the reader. So there is no exit-status goroutine
 * for a progressive session, and `Done` answers `(false, nil)` for its whole
 * life.
 *
 * EOF is a better signal than an exit status anyway, and race-free: ffmpeg's
 * stdout closes when ffmpeg exits, and nothing else closes it.
 */
func (s *Session) NoteEnded() {
	s.mu.Lock()
	s.ended = true
	s.mu.Unlock()
}

// EndedItself reports whether ffmpeg stopped of its own accord.
func (s *Session) EndedItself() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
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

/*
 * NoteServed records bytes handed to the client.
 *
 * It exists to tell two identical-looking log lines apart. Two progressive
 * starts on one item milliseconds apart are in the log and have never been
 * explained; `start_at` separated "reconnecting where it was" from
 * "re-requesting the same offset", and both halves of the pair say zero, so it
 * separates nothing here. What is left to ask is whether the superseded stream
 * ever *delivered* anything — a first request abandoned after no bytes is a
 * media stack sniffing the stream, and one abandoned after megabytes is a
 * player that genuinely asked twice. Those are different faults.
 */
func (s *Session) NoteServed(n int) {
	s.mu.Lock()
	s.served += int64(n)
	s.mu.Unlock()
}

// Served reports how many bytes this session handed to the client.
func (s *Session) Served() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.served
}

// Idle reports how long since the session was last used.
func (s *Session) Idle() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastTouch)
}

/*
 * IsLive reports whether this session is a channel rather than a library item.
 *
 * Channel ids are recorded negated in ItemID — the convention LiveHLS
 * establishes, so a session list cannot show a channel as though it were an
 * item, since the two numbering schemes overlap. Reading that convention
 * through a named method rather than at each call site keeps the sign from
 * becoming folklore.
 */
func (s *Session) IsLive() bool { return s.ItemID < 0 }

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
	childproc.Hide(cmd)
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
		Output: Progressive, StartAt: o.StartAt, Encoding: o.Decision.Encoding(),
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

	/*
	 * Detached from the caller's cancellation, deliberately.
	 *
	 * The caller is an HTTP handler, and Go cancels a request's context when
	 * the handler returns — which for the playlist route is the moment the
	 * playlist has been sent. Tied to that, ffmpeg was killed as every
	 * segmented session's playlist went out, having written one segment, and
	 * the wait below recorded it as an ordinary stop. Seen as the service on
	 * v0.9.13: `playlist=complete`, then 2.5s later "transcode finished without
	 * producing seg00001.m4s" with no reason.
	 *
	 * A segmented session is many requests long, so its lifetime belongs to the
	 * session: Stop (supersede, the reaper, StopAll) cancels it. A progressive
	 * session is different — it *is* its response — and keeps the request's.
	 */
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd := exec.CommandContext(ctx, bin, Args(o)...)
	childproc.Hide(cmd)
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
		Encoding: o.Decision.Encoding(),
		cmd:      cmd, cancel: cancel, stderr: stderr,
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
