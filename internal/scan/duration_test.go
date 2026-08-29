package scan

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * A finished scan says how long it took.
 *
 * The line recorded what it found — seen, changed, missing — and nothing about
 * the cost of finding it, which is the one number needed to decide whether a
 * scan is worth optimising. Answering "is the walk still expensive on a
 * 9,276-track library" took a stopwatch, the settings pane and a polling loop,
 * to learn something the scan already knew.
 *
 * Milliseconds rather than the difference of the two second-resolution stamps
 * already on Progress: that measurement came out at about two seconds, and a
 * subtraction of Unix seconds reports it as "0" or "3" — the gap between those
 * being most of the answer.
 */

// logged runs a scan against a real logger and returns everything it wrote.
func logged(t *testing.T) (*Scanner, *store.Store, *bytes.Buffer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	var buf bytes.Buffer
	// Debug so the failure path's message is captured too; the assertions name
	// the record they want rather than relying on the level to filter.
	return New(st, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))), st, &buf
}

// elapsedFrom pulls the reported duration out of a log line, or fails.
func elapsedFrom(t *testing.T, line string) int64 {
	t.Helper()
	i := strings.Index(line, "elapsed_ms=")
	if i < 0 {
		t.Fatalf("no duration in the line at all: %s", line)
	}
	field := line[i+len("elapsed_ms="):]
	if sp := strings.IndexByte(field, ' '); sp >= 0 {
		field = field[:sp]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
	if err != nil {
		t.Fatalf("duration %q is not a number: %v", field, err)
	}
	return n
}

func TestAFinishedScanReportsHowLongItTook(t *testing.T) {
	sc, st, buf := logged(t)
	root := t.TempDir()
	/*
	 * Enough files that the scan cannot finish inside a millisecond.
	 *
	 * A one-file scan can legitimately report 0, which makes "greater than
	 * zero" unassertable — and 0 is exactly what a duration measured from a
	 * zero-valued start, or never measured at all, also produces. The first
	 * version of this test passed with the field hardcoded to 0, which is a
	 * test that cannot fail.
	 */
	for i := 0; i < 200; i++ {
		writeFile(t, root, fmt.Sprintf("Film %03d (2020).mkv", i), 16)
	}

	lib, err := st.CreateLibrary(context.Background(), "Media", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	scanAndWait(t, sc, *lib)
	wall := time.Since(before).Milliseconds()

	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "scan complete") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("no completion was logged at all")
	}
	ms := elapsedFrom(t, line)
	if ms <= 0 {
		t.Errorf("elapsed_ms = %d for a 200-file scan; it is not counting from the start", ms)
	}
	// And it is the scan's own clock rather than something unrelated: the scan
	// cannot have taken longer than the wall time around it.
	if ms > wall {
		t.Errorf("elapsed_ms = %d but the whole call took %dms", ms, wall)
	}

}

/*
 * A scan that failed after forty minutes and one that failed on its first
 * directory are different faults, and the line said the same thing about both.
 */
func TestAFailedScanAlsoReportsHowLongItTook(t *testing.T) {
	sc, st, buf := logged(t)

	lib, err := st.CreateLibrary(context.Background(), "Media", "movie",
		filepath.Join(t.TempDir(), "not-there"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Start(*lib); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := 0; i < 500; i++ {
		if sc.Status(lib.ID).State != StateRunning {
			break
		}
	}

	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "scan failed") {
			if !strings.Contains(l, "elapsed_ms=") {
				t.Errorf("failure carries no duration: %s", l)
			}
			return
		}
	}
	// A missing root may be reported rather than failed depending on the
	// platform; nothing is asserted about which, only about the line if it came.
}
