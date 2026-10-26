/*
 * Package webhook delivers events to the URLs an administrator has set
 * (ADR 0097).
 *
 * Nothing is sent unless somebody configured a webhook: no defaults, no
 * service of LANcast's own. Deliveries are signed with the webhook's secret,
 * queued off the request path, tried three times, and never follow a
 * redirect. What happened last is kept in memory for Settings to show.
 *
 * Which events exist, and what each discloses, is decided by the callers and
 * written down in the ADR. This package moves bytes and keeps score.
 */
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lancast/internal/config"
)

// The events a webhook can subscribe to. Each is an ADR 0097 decision.
const (
	PlaybackStarted = "playback.started"
	PlaybackStopped = "playback.stopped"
	ScanFinished    = "scan.finished"
	BackupTaken     = "backup.taken"
	Ping            = "ping"
)

// Events lists what may be subscribed to. Ping is not here: it is sent by
// Test to one webhook, whatever it subscribes to.
var Events = []string{PlaybackStarted, PlaybackStopped, ScanFinished, BackupTaken}

// MaxHooks bounds how many webhooks a server keeps.
const MaxHooks = 10

var (
	ErrBadURL     = errors.New("a webhook URL must be an http or https address with a host")
	ErrBadEvent   = errors.New("unknown event")
	ErrNoEvents   = errors.New("a webhook needs at least one event")
	ErrTooMany    = fmt.Errorf("at most %d webhooks", MaxHooks)
	ErrNotFound   = errors.New("no such webhook")
	errNotSuccess = errors.New("the receiver did not answer with a 2xx status")
)

// ValidateURL checks a URL an administrator typed. LAN addresses are allowed:
// that is where Home Assistant lives (ADR 0097).
func ValidateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return "", ErrBadURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", ErrBadURL
	}
	return u.String(), nil
}

// ValidateEvents checks and normalises a subscription list.
func ValidateEvents(events []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, e := range events {
		e = strings.TrimSpace(e)
		ok := false
		for _, known := range Events {
			if e == known {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("%q: %w", e, ErrBadEvent)
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoEvents
	}
	return out, nil
}

// NewSecret makes a signing secret and an id for a new webhook.
func NewSecret() (id, secret string, err error) {
	b := make([]byte, 40)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	return "wh_" + hex.EncodeToString(b[:8]), "whsec_" + hex.EncodeToString(b[8:]), nil
}

// Sign is the X-LANcast-Signature value for a body.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Payload is what a receiver gets.
type Payload struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	At   int64  `json:"at"`
	Data any    `json:"data"`
}

// Status is what happened the last time a webhook was sent something.
type Status struct {
	At     int64  `json:"at"`
	Event  string `json:"event"`
	OK     bool   `json:"ok"`
	Code   int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	Tries  int    `json:"tries"`
	TookMS int64  `json:"took_ms"`
}

type delivery struct {
	hook    config.Webhook
	payload Payload
}

// Dispatcher sends events. The zero value is not usable; call New.
type Dispatcher struct {
	hooks   func() []config.Webhook
	client  *http.Client
	log     *slog.Logger
	version string
	queue   chan delivery
	backoff []time.Duration

	mu      sync.Mutex
	status  map[string]Status
	dropped atomic.Int64
}

// New makes a dispatcher reading its webhooks through hooks, so a change in
// Settings applies to the next event with nothing to restart.
func New(hooks func() []config.Webhook, version string, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		hooks: hooks,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Sent to the URL it was given and nowhere it is sent on to.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		log:     log,
		version: version,
		queue:   make(chan delivery, 256),
		backoff: []time.Duration{2 * time.Second, 10 * time.Second},
		status:  map[string]Status{},
	}
}

// Run delivers queued events until ctx ends. Four at a time, so one slow
// receiver does not hold up the rest.
func (d *Dispatcher) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case dl := <-d.queue:
					d.deliver(ctx, dl)
				}
			}
		}()
	}
	wg.Wait()
}

