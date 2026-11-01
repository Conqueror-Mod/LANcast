package api

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"lancast/internal/subtitle"
)

/*
 * osLoginCache keeps an opensubtitles.com login token between requests
 * (ADR 0110).
 *
 * OpenSubtitles limits how often a client may log in, and a token lasts about
 * a day, so logging in for every download would spend the allowance it exists
 * to raise. The token lives in memory only: a restart logs in again, which is
 * one request, and nothing new is written to disk.
 *
 * Keyed by a hash of the key and the credentials, so changing any of them in
 * Settings is a fresh login rather than a token for somebody else.
 */
type osLoginCache struct {
	mu      sync.Mutex
	of      [32]byte
	token   string
	host    string
	expires time.Time
	// failedAt stops a wrong password being retried on every download.
	failedAt time.Time
}

// A token is good for 24 hours; renewing an hour early leaves room for a
// clock that disagrees.
const (
	osTokenLife     = 23 * time.Hour
	osLoginCooldown = 10 * time.Minute
)

// attachLogin gives the client the account's token if Settings holds a login,
// logging in when there is no fresh one. It reports a failure to log in, and
// the caller carries on without one: a download on the anonymous allowance is
// better than none.
func (s *Server) attachLogin(ctx context.Context, c *subtitle.OpenSubtitles, key string) error {
	cur := s.settings.Get()
	if cur.OpenSubtitlesUser == "" || cur.OpenSubtitlesPassword == "" {
		return nil
	}
	of := sha256.Sum256([]byte(key + "\x00" + cur.OpenSubtitlesUser + "\x00" + cur.OpenSubtitlesPassword))

	l := &s.osLogin
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.of == of && l.token != "" && now.Before(l.expires) {
		c.SetToken(l.token)
		c.SetAccountHost(l.host)
		return nil
	}
	if l.of == of && !l.failedAt.IsZero() && now.Sub(l.failedAt) < osLoginCooldown {
		return subtitle.ErrLoginRejected
	}

	l.of, l.token, l.host, l.failedAt = of, "", "", time.Time{}
	token, err := c.Login(ctx, cur.OpenSubtitlesUser, cur.OpenSubtitlesPassword)
	if err != nil {
		l.failedAt = now
		return err
	}
	l.token, l.host, l.expires = token, c.AccountHost(), now.Add(osTokenLife)
	return nil
}
