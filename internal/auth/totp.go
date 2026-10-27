package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

/*
 * Time-based one-time codes, RFC 6238 (ADR 0100).
 *
 * The parameters every authenticator app assumes and few let you change:
 * HMAC-SHA1, six digits, thirty-second steps. A code is accepted for the step
 * before and after the current one as well, because a phone's clock and a
 * server's drift, and a code typed in the last second of its step arrives in
 * the next. A step, once used, is never accepted again for that account.
 *
 * Written here rather than taken as a dependency: it is forty lines of the
 * standard library and a published test vector, and it sits on the sign-in
 * path.
 */

const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	totpSkew   = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret makes a secret: 20 random bytes, the size RFC 4226 recommends.
func NewTOTPSecret() ([]byte, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("totp secret: %w", err)
	}
	return b, nil
}

// TOTPKey is the secret as people type it into an app: base32, no padding,
// in groups of four so it can be read off a screen.
func TOTPKey(secret []byte) string {
	raw := b32.EncodeToString(secret)
	var groups []string
	for i := 0; i < len(raw); i += 4 {
		end := i + 4
		if end > len(raw) {
			end = len(raw)
		}
		groups = append(groups, raw[i:end])
	}
	return strings.Join(groups, " ")
}

// TOTPURL is the otpauth:// link an authenticator app opens.
func TOTPURL(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", b32.EncodeToString(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// totpAt is the code for one step.
func totpAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// TOTPCode is the code an app would show at t. The server never needs it —
// it only verifies — but tests and tools stand in for the app with it.
func TOTPCode(secret []byte, t time.Time) string { return totpAt(secret, TOTPStep(t)) }

// TOTPStep is the step a time falls in.
func TOTPStep(t time.Time) int64 { return t.Unix() / int64(totpStep/time.Second) }

/*
 * VerifyTOTP checks a code typed at `now`. It returns the step the code
 * matched, so the caller can record it and refuse that step next time, and
 * refuses any step at or before `lastUsed` — a code seen once is spent, even
 * inside its thirty seconds, which is what stops a code read over a shoulder
 * being used a second time.
 *
 * Spaces in what was typed are ignored; anything that is not six digits
 * never matches. Every candidate is compared in constant time.
 */
func VerifyTOTP(secret []byte, code string, now time.Time, lastUsed int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	cur := TOTPStep(now)
	matched, ok := int64(0), false
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := cur + d
		if subtle.ConstantTimeCompare([]byte(totpAt(secret, step)), []byte(code)) == 1 && step > lastUsed {
			matched, ok = step, true
		}
	}
	return matched, ok
}

// NewRecoveryCodes makes n one-time codes like "k7m2-9qxd": ten characters
// from an alphabet with no look-alikes, enough that guessing one is hopeless.
func NewRecoveryCodes(n int) ([]string, error) {
	const alphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	out := make([]string, n)
	buf := make([]byte, 8)
	for i := range out {
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		var sb strings.Builder
		for j, b := range buf {
			if j == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(b)%len(alphabet)])
		}
		out[i] = sb.String()
	}
	return out, nil
}

// NormalizeRecoveryCode puts a typed recovery code in the stored form, so a
// code typed in capitals or without its dash still matches.
func NormalizeRecoveryCode(code string) string {
	c := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), " ", ""), "-", ""))
	if len(c) != 8 {
		return ""
	}
	return c[:4] + "-" + c[4:]
}
