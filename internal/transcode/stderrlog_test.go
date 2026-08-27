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
	return goFakeFFmpegImporting(t, []string{"fmt", "os"}, body)
}

// goFakeFFmpegImporting is the same fake for a body needing more than fmt and
// os — a stand-in that has to stay alive needs "time", and an unused import is
// a compile error.
func goFakeFFmpegImporting(t *testing.T, imports []string, body string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	var imp strings.Builder
	for _, i := range imports {
		imp.WriteString("\t\"" + i + "\"\n")
	}
	prog := "package main\n\nimport (\n" + imp.String() + ")\n\nfunc main() {\n" + body + "\n}\n"
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

/*
 * Killing a running ffmpeg is not a failure, and must not be logged as one.
 *
 * The comment on reportStderr used to assert that "being killed is not an error
 * ffmpeg reports". Watching it disproved that: a killed ffmpeg reliably writes
 * `Error submitting a packet to the muxer: Broken pipe`, and on the live fMP4
 * path a muxer error with a PATCHWELCOME return code. So **every ordinary
 * channel stop** logged `WARN "ffmpeg reported errors"` under a wall of
 * alarming text.
 *
 * It cost real time: during an investigation into a frozen channel it sent two
 * separate diagnoses down the wrong path, because a warning that fires on every
 * success cannot be told from one that fires on a failure.
 *
 * The fake stays alive rather than exiting, which is the whole point — the
 * reader never reaches EOF, so the session is stopped while ffmpeg is still
 * running.
 */
func TestStoppingARunningSessionIsNotAnError(t *testing.T) {
	bin := goFakeFFmpegImporting(t, []string{"fmt", "os", "time"},
		"\tfmt.Print(\"some bytes\")\n"+
			"\tos.Stdout.Sync()\n"+
			"\tfmt.Fprintln(os.Stderr, \"Error submitting a packet to the muxer: Broken pipe\")\n"+
			"\ttime.Sleep(30 * time.Second)")
	m, out := loggingManager(t, bin)

	stream, err := m.Live(context.Background(), 99, LiveOptions{
		URL: "http://example.invalid/channel.ts", Decision: remux(),
	})
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	// Read a little and walk away, which is what switching channels does. Not
	// io.Copy: reading to EOF would mean ffmpeg had ended by itself, which is
	// the case this test exists to be distinguished from.
	buf := make([]byte, 4)
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	stream.Close()

	if strings.Contains(out.String(), "ffmpeg reported errors") {
		t.Errorf("stopping a healthy session logged an error line.\ngot: %s", out.String())
	}
}

/*
 * ...but the text is kept, not discarded.
 *
 * Downgrading the level is only defensible if the evidence survives. A
 * maintainer turning debug logging on to investigate playback should still find
 * what ffmpeg said on the way out, or this has traded a noisy log for a silent
 * one — which is the other way to lose an investigation.
 */
func TestWhatAStoppedFFmpegSaidIsStillAvailableAtDebug(t *testing.T) {
	bin := goFakeFFmpegImporting(t, []string{"fmt", "os", "time"},
		"\tfmt.Print(\"some bytes\")\n"+
			"\tos.Stdout.Sync()\n"+
			"\tfmt.Fprintln(os.Stderr, \"Error submitting a packet to the muxer: Broken pipe\")\n"+
			"\ttime.Sleep(30 * time.Second)")

	out := &safeBuf{}
	m := NewManager(t.TempDir(), slog.New(slog.NewTextHandler(out,
		&slog.HandlerOptions{Level: slog.LevelDebug})))
	m.bin = bin

	stream, err := m.Live(context.Background(), 100, LiveOptions{
		URL: "http://example.invalid/channel.ts", Decision: remux(),
	})
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	stream.Close()

	if !strings.Contains(out.String(), "Broken pipe") {
		t.Errorf("ffmpeg's parting words were dropped entirely.\ngot: %s", out.String())
	}
}
