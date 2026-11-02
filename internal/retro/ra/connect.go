package ra

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/*
 * Earning (ADR 0112): RetroAchievements' Connect API, dorequest.php, which
 * rcheevos speaks. The desktop player's rcheevos makes these requests, and
 * the LANcast server forwards them — this file is the forwarding's rules,
 * kept pure so each is tested without a server or the network.
 *
 * Taken from the pinned rcheevos (v12.5.0, src/rapi), not from memory:
 *
 *   - every request is a form POST to /dorequest.php with r=<request>;
 *   - the signed-in ones carry u=<username> and t=<token>;
 *   - awardachievement and submitlbentry carry v, an MD5 signature over the
 *     id, the username, hardcore or the score, and the delay — not the
 *     token, so swapping the token leaves it valid;
 *   - a delayed unlock says how long ago in o, which is in the signature.
 */

// ConnectPath is where Connect requests go.
const ConnectPath = "/dorequest.php"

// PlaceholderToken is what the desktop player logs rcheevos in with. The
// real token never leaves the server; the server swaps it in on each request
// it forwards, and swaps this back into anything that would reveal it.
const PlaceholderToken = "lancast"

// ErrBadPassword is RetroAchievements turning a login down.
var ErrBadPassword = errors.New("RetroAchievements did not accept that username and password")

// doorRequests are the requests rcheevos makes in play, and the only ones
// forwarded: logging in, identifying the game and loading its sets, the
// session and its pings, and the two things a player earns. This is not a
// general proxy, so nothing an editor or a site page would ask is here.
var doorRequests = map[string]bool{
	"login2":           true,
	"gameid":           true,
	"achievementsets":  true,
	"startsession":     true,
	"ping":             true,
	"awardachievement": true,
	"submitlbentry":    true,
}

// Earned reports whether a request is something a player earns: what the
// server keeps in its queue when RetroAchievements can't take it.
func Earned(request string) bool {
	return request == "awardachievement" || request == "submitlbentry"
}

// userAgent is the format ADR 0112 promises RetroAchievements:
// "LANcast/<version> (Windows <version>) <core>/<version>", with rcheevos'
// own clause after it.
var userAgent = regexp.MustCompile(`^LANcast/[0-9A-Za-z.+-]+ \(Windows [0-9.]+\) [0-9A-Za-z._+-]+/[0-9A-Za-z._+-]+( rcheevos/[0-9.]+)?$`)

// ValidUserAgent reports whether ua is one the server forwards.
func ValidUserAgent(ua string) bool { return len(ua) <= 200 && userAgent.MatchString(ua) }

// DoorError is a request the door refuses, with why.
type DoorError struct {
	Status  int
	Message string
}

func (e *DoorError) Error() string { return e.Message }

/*
 * Admit checks a request rcheevos made, as the server received it, and
 * returns the request type. raUser is the connected account's name: the
 * player may only ever speak as that account.
 *
 * Hardcore is refused outright (ADR 0112 section 5): until RetroAchievements
 * approves LANcast it would not count, and a request that says h=1 is a
 * player claiming something this server cannot vouch for.
 */
func Admit(form url.Values, raUser string) (string, error) {
	r := form.Get("r")
	if !doorRequests[r] {
		return "", &DoorError{http.StatusForbidden, fmt.Sprintf("%q is not a request LANcast forwards to RetroAchievements", r)}
	}
	if r == "gameid" {
		// Looking a hash up is the one request rcheevos makes signed out:
		// r and m, no name and no token. Nothing is added to it either.
		if form.Has("u") || form.Has("t") || form.Has("p") {
			return "", &DoorError{http.StatusBadRequest, "a game lookup carries no account"}
		}
		return r, nil
	}
	if u := form.Get("u"); !strings.EqualFold(u, raUser) {
		return "", &DoorError{http.StatusForbidden, "that request is not for the RetroAchievements account connected here"}
	}
	if t := form.Get("t"); t != "" && t != PlaceholderToken {
		return "", &DoorError{http.StatusBadRequest, "the player sent a token of its own; it should send the placeholder"}
	}
	if form.Has("p") {
		return "", &DoorError{http.StatusBadRequest, "a password never goes through the player; turn earning on from your profile"}
	}
	if form.Get("h") == "1" {
		return "", &DoorError{http.StatusForbidden, "hardcore isn't available until RetroAchievements approves LANcast"}
	}
	return r, nil
}