// Emit queues an event for every enabled webhook subscribed to it. It never
// blocks: when the queue is full the event is dropped and counted, because a
// webhook is not worth slowing the server down for.
func (d *Dispatcher) Emit(event string, data any) {
	if d == nil {
		return
	}
	var targets []config.Webhook
	for _, h := range d.hooks() {
		if !h.Enabled {
			continue
		}
		for _, e := range h.Events {
			if e == event {
				targets = append(targets, h)
				break
			}
		}
	}
	if len(targets) == 0 {
		return
	}
	p := newPayload(event, data)
	for _, h := range targets {
		select {
		case d.queue <- delivery{hook: h, payload: p}:
		default:
			d.dropped.Add(1)
			d.log.Warn("webhook queue full; event dropped", "webhook", h.ID, "event", event)
		}
	}
}

// Test sends ping to one webhook now and reports what happened, once, with
// no retries: the person pressing Test is waiting for the answer.
func (d *Dispatcher) Test(ctx context.Context, h config.Webhook) Status {
	return d.attemptAll(ctx, delivery{hook: h, payload: newPayload(Ping, map[string]any{"message": "Test from LANcast"})}, false)
}

// Last is the most recent outcome for each webhook, by id.
func (d *Dispatcher) Last() map[string]Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]Status, len(d.status))
	for k, v := range d.status {
		out[k] = v
	}
	return out
}

// Dropped is how many deliveries were dropped for a full queue since start.
func (d *Dispatcher) Dropped() int64 { return d.dropped.Load() }

func newPayload(event string, data any) Payload {
	id := make([]byte, 8)
	rand.Read(id)
	return Payload{ID: "dl_" + hex.EncodeToString(id), Type: event, At: time.Now().Unix(), Data: data}
}

func (d *Dispatcher) deliver(ctx context.Context, dl delivery) {
	st := d.attemptAll(ctx, dl, true)
	if !st.OK {
		d.log.Warn("webhook delivery failed", "webhook", dl.hook.ID, "event", dl.payload.Type,
			"status", st.Code, "error", st.Error, "tries", st.Tries)
	}
}

func (d *Dispatcher) attemptAll(ctx context.Context, dl delivery, retry bool) Status {
	body, err := json.Marshal(dl.payload)
	if err != nil {
		return d.record(dl.hook.ID, Status{At: time.Now().Unix(), Event: dl.payload.Type, Error: err.Error()})
	}
	start := time.Now()
	st := Status{Event: dl.payload.Type}
	attempts := 1
	if retry {
		attempts += len(d.backoff)
	}
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				st.Error = "stopped before it could be retried"
				return d.record(dl.hook.ID, finish(st, start, i))
			case <-time.After(d.backoff[i-1]):
			}
		}
		code, err := d.post(ctx, dl, body)
		st.Code = code
		if err == nil {
			st.OK, st.Error = true, ""
			return d.record(dl.hook.ID, finish(st, start, i+1))
		}
		st.Error = err.Error()
	}
	return d.record(dl.hook.ID, finish(st, start, attempts))
}

func finish(st Status, start time.Time, tries int) Status {
	st.At = time.Now().Unix()
	st.Tries = tries
	st.TookMS = time.Since(start).Milliseconds()
	return st
}

func (d *Dispatcher) post(ctx context.Context, dl delivery, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dl.hook.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LANcast/"+d.version)
	req.Header.Set("X-LANcast-Event", dl.payload.Type)
	req.Header.Set("X-LANcast-Delivery", dl.payload.ID)
	req.Header.Set("X-LANcast-Signature", Sign(dl.hook.Secret, body))
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, describe(err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("%w (%d)", errNotSuccess, resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// describe turns a transport error into a sentence without the URL, which can
// carry a token somebody put in it.
func describe(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return errors.New("no answer within ten seconds")
		}
		return ue.Err
	}
	return err
}

func (d *Dispatcher) record(id string, st Status) Status {
	d.mu.Lock()
	d.status[id] = st
	d.mu.Unlock()
	return st
}
