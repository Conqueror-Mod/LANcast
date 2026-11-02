package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"lancast/internal/retro/ra"
	"lancast/internal/store"
)

/*
 * Earning achievements (ADR 0112 section 3): the server is the player's one
 * door to RetroAchievements.
 *
 *   - PUT /api/profile/retroachievements/earning turns it on: the password
 *     goes to RetroAchievements once, and only the token it returns is kept,
 *     sealed like the profile's API key.
 *   - DELETE turns it off: the token goes, and anything still queued.
 *   - POST /api/items/{id}/retroachievements is the door. The desktop
 *     player's rcheevos sends what it would send RetroAchievements; the
 *     server checks it (ra.Admit), puts the real token in, forwards it as
 *     the player's user agent, and answers with RetroAchievements' reply.
 *     The game's ticket reaches it, as it reaches the game's saves.
 *
 * An unlock or leaderboard entry RetroAchievements can't take — no answer,
 * an outage, too many requests — goes into the queue, and the player is told
 * it succeeded, because the server now holds it. Once an account has
 * anything queued, its new unlocks queue behind it, so they reach
 * RetroAchievements in the order they were earned. RunRetroQueue sends them.
 */

const (
	// maxDoorBody bounds a request through the door. rcheevos' largest is an
	// award with its hash and signature, under a kilobyte.
	maxDoorBody = 16 << 10
	// The queue is tried every minute while it is empty or draining, and
	// backs off to every fifteen while RetroAchievements is not answering.
	raQueueIdle    = time.Minute
	raQueueMaxWait = 15 * time.Minute
	raQueueBatch   = 50
)

// raScores is each account's points as RetroAchievements last said, for the
// reply to a queued unlock: rcheevos takes the score from it, and a reply
// without one would show the player a total of zero.
type raScores struct {
	mu sync.Mutex
	m  map[string][2]int
}

func (s *raScores) set(user string, score, softcore int) {
	s.mu.Lock()
	if s.m == nil {
		s.m = map[string][2]int{}
	}
	s.m[user] = [2]int{score, softcore}
	s.mu.Unlock()
}

func (s *raScores) get(user string) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.m[user]
	return v[0], v[1]
}

// putRAEarning turns earning on for the caller's connected account.
func (s *Server) putRAEarning(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		writeError(w, http.StatusConflict, "conflict", "RetroAchievements is not available on this server")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.Password == "" || len(req.Password) > 256 {
		writeError(w, http.StatusBadRequest, "bad_request", "your RetroAchievements password is needed, once, to turn earning on")
		return
	}
	user := s.userID(r)
	acct, err := s.st.GetRetroAccount(r.Context(), user)
	if err != nil {
		s.writeInternal(w, err, "read retroachievements account")
		return
	}
	if acct == nil {
		writeError(w, http.StatusConflict, "conflict", "connect your RetroAchievements account first")
		return
	}
	token, name, err := s.ra.Client.Login(r.Context(), acct.RAUser, req.Password)
	if errors.Is(err, ra.ErrBadPassword) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", "RetroAchievements could not be reached")
		return
	}
	if name != "" && !strings.EqualFold(name, acct.RAUser) {
		// RetroAchievements signed in somebody other than the connected
		// account. Keep nothing rather than earn for the wrong person.
		writeError(w, http.StatusBadGateway, "upstream", "RetroAchievements signed in a different account")
		return
	}
	sealed, err := s.ra.Sealer.Seal([]byte(token))
	if err != nil {
		s.writeInternal(w, err, "seal retroachievements token")
		return
	}
	if err := s.st.SetRetroConnectToken(r.Context(), user, sealed); err != nil {
		s.writeInternal(w, err, "save retroachievements token")
		return
	}
	s.log.Info("retroachievements: earning turned on", "user", user)
	s.getRetroAchievements(w, r)
}

