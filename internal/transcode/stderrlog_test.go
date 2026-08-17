package transcode

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// safeBuf collects log output from whichever goroutine ffmpeg's stderr lands on.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func loggingManager(t *testing.T, bin string) (*Manager, *safeBuf) {
	t.Helper()
	out := &safeBuf{}
	m := NewManager(t.TempDir(), slog.New(slog.NewTextHandler(out, nil)))
	m.bin = bin
	return m, out
}

/*
 * A stand-in for ffmpeg that runs on every platform.
 *
 * The shell-script fake these tests would otherwise use skips on Windows, which
 * means it never runs on the machine this project is developed on — a test that
 * only executes in CI is one whose failure is found late and by somebody else.
 * Compiling a two-line Go program costs a second and runs anywhere Go does.
 */
func goFakeFFmpeg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	prog := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n" + body + "\n}\n"
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fakeffmpeg")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the fake: %v\n%s", err, out)
	}
	return bin
}

/*
 * What ffmpeg complained about has to reach the log.
 *
 * It was captured all along — every session hands ffmpeg a bounded ring buffer —
 * and nothing ever read it for a stream. A channel that failed left one line,
 * "live transcode started", and no reason; the browser's side of the story was
 * `DEMUXER_ERROR_COULD_NOT_OPEN`, which says only that what arrived could not be
 * opened. The half that knew *why* was sitting in memory until the process died.
 *
 * Found while diagnosing a live channel that would not play, where the absence
 * of this line was the reason the diagnosis stalled.
 */
func TestFFmpegStderrReachesTheLog(t *testing.T) {
	bin := goFakeFFmpeg(t, "\tfmt.Fprintln(os.Stderr, \"Malformed AAC bitstream detected\")\n\tos.Exit(1)")
	m, out := loggingManager(t, bin)

	stream, err := m.Live(context.Background(), 42, LiveOptions{
		URL: "http://example.invalid/channel.ts", Decision: remux(),
	})
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	_, _ = io.Copy(io.Discard, stream)
	stream.Close()

	logged := out.String()
	if !strings.Contains(logged, "Malformed AAC bitstream") {
		t.Errorf("ffmpeg's complaint never reached the log.\ngot: %s", logged)
	}
	if !strings.Contains(logged, "ffmpeg reported errors") {
		t.Errorf("no diagnostic line was emitted.\ngot: %s", logged)
	}
}

// A stream that ends cleanly says nothing. ffmpeg runs at -loglevel error, and a
// viewer closing a tab is not an error — a line every time a channel is switched
// would bury the ones that matter.
func TestACleanSessionLogsNoErrors(t *testing.T) {
	bin := goFakeFFmpeg(t, "\tfmt.Print(\"some bytes\")\n\tos.Exit(0)")
	m, out := loggingManager(t, bin)

	stream, err := m.Live(context.Background(), 7, LiveOptions{
		URL: "http://example.invalid/channel.ts", Decision: remux(),
	})
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	_, _ = io.Copy(io.Discard, stream)
	stream.Close()

	if strings.Contains(out.String(), "ffmpeg reported errors") {
		t.Errorf("a healthy stream logged an error line.\ngot: %s", out.String())
	}
}
