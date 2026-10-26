package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lancast/internal/config"
)

type receiver struct {
	mu   sync.Mutex
	got  []received
	code atomic.Int32
	srv  *httptest.Server
}

type received struct {
	header  http.Header
	body    []byte
	payload Payload
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{}
	r.code.Store(200)
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		var p Payload
		json.Unmarshal(b, &p)
		r.mu.Lock()
		r.got = append(r.got, received{header: req.Header.Clone(), body: b, payload: p})
		r.mu.Unlock()
		w.WriteHeader(int(r.code.Load()))
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *receiver) waitFor(t *testing.T, n int) []received {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r.count() >= n {
			r.mu.Lock()
			defer r.mu.Unlock()
			return append([]received(nil), r.got...)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("received %d deliveries, want %d", r.count(), n)
	return nil
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func running(t *testing.T, hooks ...config.Webhook) *Dispatcher {
	t.Helper()
	d := New(func() []config.Webhook { return hooks }, "test", quietLog())
	d.backoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	return d
}

func hook(url string, events ...string) config.Webhook {
	return config.Webhook{ID: "wh_1", URL: url, Events: events, Secret: "whsec_test", Enabled: true}
}

func TestADeliveryIsSignedAndSaysWhatItIs(t *testing.T) {
	r := newReceiver(t)
	d := running(t, hook(r.srv.URL, ScanFinished))
	d.Emit(ScanFinished, map[string]any{"library": "Movies"})

	got := r.waitFor(t, 1)[0]
	if got.header.Get("X-LANcast-Event") != ScanFinished || got.payload.Type != ScanFinished {
		t.Fatalf("event header %q, payload type %q", got.header.Get("X-LANcast-Event"), got.payload.Type)
	}
	if got.header.Get("X-LANcast-Signature") != Sign("whsec_test", got.body) {
		t.Fatal("the signature does not match the body")
	}
	if got.header.Get("X-LANcast-Delivery") != got.payload.ID || got.payload.ID == "" {
		t.Fatal("delivery id missing or inconsistent")
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type %q", got.header.Get("Content-Type"))
	}
	if st := d.Last()["wh_1"]; !st.OK || st.Code != 200 || st.Tries != 1 {
		t.Fatalf("status = %+v", st)
	}
}

func TestOnlySubscribedEnabledHooksAreSent(t *testing.T) {
	r := newReceiver(t)
	off := hook(r.srv.URL, ScanFinished)
	off.ID, off.Enabled = "wh_off", false
	d := running(t, hook(r.srv.URL, BackupTaken), off)
	d.Emit(ScanFinished, nil)
	d.Emit(BackupTaken, nil)
	got := r.waitFor(t, 1)
	time.Sleep(50 * time.Millisecond)
	if r.count() != 1 || got[0].payload.Type != BackupTaken {
		t.Fatalf("received %d, first %q", r.count(), got[0].payload.Type)
	}
}

func TestAFailingReceiverIsTriedThreeTimes(t *testing.T) {
	r := newReceiver(t)
	r.code.Store(503)
	d := running(t, hook(r.srv.URL, ScanFinished))
	d.Emit(ScanFinished, nil)
	r.waitFor(t, 3)
	time.Sleep(50 * time.Millisecond)
	if r.count() != 3 {
		t.Fatalf("tried %d times, want 3", r.count())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && d.Last()["wh_1"].Tries == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if st := d.Last()["wh_1"]; st.OK || st.Code != 503 || st.Tries != 3 {
		t.Fatalf("status = %+v", st)
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	r := newReceiver(t)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, r.srv.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	d := New(func() []config.Webhook { return nil }, "test", quietLog())
	st := d.Test(context.Background(), hook(redirect.URL, ScanFinished))
	if st.OK || st.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %+v, want a failure at the redirect", st)
	}
	if r.count() != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestTestSendsPingOnceAndWaits(t *testing.T) {
	r := newReceiver(t)
	r.code.Store(500)
	d := New(func() []config.Webhook { return nil }, "test", quietLog())
	st := d.Test(context.Background(), hook(r.srv.URL, ScanFinished))
	if st.OK || st.Tries != 1 || r.count() != 1 || r.got[0].payload.Type != Ping {
		t.Fatalf("status = %+v, received %d", st, r.count())
	}
}

func TestAFullQueueDropsRatherThanBlocks(t *testing.T) {
	d := New(func() []config.Webhook { return []config.Webhook{hook("http://127.0.0.1:1/", ScanFinished)} }, "test", quietLog())
	// Not running, so nothing drains the queue.
	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(d.queue)+5; i++ {
			d.Emit(ScanFinished, nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Emit blocked on a full queue")
	}
	if d.Dropped() != 5 {
		t.Fatalf("dropped %d, want 5", d.Dropped())
	}
}

func TestValidation(t *testing.T) {
	for _, bad := range []string{"", "ftp://x/", "http://", "/relative", "http://user:pw@host/"} {
		if _, err := ValidateURL(bad); !errors.Is(err, ErrBadURL) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"http://192.168.1.10:8123/api/webhook/abc", "https://discord.com/api/webhooks/1/x"} {
		if _, err := ValidateURL(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
	if _, err := ValidateEvents(nil); !errors.Is(err, ErrNoEvents) {
		t.Error("no events accepted")
	}
	if _, err := ValidateEvents([]string{"ping"}); !errors.Is(err, ErrBadEvent) {
		t.Error("ping is not subscribable")
	}
	got, err := ValidateEvents([]string{ScanFinished, ScanFinished, BackupTaken})
	if err != nil || len(got) != 2 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestTransportErrorsDoNotRepeatTheURL(t *testing.T) {
	d := New(func() []config.Webhook { return nil }, "test", quietLog())
	st := d.Test(context.Background(), hook("http://127.0.0.1:1/secret-token-in-path", ScanFinished))
	if st.OK || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
	if contains(st.Error, "secret-token-in-path") {
		t.Fatalf("error repeats the URL: %q", st.Error)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
