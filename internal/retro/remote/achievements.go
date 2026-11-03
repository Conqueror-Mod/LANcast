package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

/*
 * Achievements sends one request rcheevos built to the server, which sends
 * it on to RetroAchievements with the person's token (ADR 0112). It is a
 * cheevos.Sender.
 *
 * The answer is put back into the shape rcheevos understands. RetroAchievements'
 * own answer comes through as it came. The server's refusals become
 * RetroAchievements-shaped ones, so rcheevos reports them rather than
 * retrying them forever: earning switched off reads as a failed sign-in, and
 * a request the server would not send reads as a request refused. Only an
 * unreachable server, or one that could not reach RetroAchievements, is an
 * error, which rcheevos tries again later.
 */
func (g *Game) Achievements(ctx context.Context, body, userAgentClause string) (int, []byte, error) {
	return g.AchievementsAs(ctx, body, userAgentClause, "")
}

// AchievementsAs is Achievements with the whole User-Agent the player built
// (ADR 0112 §3), which the server sends if it has the expected shape.
func (g *Game) AchievementsAs(ctx context.Context, body, userAgentClause, userAgent string) (int, []byte, error) {
	payload, err := json.Marshal(struct {
		Body   string `json:"body"`
		Clause string `json:"user_agent_clause"`
		Agent  string `json:"user_agent,omitempty"`
	}{body, userAgentClause, userAgent})
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url("/api/items/"+g.id()+"/retroachievements", nil), bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Ticket "+g.ticket)
	req.Header.Set("User-Agent", "LANcast-Client (retro)")
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	if resp.StatusCode == http.StatusOK {
		var ans struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		}
		if err := json.Unmarshal(raw, &ans); err != nil || ans.Status == 0 {
			return 0, nil, errors.New("retro remote: the server's achievements answer was unreadable")
		}
		return ans.Status, []byte(ans.Body), nil
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	msg := env.Error.Message
	if msg == "" {
		msg = resp.Status
	}
	switch {
	case resp.StatusCode == http.StatusConflict:
		return http.StatusUnauthorized, raRefusal(msg, "not_authorized"), nil
	case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests:
		return 0, nil, fmt.Errorf("retro remote: achievements: %s", msg)
	default:
		return resp.StatusCode, raRefusal(msg, ""), nil
	}
}

// raRefusal is a refusal in RetroAchievements' own JSON shape.
func raRefusal(msg, code string) []byte {
	b, _ := json.Marshal(struct {
		Success bool   `json:"Success"`
		Error   string `json:"Error"`
		Code    string `json:"Code,omitempty"`
	}{false, msg, code})
	return b
}