// deleteRAEarning turns earning off: the token, and anything queued.
func (s *Server) deleteRAEarning(w http.ResponseWriter, r *http.Request) {
	user := s.userID(r)
	err := s.st.SetRetroConnectToken(r.Context(), user, nil)
	if err != nil && !errors.Is(err, store.ErrNoRetroAccount) {
		s.writeInternal(w, err, "turn retroachievements earning off")
		return
	}
	if err == nil {
		s.log.Info("retroachievements: earning turned off", "user", user)
	}
	w.WriteHeader(http.StatusNoContent)
}

// raDoor is POST /api/items/{id}/retroachievements.
func (s *Server) raDoor(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		writeError(w, http.StatusConflict, "conflict", "RetroAchievements is not available on this server")
		return
	}
	if s.romFor(w, r) == nil {
		return
	}
	user := s.userID(r)
	acct, err := s.st.GetRetroAccount(r.Context(), user)
	if err != nil {
		s.writeInternal(w, err, "read retroachievements account")
		return
	}
	if acct == nil || len(acct.ConnectTokenSealed) == 0 {
		writeError(w, http.StatusConflict, "conflict", "earning achievements is off for this account")
		return
	}
	ua := r.UserAgent()
	if !ra.ValidUserAgent(ua) {
		writeError(w, http.StatusBadRequest, "bad_request", "the player's user agent must name LANcast and the core, as RetroAchievements requires")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDoorBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "the request could not be read")
		return
	}
	if len(body) > maxDoorBody {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "that is larger than anything rcheevos sends")
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "the request is not a form")
		return
	}
	request, err := ra.Admit(form, acct.RAUser)
	var refused *ra.DoorError
	if errors.As(err, &refused) {
		s.log.Warn("retroachievements: request refused", "user", user, "request", form.Get("r"), "reason", refused.Message)
		code := "forbidden"
		if refused.Status == http.StatusBadRequest {
			code = "bad_request"
		}
		writeError(w, refused.Status, code, refused.Message)
		return
	}
	token, err := s.ra.Sealer.Open(acct.ConnectTokenSealed)
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", "earning was turned on on another server or before a restore; turn it on again here")
		return
	}

	if ra.Earned(request) {
		n, err := s.st.RetroQueueCount(r.Context(), user)
		if err != nil {
			s.writeInternal(w, err, "count retroachievements queue")
			return
		}
		if n > 0 {
			s.raEnqueue(w, r, acct, request, form, ua, "behind earlier unlocks")
			return
		}
	}

	reply, err := s.ra.Client.Connect(r.Context(), ra.WithToken(form, acct.RAUser, string(token)), ua)
	if err != nil || reply.Retry() {
		if ra.Earned(request) {
			s.raEnqueue(w, r, acct, request, form, ua, "RetroAchievements did not take it")
			return
		}
		writeError(w, http.StatusBadGateway, "upstream", "RetroAchievements could not be reached")
		return
	}
	if request == "login2" || request == "awardachievement" {
		if score, soft, ok := ra.Scores(reply.Body); ok {
			s.raPoints.set(user, score, soft)
		}
	}
	out := reply.Body
	if request == "login2" {
		out = ra.HideToken(out)
	}
	if request == "awardachievement" && reply.Taken() {
		// The profile's view is cached for ten minutes; an unlock is the
		// moment it is most likely to be looked at.
		s.raCache.forget(user)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(reply.Status)
	_, _ = w.Write(out)
}

// raEnqueue keeps an earned request and tells the player it succeeded.
func (s *Server) raEnqueue(w http.ResponseWriter, r *http.Request, acct *store.RetroAccount, request string, form url.Values, ua, why string) {
	user := acct.UserID
	if _, err := s.st.EnqueueRetro(r.Context(), store.RetroQueued{
		UserID: user, RAUser: acct.RAUser, Request: request,
		Form: ra.WithoutToken(form).Encode(), UserAgent: ua,
	}); err != nil {
		s.writeInternal(w, err, "queue retroachievements unlock")
		return
	}
	s.log.Info("retroachievements: queued", "user", user, "request", request, "why", why)
	score, soft := s.raPoints.get(user)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(ra.QueuedReply(form, score, soft))
}