// WithToken is the form as it goes to RetroAchievements: the account's name
// as RetroAchievements spells it, and the real token.
func WithToken(form url.Values, raUser, token string) url.Values {
	out := cloneForm(form)
	if form.Get("r") == "gameid" {
		return out
	}
	out.Set("u", raUser)
	out.Set("t", token)
	return out
}

// WithoutToken is the form as the queue keeps it.
func WithoutToken(form url.Values) url.Values {
	out := cloneForm(form)
	out.Del("t")
	return out
}

func cloneForm(form url.Values) url.Values {
	out := make(url.Values, len(form))
	for k, v := range form {
		out[k] = append([]string(nil), v...)
	}
	return out
}

/*
 * Delayed adds waited seconds to an earned request that sat in the queue, so
 * RetroAchievements dates it when it happened, and signs it again: o is part
 * of v. The signature is rcheevos' own (rc_api_runtime.c), reproduced here
 * and tested against a value rcheevos computed.
 */
func Delayed(form url.Values, waited int64) (url.Values, error) {
	if waited <= 0 {
		return form, nil
	}
	out := cloneForm(form)
	var prior int64
	if o := form.Get("o"); o != "" {
		n, err := strconv.ParseInt(o, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("ra: a delay of %q", o)
		}
		prior = n
	}
	delay := prior + waited
	out.Set("o", strconv.FormatInt(delay, 10))
	v, err := Signature(out)
	if err != nil {
		return nil, err
	}
	out.Set("v", v)
	return out, nil
}

