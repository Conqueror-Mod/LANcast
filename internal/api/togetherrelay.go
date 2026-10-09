package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lancast/internal/peer"
	"lancast/internal/store"
	"lancast/internal/together"
)

/*
 * A room on a paired server, from the guest's side.
 *
 * Phase 5, step 4 ([plan](../../docs/phase-5-room-crosses-the-boundary-plan.md)).
 * The guest's window trusts one server's key (ADR 0070), so it cannot reach
 * the host's room at all. It asks this server, and this server asks theirs
 * over the pinned peer channel (ADR 0046, amended).
 *
 * # This server decides nothing about the room
 *
 * The same standing as peerplay.go: a pipe. Whether somebody may ask, whether
 * they are let in and what they may play are the host's answers. What this
 * side owns is one fact nobody else can supply: **which person is asking**.
 * It is always the caller's own account, taken from their session here, and
 * never from anything in the request. Otherwise one member of a household
 * could ask, join or play as another.
 *
 * # Their answers are re-encoded, not passed through
 *
 * A success is decoded into the shape this side expects and written again, so
 * a field the host's server adds does not reach this household's client
 * because it happened to be in the body. A refusal is reported in this
 * server's own words, for the reason peerRefusalText gives: a message this
 * server repeats is a message this server is vouching for.
 */

// peerAnswer is one HTTP answer from a peer, whatever its status.
type peerAnswer struct {
	status int
	body   []byte
}

/*
 * sendPeer makes one request to a peer and returns its answer, whatever the
 * status.
 *
 * callPeer treats every status but 200 as a reason to try the next address,
 * which is right for presence ("is anybody there") and wrong here: a 404 from
 * the host means **the room has ended**, and reported as "not answering" it
 * would send the guest's player looking for a network fault. Only a transport
 * failure moves on to the next address. An HTTP answer of any kind is the
 * peer, answering.
 */
