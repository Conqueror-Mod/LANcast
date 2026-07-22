package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("the hash contains the plaintext")
	}
	if !CheckPassword(hash, "correct horse battery") {
		t.Error("the correct password did not verify")
	}
	if CheckPassword(hash, "wrong password") {
		t.Error("an incorrect password verified")
	}

	// bcrypt salts, so the same password hashes differently every time.
	other, _ := HashPassword("correct horse battery")
	if other == hash {
		t.Error("two hashes of the same password are identical — the salt is not working")
	}
}

func TestShortPasswordRejected(t *testing.T) {
	if _, err := HashPassword("short"); err != ErrWeakPassword {
		t.Errorf("error = %v, want ErrWeakPassword", err)
	}
	if _, err := HashPassword(strings.Repeat("a", MinPasswordLength)); err != nil {
		t.Errorf("a password at the minimum length was rejected: %v", err)
	}
}

func TestCheckPasswordAgainstGarbageHash(t *testing.T) {
	if CheckPassword("not-a-bcrypt-hash", "anything") {
		t.Error("a malformed hash verified")
	}
	if CheckPassword("", "") {
		t.Error("an empty hash verified")
	}
}

func TestTokensAreUniqueAndHashed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		token, hash, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[token] {
			t.Fatal("duplicate token generated")
		}
		seen[token] = true

		if hash == token {
			t.Fatal("the stored hash equals the token — a stolen database would grant sessions")
		}
		if HashToken(token) != hash {
			t.Fatal("HashToken disagrees with NewToken")
		}
		if len(token) < 40 {
			t.Fatalf("token is only %d characters", len(token))
		}
	}
}

// SameSite=Strict is the first half of the CSRF defence; without it any page in
// the user's browser could issue authenticated requests to the server.
func TestCookieAttributes(t *testing.T) {
	c := Cookie("tok", time.Hour)
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly — script could read the session")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Error("cookie is not SameSite=Strict")
	}
	if c.Secure {
		t.Error("Secure is set, but LANcast serves plain HTTP; the cookie would never be sent")
	}
	if c.Path != "/" {
		t.Errorf("Path = %q", c.Path)
	}

	if cleared := ClearCookie(); cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Errorf("ClearCookie does not expire the cookie: %+v", cleared)
	}
}

func TestSameOriginRequest(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		refer  string
		host   string
		want   bool
	}{
		{"matching origin", "http://localhost:8080", "", "localhost:8080", true},
		{"cross-site origin", "https://evil.example", "", "localhost:8080", false},
		{"different port is a different origin", "http://localhost:9999", "", "localhost:8080", false},
		{"referer used when origin absent", "", "http://localhost:8080/", "localhost:8080", true},
		{"cross-site referer", "", "https://evil.example/x", "localhost:8080", false},
		{"no headers at all is allowed", "", "", "localhost:8080", true},
		{"junk origin", "not-a-url", "", "localhost:8080", false},
		{"LAN address matches", "http://192.168.1.66:8080", "", "192.168.1.66:8080", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/libraries", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.refer != "" {
				r.Header.Set("Referer", tt.refer)
			}
			if got := SameOriginRequest(r); got != tt.want {
				t.Errorf("SameOriginRequest = %v, want %v", got, tt.want)
			}
		})
	}
}

// A single shared password is one guessable secret, so unlimited attempts
// against it is the entire attack.
func TestThrottle(t *testing.T) {
	th := NewThrottle()
	th.Max = 3

	for i := 0; i < 3; i++ {
		if !th.Allow("1.2.3.4") {
			t.Fatalf("attempt %d was blocked before the limit", i+1)
		}
	}
	if th.Allow("1.2.3.4") {
		t.Error("the limit was not enforced")
	}

	// Throttling is per client, so one attacker cannot lock out the owner.
	if !th.Allow("5.6.7.8") {
		t.Error("a different client was blocked by another client's attempts")
	}

	th.Reset("1.2.3.4")
	if !th.Allow("1.2.3.4") {
		t.Error("Reset did not clear the counter")
	}
}

func TestThrottleWindowExpires(t *testing.T) {
	th := NewThrottle()
	th.Max = 1
	th.Window = 20 * time.Millisecond

	th.Allow("host")
	if th.Allow("host") {
		t.Fatal("the limit was not enforced")
	}
	time.Sleep(40 * time.Millisecond)
	if !th.Allow("host") {
		t.Error("the window did not expire; failures must decay on their own")
	}
}

// Forwarded headers are attacker-controlled unless a trusted proxy sets them,
// so trusting them would let anyone reset their own counter.
func TestClientKeyIgnoresForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.RemoteAddr = "192.168.1.50:54321"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")

	if got := ClientKey(r); got != "192.168.1.50" {
		t.Errorf("ClientKey = %q, want the remote address only", got)
	}
}
