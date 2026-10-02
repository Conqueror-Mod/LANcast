package api

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * A peer that stays off is logged once, not once a minute.
 *
 * Measured on a real install: 3,263 "peer not answering" lines over five days
 * for one switched-off peer, one every 63 seconds, because the rail polls each
 * peer's libraries every minute and every failure was logged as news. Driven
 * through peerUnreachable, the function every failed peer call ends in, so the
 * test covers the browse and playback routes alike.
 */
func TestAnOfflinePeerIsLoggedOnceNotPerPoll(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{
		log:      slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		peerDown: map[string]time.Time{},
	}
	p := store.Peer{Fingerprint: "F7H2", Name: "Utopia"}
	down := errors.New("context deadline exceeded")
	fail := func() {
		w := httptest.NewRecorder()
		s.peerUnreachable(w, p, down)
		// The caller's answer is unchanged: still a 502 every time.
		if w.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 on every failed call", w.Code)
		}
	}
	count := func(msg string) int { return strings.Count(buf.String(), `msg="`+msg+`"`) }

	for range 60 { // an hour of polling against a peer that is off
		fail()
	}
	if n := count("peer not answering"); n != 1 {
		t.Fatalf(`an hour offline logged "peer not answering" %d times, want once`, n)
	}

	s.peerAnswered(p)
	if n := count("peer answering again"); n != 1 {
		t.Fatalf(`recovery logged %d times, want once`, n)
	}
	s.peerAnswered(p) // a healthy peer answering again is not news
	if n := count("peer answering again"); n != 1 {
		t.Errorf(`a peer that never went down was logged as recovering`)
	}

	fail() // the next outage is news again
	if n := count("peer not answering"); n != 2 {
		t.Errorf(`a second outage logged %d lines in total, want 2`, n)
	}
}

// One peer being off says nothing about another.
func TestPeerHealthIsPerPeer(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{
		log:      slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		peerDown: map[string]time.Time{},
	}
	a := store.Peer{Fingerprint: "AAAA", Name: "A"}
	b := store.Peer{Fingerprint: "BBBB", Name: "B"}
	s.peerUnreachable(httptest.NewRecorder(), a, errors.New("down"))
	s.peerUnreachable(httptest.NewRecorder(), b, errors.New("down"))
	if n := strings.Count(buf.String(), `msg="peer not answering"`); n != 2 {
		t.Fatalf("two peers going down logged %d lines, want one each", n)
	}
	s.peerAnswered(a)
	if strings.Contains(buf.String(), "peer=BBBB down_for") {
		t.Error("A answering cleared B")
	}
}
