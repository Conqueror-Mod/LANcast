package ra

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

/*
 * The connect API (ADR 0112): what an emulator uses to log in, load a game's
 * achievements and award them. Its credential is a login token, not the Web
 * API key the rest of this package uses, and the two cannot stand in for
 * each other.
 *
 * LANcast's player never calls this itself. It hands each request rcheevos
 * builds to the person's own server, which checks it (Check), puts the
 * person's token in, and sends it here (Do).
 */

// ConnectPath is where every connect request goes.
const ConnectPath = "/dorequest.php"

// Placeholder is the token the player logs in with, and the name it signs in
// as. The server swaps in the real ones on the way out, and takes the token
// back out of the answer, so the player never holds it.
const Placeholder = "lancast"

// ErrLoginRefused is a wrong RetroAchievements username or password.
var ErrLoginRefused = errors.New("RetroAchievements did not accept that username and password")

// Allowed are the request types rcheevos' rc_client sends for a softcore
// player (v12.5). Anything else, an editor upload among them, is refused.
// A leaderboard entry is allowed only with HardcoreApproved: rcheevos
// submits them in hardcore only.
var Allowed = map[string]bool{
	"login2":           true,
	"gameid":           true,
	"achievementsets":  true,
	"startsession":     true,
	"ping":             true,
	"awardachievement": true,
}

/*
 * HardcoreApproved is RetroAchievements' approval of LANcast as a hardcore
 * emulator (ADR 0112 §5), and the one switch for everything hardcore: the
 * player offers it, and the server forwards hardcore unlocks and leaderboard
 * entries, only while it is true.
 *
 * It is false, and changes only in a commit made after RetroAchievements has
 * said yes. A var rather than a const so that tests can see both sides.
 */
var HardcoreApproved = false

// allowed is Allowed with what HardcoreApproved adds.
func allowed(kind string) bool {
	return Allowed[kind] || (HardcoreApproved && kind == "submitlbentry")
}

// Login exchanges a username and password for a login token, once. The
// password goes to RetroAchievements in this one request and nowhere else.
func (c Client) Login(ctx context.Context, user, password string) (name, token string, err error) {
	form := url.Values{"r": {"login2"}, "u": {user}, "p": {password}}
	status, body, err := c.Do(ctx, form.Encode(), c.UserAgent)
	if err != nil {
		return "", "", err
	}
	var r struct {
		Success bool   `json:"Success"`
		User    string `json:"User"`
		Token   string `json:"Token"`
		Error   string `json:"Error"`
	}
	if json.Unmarshal(body, &r) != nil {
		return "", "", fmt.Errorf("RetroAchievements answered %d to a sign-in", status)
	}
	if !r.Success || r.Token == "" {
		if status == http.StatusUnauthorized || status == http.StatusForbidden || r.Error != "" {
			return "", "", ErrLoginRefused
		}
		return "", "", fmt.Errorf("RetroAchievements answered %d to a sign-in", status)
	}
	return r.User, r.Token, nil
}

// Do posts one connect request and returns RetroAchievements' answer as it
// came. An error means nothing usable came back: the network failed, or the
// server was unavailable (5xx), which the caller may treat as "try later".
func (c Client) Do(ctx context.Context, form, userAgent string) (int, []byte, error) {
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ConnectPath, strings.NewReader(form))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		// The form carries a token or a password: never put it in an error.
		return 0, nil, ErrUnreachable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, nil, ErrUnreachable
	}
	if resp.StatusCode >= 500 {
		return resp.StatusCode, body, ErrUnreachable
	}
	return resp.StatusCode, body, nil
}

// ErrUnreachable is RetroAchievements not answering usefully: try later.
var ErrUnreachable = errors.New("RetroAchievements could not be reached")

/*
 * Check reads a request the player sent and says whether LANcast will send
 * it for this person: one of the Allowed types, for the RetroAchievements
 * account they connected, softcore, and carrying no password. It returns
 * the form with the token removed, ready for WithToken.
 */
func Check(form, raUser string) (url.Values, string, error) {
	v, err := url.ParseQuery(form)
	if err != nil {
		return nil, "", errors.New("not a RetroAchievements request")
	}
	kind := v.Get("r")
	if !allowed(kind) {
		return nil, "", fmt.Errorf("LANcast does not send %q requests", kind)
	}
	if v.Has("p") {
		return nil, "", errors.New("the player never sends a password")
	}
	// The player does not know the person's RetroAchievements name, so it
	// signs in as the placeholder and the server names them. Every request
	// after that carries the name RetroAchievements answered with.
	if kind == "login2" && v.Get("u") == Placeholder {
		v.Set("u", raUser)
	}
	if !strings.EqualFold(v.Get("u"), raUser) {
		return nil, "", errors.New("that request is for a different RetroAchievements account")
	}
	if v.Get("h") != "" && v.Get("h") != "0" && !HardcoreApproved {
		return nil, "", errors.New("LANcast earns softcore achievements only")
	}
	v.Del("t")
	return v, kind, nil
}

// WithToken is the form with the person's token in it, encoded to send.
func WithToken(v url.Values, token string) string {
	w := url.Values{}
	for k, vs := range v {
		w[k] = vs
	}
	w.Set("t", token)
	return w.Encode()
}

// RedactToken replaces any Token in an answer with the placeholder, so the
// real one never reaches the player.
func RedactToken(body []byte) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	if _, ok := m["Token"]; !ok {
		return body
	}
	m["Token"], _ = json.Marshal(Placeholder)
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

/*
 * Delay adds the time an award spent waiting to its "seconds since the
 * unlock" (o), and signs it again, so RetroAchievements records when the
 * achievement was earned rather than when the internet came back. The
 * signature (v) is rcheevos' own: md5 of the achievement id, the user and
 * hardcore, then the id again and the seconds when there are any. It holds
 * no secret.
 */
func Delay(v url.Values, waited time.Duration) url.Values {
	secs := int64(waited / time.Second)
	if secs <= 0 {
		return v
	}
	if prev, err := strconv.ParseInt(v.Get("o"), 10, 64); err == nil && prev > 0 {
		secs += prev
	}
	out := url.Values{}
	for k, vs := range v {
		out[k] = vs
	}
	out.Set("o", strconv.FormatInt(secs, 10))
	out.Set("v", AwardSignature(v.Get("a"), v.Get("u"), v.Get("h"), secs))
	return out
}

// AwardSignature is rcheevos' v for an award.
func AwardSignature(id, user, hardcore string, secondsSince int64) string {
	if hardcore == "" {
		hardcore = "0"
	}
	s := id + user + hardcore
	if secondsSince > 0 {
		s += id + strconv.FormatInt(secondsSince, 10)
	}
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// QueuedAnswer is what the player is told about an award the server is
// keeping to send later: RetroAchievements' own success shape, so rcheevos
// treats it as unlocked.
func QueuedAnswer(achievementID string) []byte {
	id, _ := strconv.ParseUint(achievementID, 10, 32)
	b, _ := json.Marshal(map[string]any{"Success": true, "AchievementID": id})
	return b
}