// RunRetroQueue sends queued unlocks until ctx ends: once at start, so a
// restart doesn't wait a minute to send what an outage left behind, then
// every minute, backing off while RetroAchievements is not answering.
func (s *Server) RunRetroQueue(ctx context.Context) {
	if s.ra == nil {
		return
	}
	wait := raQueueIdle
	for {
		if s.sendRetroQueue(ctx) {
			if wait *= 2; wait > raQueueMaxWait {
				wait = raQueueMaxWait
			}
		} else {
			wait = raQueueIdle
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

/*
 * sendRetroQueue tries everything queued, oldest first, and reports whether
 * any of it is still waiting on RetroAchievements.
 *
 * Per account, in order: once one of an account's rows can't be sent, its
 * later rows wait too, so a leaderboard entry never overtakes the unlock
 * before it. Other accounts carry on.
 *
 * A row is dropped, not sent, when its account no longer earns as the
 * RetroAchievements user it was made as: turned off, disconnected, or
 * connected to somebody else since. A row RetroAchievements turns down for
 * good is dropped too, with the reason in the log; trying it again would be
 * turned down the same way, and would hold up everything behind it.
 */
func (s *Server) sendRetroQueue(ctx context.Context) (waiting bool) {
	rows, err := s.st.RetroQueue(ctx, raQueueBatch)
	if err != nil {
		s.log.Warn("retroachievements: queue unreadable", "error", err)
		return true
	}
	held := map[string]bool{}
	for _, q := range rows {
		if ctx.Err() != nil {
			return true
		}
		if held[q.UserID] {
			continue
		}
		acct, err := s.st.GetRetroAccount(ctx, q.UserID)
		if err != nil {
			held[q.UserID], waiting = true, true
			continue
		}
		if acct == nil || len(acct.ConnectTokenSealed) == 0 || !strings.EqualFold(acct.RAUser, q.RAUser) {
			s.log.Info("retroachievements: queued unlock dropped; the account no longer earns as "+q.RAUser, "user", q.UserID, "request", q.Request)
			_ = s.st.DeleteRetroQueued(ctx, q.ID)
			continue
		}
		token, err := s.ra.Sealer.Open(acct.ConnectTokenSealed)
		if err != nil {
			_ = s.st.NoteRetroAttempt(ctx, q.ID, "the token can't be read on this server")
			held[q.UserID], waiting = true, true
			continue
		}
		form, err := url.ParseQuery(q.Form)
		if err == nil {
			form, err = ra.Delayed(form, time.Now().Unix()-q.CreatedAt)
		}
		if err != nil {
			s.log.Warn("retroachievements: queued unlock unreadable, dropped", "user", q.UserID, "error", err)
			_ = s.st.DeleteRetroQueued(ctx, q.ID)
			continue
		}
		reply, err := s.ra.Client.Connect(ctx, ra.WithToken(form, acct.RAUser, string(token)), q.UserAgent)
		switch {
		case err != nil || reply.Retry():
			problem := "RetroAchievements did not answer"
			if err == nil {
				problem = "RetroAchievements answered " + http.StatusText(reply.Status)
			}
			_ = s.st.NoteRetroAttempt(ctx, q.ID, problem)
			held[q.UserID], waiting = true, true
		case reply.Taken():
			_ = s.st.DeleteRetroQueued(ctx, q.ID)
			s.raCache.forget(q.UserID)
			s.log.Info("retroachievements: queued unlock sent", "user", q.UserID, "request", q.Request,
				"waited", time.Since(time.Unix(q.CreatedAt, 0)).Round(time.Second))
		default:
			_ = s.st.DeleteRetroQueued(ctx, q.ID)
			s.log.Warn("retroachievements: queued unlock turned down", "user", q.UserID, "request", q.Request,
				"status", reply.Status, "reply", string(reply.Body[:min(len(reply.Body), 200)]))
		}
	}
	return waiting
}
