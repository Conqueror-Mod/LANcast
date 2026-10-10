package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"lancast/internal/store"
)

/*
 * The event stream: the server saying that something changed
 * ([ADR 0079](../../docs/adr/0079-the-server-says-when-something-changed.md)).
 *
 * A screen otherwise learns of change only from its own mutations or a job
 * poll, so anything the server changes by itself stayed on screen stale until
 * the screen remounted. The first evening with a remote peer hit that three
 * times in an hour: a pairing that had become mutual still said "added", a
 * friend's film did not appear on People, and promotion itself only ran while
 * somebody had People open.
 *
 * Topics, never payloads. An event says "ask again" and the answer comes from
 * the route that always served it, so authorization stays where it was and a
 * missed event can only leave a window holding old data, never wrong data.
 */

// The topics this server publishes. docs/api.md lists them for third-party
// clients, and web/src/api/serverEvents.ts maps each to the queries it makes
// stale. A topic added here needs a row in both.
const (
	// TopicPeers: a pairing was added, removed or became mutual, a peer's
	// roster changed, or what is shared with a peer changed.
	TopicPeers = store.ChangePeers
	// TopicPresence: what a paired server lets this person see changed.
	// Published only to that person's windows.
	TopicPresence = "presence"
	// TopicLibraries: a library was added, removed, renamed or rescanned.
	TopicLibraries = store.ChangeLibraries
	// TopicItems: what a library holds changed, by a scan or enrichment.
	TopicItems = "items"
)

/*
 * eventHeartbeat is how often an idle stream sends a comment.
 *
 * A connection that carries nothing for minutes looks dead to every proxy and
 * NAT between the window and the server, and some close it. A comment line is
 * ignored by EventSource and keeps the path warm. It is also how the handler
 * notices a window that has gone: the write fails.
 */
const eventHeartbeat = 25 * time.Second

// Publish tells every open window that topics changed.
func (s *Server) Publish(topics ...string) { s.events.Publish(topics...) }

// CloseEvents ends every open stream. Called on shutdown, before the HTTP
// server's graceful close, which would otherwise wait out its whole grace
// period on responses that never finish by themselves.
func (s *Server) CloseEvents() { s.events.Close() }

func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	// A reverse proxy in front of the server must not buffer an answer that
	// is only ever a few bytes at a time.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// How long EventSource waits before reconnecting. The client invalidates
	// everything on screen when it does (ADR 0079 §4), so this only bounds
	// how long a restart leaves a window unaware.
	if _, err := fmt.Fprint(w, "retry: 3000\n: connected\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	sub := s.events.Subscribe(s.userID(r))
	defer s.events.Unsubscribe(sub)

	for {
		ctx, cancel := context.WithTimeout(r.Context(), eventHeartbeat)
		topics := sub.Next(ctx)
		cancel()

		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		default:
		}

		var err error
		if topics == nil {
			_, err = fmt.Fprint(w, ": ping\n\n")
		} else {
			sort.Strings(topics)
			data, _ := json.Marshal(map[string][]string{"topics": topics})
			_, err = fmt.Fprintf(w, "event: changed\ndata: %s\n\n", data)
		}
		if err == nil {
			err = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}
