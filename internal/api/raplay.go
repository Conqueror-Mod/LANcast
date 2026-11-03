package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"lancast/internal/retro/ra"
)

/*
 * Earning achievements while playing (ADR 0112).
 *
 * The desktop player runs rcheevos, which builds every request an emulator
 * makes; it never sends one. It posts each to POST /api/items/{id}/
 * retroachievements, with the game's stream ticket, and this file sends it
 * on: checked (ra.Check), with the person's login token put in, and the token
 * taken out of the answer. The player logs in with a placeholder and never
 * holds the token.
 *
 * Softcore only until ra.HardcoreApproved: a request that claims hardcore
 * is refused here as well as never being made by the player.
 */

// putRetroPlay turns earning on: the person's RetroAchievements password,
// once, for a token. The password is not kept.
func (s *Server) putRetroPlay(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		writeError(w, http.StatusConflict, "conflict", "RetroAchievements is not available on this server")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "your RetroAchievements password is needed")
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
	name, token, err := s.ra.Client.Login(r.Context(), acct.RAUser, req.Password)
	switch {
	case errors.Is(err, ra.ErrLoginRefused):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	case !strings.EqualFold(name, acct.RAUser):
		// Names can change; this one did between connecting and now.
		writeError(w, http.StatusConflict, "conflict",
			"RetroAchievements now knows this account as "+name+". Connect again, then turn this on.")
		return
	}
	sealed, err := s.ra.Sealer.Seal([]byte(token))
	if err != nil {
		s.writeInternal(w, err, "seal retroachievements token")
		return
	}
	if err := s.st.SetRetroToken(r.Context(), user, sealed); err != nil {
		s.writeInternal(w, err, "save retroachievements token")
		return
	}
	s.raCache.forget(user)
	s.getRetroAchievements(w, r)
}

// deleteRetroPlay turns earning off. The token is deleted, and with it any
// unlocks still waiting to be sent, which nothing could send now.
func (s *Server) deleteRetroPlay(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SetRetroToken(r.Context(), s.userID(r), nil); err != nil {
		s.writeInternal(w, err, "turn off retroachievements earning")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// raConnectRequest is one request rcheevos built, as the player hands it on.
type raConnectRequest struct {
	Body string `json:"body"`
	// UserAgentClause is the clause rcheevos writes about itself
	// (rc_client_get_user_agent_clause). RetroAchievements asks for it.
	UserAgentClause string `json:"user_agent_clause"`
	// UserAgent is the whole User-Agent the player built, naming its core
	// (ADR 0112 §3). Sent as it is when ra.UserAgentOK; otherwise, and for a
	// player too old to send one, the server builds a shorter one.
	UserAgent string `json:"user_agent"`
}

// raConnectAnswer is RetroAchievements' answer, for the player to give to
// rcheevos as it came, with the token taken out.
type raConnectAnswer struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	// Queued is an award kept to send later (ADR 0112 §3): the player shows
	// it unlocked, and the profile counts it as waiting.
	Queued bool `json:"queued,omitempty"`
}

func (s *Server) raConnect(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		writeError(w, http.StatusConflict, "conflict", "RetroAchievements is not available on this server")
		return
	}
	if s.romFor(w, r) == nil {
		return
	}
	var req raConnectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	user := s.userID(r)
	acct, err := s.st.GetRetroAccount(r.Context(), user)
	if err != nil {
		s.writeInternal(w, err, "read retroachievements account")
		return
	}
	if acct == nil || len(acct.TokenSealed) == 0 {
		writeError(w, http.StatusConflict, "earning_off",
			"earning achievements is off; turn it on in your profile")
		return
	}
	form, kind, err := ra.Check(req.Body, acct.RAUser)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	token, err := s.ra.Sealer.Open(acct.TokenSealed)
	if err != nil {
		writeError(w, http.StatusConflict, "earning_off",
			"this server can't read your RetroAchievements token; turn earning on again in your profile")
		return
	}
	agent := s.raAgent(req.UserAgent, req.UserAgentClause)
	// Not the request's context: a player closing mid-award must not cancel
	// the award on its way to RetroAchievements.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	status, body, err := s.ra.Client.Do(ctx, ra.WithToken(form, string(token)), agent)
	if errors.Is(err, ra.ErrUnreachable) && kind == "awardachievement" {
		if qerr := s.st.QueueRetroUnlock(ctx, user, form.Encode()); qerr != nil {
			writeError(w, http.StatusBadGateway, "upstream", "RetroAchievements could not be reached, and the unlock could not be kept")
			return
		}
		s.log.Info("retroachievements unlock queued", "achievement", form.Get("a"))
		s.raCache.forget(user)
		writeJSON(w, http.StatusOK, raConnectAnswer{Status: http.StatusOK, Body: string(ra.QueuedAnswer(form.Get("a"))), Queued: true})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	if kind == "awardachievement" {
		s.raCache.forget(user)
	}
	writeJSON(w, http.StatusOK, raConnectAnswer{Status: status, Body: string(ra.RedactToken(body))})
}

// raAgent is the User-Agent RetroAchievements asks an emulator for: the
// player's own, when it has the expected shape, or else the emulator and
// version, then rcheevos' clause about itself.
func (s *Server) raAgent(player, clause string) string {
	if ra.UserAgentOK(player) {
		return player
	}
	clause = strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, clause)
	if len(clause) > 120 {
		clause = clause[:120]
	}
	agent := "LANcast/" + Version + " (Windows)"
	if clause != "" {
		agent += " " + clause
	}
	return agent
}

