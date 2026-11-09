package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lancast/internal/diskuse"
)

/*
 * GET /api/storage (settings-3-plan step 7): the measurement never runs inside
 * the request, one runs at a time, a fresh one is reused, and only an admin
 * may ask.
 */

type storageAnswer struct {
	Measuring bool            `json:"measuring"`
	Report    *diskuse.Report `json:"report"`
}

func readStorage(t *testing.T, h *harness, path string) storageAnswer {
	t.Helper()
	var a storageAnswer
	decode(t, h.do(t, "GET", path, nil), &a)
	return a
}

// untilMeasured asks until a measurement is in, as the client does.
func untilMeasured(t *testing.T, h *harness) storageAnswer {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a := readStorage(t, h, "/api/storage"); !a.Measuring && a.Report != nil {
			return a
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no measurement arrived")
	return storageAnswer{}
}

func TestStorageIsMeasuredInTheBackgroundAndReported(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Join(h.dataDir, "previews"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, "previews", "7.jpg"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	first := readStorage(t, h, "/api/storage")
	if first.Report != nil || !first.Measuring {
		t.Fatalf("first answer = %+v, want no report yet and a measurement running", first)
	}
	a := untilMeasured(t, h)
	var previews *diskuse.Part
	for i := range a.Report.Parts {
		if a.Report.Parts[i].ID == "previews" {
			previews = &a.Report.Parts[i]
		}
	}
	if previews == nil || previews.Bytes != 4096 || previews.Files != 1 {
		t.Errorf("previews = %+v, want 4096 bytes in one file", previews)
	}
	if a.Report.Parts[len(a.Report.Parts)-1].ID != "other" {
		t.Error("other is not last")
	}
}

func TestAFreshMeasurementIsReusedUntilAskedAgain(t *testing.T) {
	h := newHarness(t)
	var walks atomic.Int32
	h.srvAPI.storage.measure = func(ctx context.Context, l diskuse.Layout) diskuse.Report {
		walks.Add(1)
		return diskuse.Report{DataDir: l.DataDir, MeasuredAt: time.Now().Unix()}
	}
	untilMeasured(t, h)
	readStorage(t, h, "/api/storage")
	readStorage(t, h, "/api/storage")
	if n := walks.Load(); n != 1 {
		t.Errorf("walks = %d, want 1: a fresh measurement was redone", n)
	}
	readStorage(t, h, "/api/storage?fresh=1")
	untilMeasured(t, h)
	if n := walks.Load(); n != 2 {
		t.Errorf("walks = %d, want 2 after ?fresh=1", n)
	}
}

func TestOneMeasurementRunsAtATime(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	var walks atomic.Int32
	h.srvAPI.storage.measure = func(ctx context.Context, l diskuse.Layout) diskuse.Report {
		walks.Add(1)
		<-release
		return diskuse.Report{MeasuredAt: time.Now().Unix()}
	}
	for i := 0; i < 3; i++ {
		if a := readStorage(t, h, "/api/storage?fresh=1"); !a.Measuring {
			t.Errorf("ask %d: not measuring", i)
		}
	}
	close(release)
	untilMeasured(t, h)
	if n := walks.Load(); n != 1 {
		t.Errorf("walks = %d, want 1 while one was already running", n)
	}
}

func TestOnlyAnAdminSeesStorage(t *testing.T) {
	h := newHarness(t)
	m := h.member(t, "reader", "a long enough password")
	resp := h.asUser(t, m, "GET", "/api/storage", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("member: %d, want 403", resp.StatusCode)
	}
}