func (s *Server) sendPeer(ctx context.Context, p store.Peer, method, path string, body any) (peerAnswer, error) {
	client, err := peer.Client(s.ident, p.Fingerprint)
	if err != nil {
		return peerAnswer{}, err
	}
	var payload []byte
	if body != nil {
		if payload, err = json.Marshal(body); err != nil {
			return peerAnswer{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, peerDeadline)
	defer cancel()

	var lastErr error
	for _, addr := range s.reachOrder(ctx, p) {
		req, err := http.NewRequestWithContext(ctx, method, "https://"+addr+path, bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		// Bounded: everything on this path is a few hundred bytes of JSON.
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		s.rosterMu.Lock()
		s.goodAddr[p.Fingerprint] = addr
		s.rosterMu.Unlock()
		s.peerAnswered(p)
		_ = s.st.MarkPeerSeen(context.WithoutCancel(ctx), p.Fingerprint, time.Now())
		return peerAnswer{status: resp.StatusCode, body: raw}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("peer %s has no address to try", p.Name)
	}
	return peerAnswer{}, lastErr
}

// asMe is the person query for a call made on the caller's behalf. Always the
// caller's own account: see the file comment.
func (s *Server) asMe(r *http.Request) string {
	return "person=" + url.QueryEscape(s.userID(r))
}

/*
 * relayRoom sends one room call and writes the answer back.
 *
 * out is the shape a success is decoded into and re-encoded from. A refusal
 * keeps its status, so the client can tell "the room has ended" (404) from
 * "their server is not answering" (502), and gets this server's sentence for
 * it rather than theirs.
 */
func (s *Server) relayRoom(w http.ResponseWriter, r *http.Request, method, path string, body any, out any) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	ans, err := s.sendPeer(r.Context(), p, method, path, body)
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	switch {
	case ans.status == http.StatusNoContent:
		w.WriteHeader(http.StatusNoContent)
	case ans.status >= 200 && ans.status < 300 && out != nil:
		if err := json.Unmarshal(ans.body, out); err != nil {
			s.peerUnreachable(w, p, errors.New("unreadable answer: "+err.Error()))
			return
		}
		writeJSON(w, ans.status, out)
	case ans.status == http.StatusNotFound:
		writeError(w, http.StatusNotFound, "not_found", "that session has ended, or was never open to you")
	default:
		s.peerUnreachable(w, p, &peerRefusal{status: ans.status})
	}
}

// relayAnswer is the asker's view of a request, as this side relays it.
type relayAnswer struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	RoomID string `json:"room_id,omitempty"`
}

/*
 * peerAskTogether asks to join a person on a paired server, as the caller.
 *
 * The body names the **host**: their account id on that server, as the People
 * page received it from presence. The asker is not in the body and cannot be.
 */
func (s *Server) peerAskTogether(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Person string `json:"person"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Person == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which person to join")
		return
	}
	s.relayRoom(w, r, http.MethodPost, "/api/federation/together/requests?"+s.asMe(r),
		map[string]any{"host": req.Person}, &relayAnswer{})
}

// peerTogetherRequest asks for the answer.
func (s *Server) peerTogetherRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathPart(w, r, "id", "which request")
	if !ok {
		return
	}
	s.relayRoom(w, r, http.MethodGet,
		"/api/federation/together/requests/"+id+"?"+s.asMe(r), nil, &relayAnswer{})
}

// peerWithdrawTogether takes the caller's ask back before it is answered.
func (s *Server) peerWithdrawTogether(w http.ResponseWriter, r *http.Request) {
	id, ok := pathPart(w, r, "id", "which request")
	if !ok {
		return
	}
	s.relayRoom(w, r, http.MethodDelete,
		"/api/federation/together/requests/"+id+"?"+s.asMe(r), nil, &relayAnswer{})
}

func (s *Server) peerJoinTogether(w http.ResponseWriter, r *http.Request) {
	room, ok := pathPart(w, r, "room", "which room")
	if !ok {
		return
	}
	s.relayRoom(w, r, http.MethodPost,
		"/api/federation/together/"+room+"/join?"+s.asMe(r), nil, &together.Session{})
}

func (s *Server) peerPollTogether(w http.ResponseWriter, r *http.Request) {
	room, ok := pathPart(w, r, "room", "which room")
	if !ok {
		return
	}
	s.relayRoom(w, r, http.MethodGet,
		"/api/federation/together/"+room+"?"+s.asMe(r), nil, &together.Session{})
}

func (s *Server) peerLeaveTogether(w http.ResponseWriter, r *http.Request) {
	room, ok := pathPart(w, r, "room", "which room")
	if !ok {
		return
	}
	s.relayRoom(w, r, http.MethodDelete,
		"/api/federation/together/"+room+"/members/me?"+s.asMe(r), nil, nil)
}

// pathPart reads one path value that is about to become a path segment in a
// request to somebody else's server, and escapes it.
func pathPart(w http.ResponseWriter, r *http.Request, name, what string) (string, bool) {
	v := r.PathValue(name)
	if v == "" || len(v) > 128 {
		writeError(w, http.StatusBadRequest, "bad_request", what)
		return "", false
	}
	return url.PathEscape(v), true
}

// --- playing the room's film -------------------------------------------------

/*
 * memberQuery names the caller to the host on a playback request, when, and
 * only when, the client says it is playing as a room member (`?together=1`).
 *
 * The host admits a room member to the room's film by person (ADR 0046 §4,
 * amended), so the playback pipes have to say who is asking. Not on every
 * request: browsing a shared library names a server, not a person, and the
 * host has never needed to know which of this household's people is
 * browsing. Naming them is disclosure, so it happens only where it is the
 * point.
 *
 * Any `person` the client put on the query is stripped by forwardedQuery, so
 * this is the only way one reaches the host.
 */
func (s *Server) memberQuery(r *http.Request) string {
	if r.URL.Query().Get("together") == "" {
		return ""
	}
	return "&" + s.asMe(r)
}

// withQuery appends a query in forwardedQuery's "&a=b" form to a path that may
// or may not already have one.
func withQuery(path, q string) string {
	if q == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + strings.TrimPrefix(q, "&")
}