// raQueueEvery is how often the unlock queue is looked at.
const raQueueEvery = time.Minute

/*
 * RunRetroUnlockQueue sends awards kept while RetroAchievements could not be
 * reached (ADR 0112 §3), until ctx ends. Each goes with the time it waited
 * added to it, so the achievement is dated when it was earned. One that
 * RetroAchievements answers, yes or no, is done; one it still cannot be
 * reached for waits longer each time, up to six hours.
 */
func (s *Server) RunRetroUnlockQueue(ctx context.Context) {
	if s.ra == nil {
		return
	}
	t := time.NewTicker(raQueueEvery)
	defer t.Stop()
	for {
		s.sendRetroUnlocks(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) sendRetroUnlocks(ctx context.Context, now time.Time) {
	due, err := s.st.DueRetroUnlocks(ctx, now, 50)
	if err != nil {
		s.log.Warn("retroachievements queue unreadable", "error", err)
		return
	}
	for _, u := range due {
		if ctx.Err() != nil {
			return
		}
		acct, err := s.st.GetRetroAccount(ctx, u.UserID)
		if err != nil {
			continue
		}
		var token []byte
		if acct != nil && len(acct.TokenSealed) > 0 {
			token, err = s.ra.Sealer.Open(acct.TokenSealed)
		}
		if acct == nil || len(token) == 0 || err != nil {
			// Disconnected, or a token this server cannot open: nothing can
			// send it. Turning earning off clears these anyway.
			_ = s.st.DoneRetroUnlock(ctx, u.ID)
			continue
		}
		form, err := url.ParseQuery(u.Body)
		if err != nil {
			_ = s.st.DoneRetroUnlock(ctx, u.ID)
			continue
		}
		waited := now.Sub(time.Unix(u.QueuedAt, 0))
		// The queue keeps the request, not the player's User-Agent, so a
		// delayed award goes with the server's shorter one.
		_, body, err := s.ra.Client.Do(ctx, ra.WithToken(ra.Delay(form, waited), string(token)), s.raAgent("", ""))
		if errors.Is(err, ra.ErrUnreachable) {
			backoff := time.Minute << min(u.Tries, 9)
			if backoff > 6*time.Hour {
				backoff = 6 * time.Hour
			}
			_ = s.st.RetryRetroUnlock(ctx, u.ID, now.Add(backoff), err.Error())
			continue
		}
		var answer struct {
			Success bool   `json:"Success"`
			Error   string `json:"Error"`
		}
		_ = json.Unmarshal(body, &answer)
		if !answer.Success && !strings.HasPrefix(answer.Error, "User already has") {
			s.log.Warn("retroachievements refused a queued unlock", "achievement", form.Get("a"), "error", answer.Error)
		} else {
			s.log.Info("retroachievements queued unlock sent", "achievement", form.Get("a"), "waited", waited.Round(time.Second))
		}
		_ = s.st.DoneRetroUnlock(ctx, u.ID)
		s.raCache.forget(u.UserID)
	}
}
