package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/applog"
)

type logBody struct {
	Lines    []string `json:"lines"`
	Complete bool     `json:"complete"`
	Path     string   `json:"path"`
}

// A server that has only ever run in a terminal has no log file. That reads as
// an empty log, not an error the UI has to render — and as an empty array, so
// the client has one shape to handle.
func TestServerLogAbsent(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/logs", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body logBody
	decode(t, resp, &body)
	if body.Lines == nil {
		t.Error("lines is null; want an empty array")
	}
	if !body.Complete {
		t.Error("complete = false when nothing was withheld")
	}
	if body.Path != applog.FileName {
		t.Errorf("path = %q, want %q", body.Path, applog.FileName)
	}
}

func TestServerLogTail(t *testing.T) {
	h := newHarness(t)
	writeLog(t, h.dataDir, "first\nsecond\nthird\n")

	var body logBody
	decode(t, h.do(t, "GET", "/api/logs?lines=2", nil), &body)
	if got := strings.Join(body.Lines, "|"); got != "second|third" {
		t.Errorf("lines = %q, want %q", got, "second|third")
	}
	// Saying the view is partial is the difference between "this is the log"
	// and "this is the end of the log".
	if body.Complete {
		t.Error("complete = true while a line was dropped")
	}
}

func TestServerLogRejectsBadLineCount(t *testing.T) {
	h := newHarness(t)
	for _, v := range []string{"0", "-5", "many"} {
		wantError(t, h.do(t, "GET", "/api/logs?lines="+v, nil),
			http.StatusBadRequest, "bad_request")
	}
}

// An oversized request is clamped rather than refused: the caller asked for
// "all of it", and the honest answer is as much of it as one response carries.
func TestServerLogClampsLineCount(t *testing.T) {
	h := newHarness(t)
	writeLog(t, h.dataDir, "only\n")

	resp := h.do(t, "GET", "/api/logs?lines=999999", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body logBody
	decode(t, resp, &body)
	if len(body.Lines) != 1 {
		t.Errorf("lines = %v, want 1", body.Lines)
	}
}

func writeLog(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, applog.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