// Signature is v for an award or a leaderboard entry, from its own fields.
func Signature(form url.Values) (string, error) {
	h := md5.New()
	user := form.Get("u")
	o := form.Get("o")
	delayed := o != "" && o != "0"
	switch form.Get("r") {
	case "awardachievement":
		a := form.Get("a")
		hc := "0"
		if form.Get("h") == "1" {
			hc = "1"
		}
		io.WriteString(h, a+user+hc)
		if delayed {
			// rcheevos adds the id a second time for a delayed unlock.
			io.WriteString(h, a+o)
		}
	case "submitlbentry":
		io.WriteString(h, form.Get("i")+user+form.Get("s"))
		if delayed {
			io.WriteString(h, o)
		}
	default:
		return "", fmt.Errorf("ra: %q is not signed", form.Get("r"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// tokenField matches the token in a login reply.
var tokenField = regexp.MustCompile(`("Token"\s*:\s*)"(?:[^"\\]|\\.)*"`)

// HideToken puts the placeholder where a reply carries the real token, so
// it reaches no player. Only login replies carry one.
func HideToken(body []byte) []byte {
	return tokenField.ReplaceAll(body, []byte(`${1}"`+PlaceholderToken+`"`))
}

// QueuedReply is what the player is told when its unlock went into the
// server's queue rather than to RetroAchievements: success, because the
// server now holds it and will send it. rcheevos needs the id an award was
// for, and a leaderboard entry's score and rank fields, to read it as one.
// Score is the account's points as last heard, so its total isn't zeroed.
func QueuedReply(form url.Values, score, softcore int) []byte {
	var v any
	switch form.Get("r") {
	case "awardachievement":
		id, _ := strconv.ParseUint(form.Get("a"), 10, 32)
		v = map[string]any{
			"Success": true, "AchievementID": id, "Score": score, "SoftcoreScore": softcore,
			"Queued": true,
		}
	case "submitlbentry":
		s, _ := strconv.ParseInt(form.Get("s"), 10, 64)
		v = map[string]any{
			"Success": true, "Queued": true,
			"Response": map[string]any{
				"Score": s, "BestScore": s,
				"RankInfo":   map[string]any{"Rank": 0, "NumEntries": "0"},
				"TopEntries": []any{},
			},
		}
	default:
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// Reply is what RetroAchievements said to a forwarded request.
type Reply struct {
	Status int
	Body   []byte
}

// Taken reports whether RetroAchievements accepted an earned request, or
// already had it, which is the same thing to the person who earned it.
func (r Reply) Taken() bool {
	if r.Status != http.StatusOK {
		return false
	}
	var v struct {
		Success bool   `json:"Success"`
		Error   string `json:"Error"`
	}
	if json.Unmarshal(r.Body, &v) != nil {
		return false
	}
	return v.Success || strings.HasPrefix(v.Error, "User already has")
}

// Retry reports whether a failed reply is worth trying again: an outage, not
// a refusal. Anything else RetroAchievements turned down will be turned down
// the same way next time.
func (r Reply) Retry() bool {
	return r.Status == http.StatusTooManyRequests || r.Status >= 500
}

// Scores reads the account's points from a login or award reply, when it
// carries them.
func Scores(body []byte) (score, softcore int, ok bool) {
	var v struct {
		Score         *int `json:"Score"`
		SoftcoreScore *int `json:"SoftcoreScore"`
	}
	if json.Unmarshal(body, &v) != nil || v.Score == nil || v.SoftcoreScore == nil {
		return 0, 0, false
	}
	return *v.Score, *v.SoftcoreScore, true
}

// defaultHTTP is the client Connect uses when the Client names none.
var defaultHTTP = &http.Client{Timeout: 15 * time.Second}

// maxReply bounds what is read from RetroAchievements: a game's sets are the
// largest reply, tens of kilobytes for a big game.
const maxReply = 8 << 20

// Connect sends one form to RetroAchievements as the player's user agent.
// An error means no reply at all — the network, not RetroAchievements.
func (c Client) Connect(ctx context.Context, form url.Values, userAgent string) (Reply, error) {
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ConnectPath, strings.NewReader(form.Encode()))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if userAgent == "" {
		userAgent = c.UserAgent
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	hc := c.HTTP
	if hc == nil {
		hc = defaultHTTP
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReply+1))
	if err != nil {
		return Reply{}, err
	}
	if len(body) > maxReply {
		return Reply{}, errors.New("ra: RetroAchievements sent more than LANcast reads")
	}
	return Reply{Status: resp.StatusCode, Body: bytes.TrimSpace(body)}, nil
}

// Login exchanges a password for a Connect token, once (ADR 0112 section 3).
// The password is sent and forgotten; the token is what is kept.
func (c Client) Login(ctx context.Context, user, password string) (token, name string, err error) {
	form := url.Values{"r": {"login2"}, "u": {user}, "p": {password}}
	reply, err := c.Connect(ctx, form, "")
	if err != nil {
		return "", "", err
	}
	var v struct {
		Success bool   `json:"Success"`
		User    string `json:"User"`
		Token   string `json:"Token"`
		Error   string `json:"Error"`
	}
	if json.Unmarshal(reply.Body, &v) != nil {
		return "", "", fmt.Errorf("RetroAchievements answered %d with something LANcast can't read", reply.Status)
	}
	if !v.Success || v.Token == "" {
		if reply.Status == http.StatusUnauthorized || reply.Status == http.StatusForbidden || reply.Status == http.StatusOK {
			return "", "", ErrBadPassword
		}
		return "", "", fmt.Errorf("RetroAchievements answered %d: %s", reply.Status, v.Error)
	}
	return v.Token, v.User, nil
}
