package auth

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// testBackoff returns a backoff on a clock the test moves by hand.
func testBackoff() (*AccountBackoff, *time.Time) {
	b := NewAccountBackoff()
	clock := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return clock }
	return b, &clock
}

func TestBackoffFreeFailuresCostNothing(t *testing.T) {
	b, _ := testBackoff()
	for i := 0; i < b.Free; i++ {
		if ok, _ := b.Allow("dana"); !ok {
			t.Fatalf("attempt %d refused inside the free allowance", i+1)
		}
		b.Fail("dana")
	}
	// The fifth failure is still free; the wait starts on the sixth.
	if ok, _ := b.Allow("dana"); !ok {
		t.Fatal("refused after only the free failures")
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	b, clock := testBackoff()
	for i := 0; i < b.Free; i++ {
		b.Fail("dana")
	}
	want := []time.Duration{
		30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute,
		8 * time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute,
	}
	for i, w := range want {
		b.Fail("dana")
		ok, left := b.Allow("dana")
		if ok || left != w {
			t.Fatalf("failure %d past free: allowed=%v wait=%v, want refused for %v", i+1, ok, left, w)
		}
		// Serve the wait, so the next failure is a counted one.
		*clock = clock.Add(w)
		if ok, _ := b.Allow("dana"); !ok {
			t.Fatalf("still refused after serving a %v wait", w)
		}
	}
}

// The owner retrying during an attack must not make their own wait longer:
// refused attempts are never passed to Fail by the caller, and Allow itself
// counts nothing.
func TestBackoffRefusedAttemptsDoNotExtendTheWait(t *testing.T) {
	b, clock := testBackoff()
	// Far enough in that the wait (4 minutes) outlasts the asking below.
	for i := 0; i < b.Free+4; i++ {
		b.Fail("dana")
	}
	_, first := b.Allow("dana")
	for i := 0; i < 50; i++ {
		*clock = clock.Add(time.Second)
		b.Allow("dana")
	}
	_, left := b.Allow("dana")
	if left != first-50*time.Second {
		t.Fatalf("wait moved from %v to %v under repeated asking", first, left)
	}
}

func TestBackoffNamesFoldCase(t *testing.T) {
	b, _ := testBackoff()
	for i := 0; i < b.Free+1; i++ {
		b.Fail("Dana")
	}
	if ok, _ := b.Allow(" dana "); ok {
		t.Fatal("a differently cased name escaped the count; the user table is NOCASE")
	}
}

// A 429 must not reveal whether an account exists, so a name nobody has is
// made to wait exactly like a real one. The backoff never sees the user table,
// which is what guarantees it.
func TestBackoffTreatsEveryNameAlike(t *testing.T) {
	b, _ := testBackoff()
	for i := 0; i < b.Free+1; i++ {
		b.Fail("dana")
		b.Fail("nobody-by-this-name")
	}
	_, a := b.Allow("dana")
	_, n := b.Allow("nobody-by-this-name")
	if a != n || a == 0 {
		t.Fatalf("real name waits %v, invented name waits %v; must be equal and non-zero", a, n)
	}
}

func TestBackoffResetOnSuccess(t *testing.T) {
	b, _ := testBackoff()
	for i := 0; i < b.Free+3; i++ {
		b.Fail("dana")
	}
	b.Reset("dana")
	if ok, _ := b.Allow("dana"); !ok {
		t.Fatal("a correct password did not clear the wait")
	}
}

func TestBackoffForgetsAfterQuiet(t *testing.T) {
	b, clock := testBackoff()
	for i := 0; i < b.Free+3; i++ {
		b.Fail("dana")
	}
	*clock = clock.Add(b.Forget)
	if ok, _ := b.Allow("dana"); !ok {
		t.Fatal("an hour without failures did not clear the count")
	}
	// And the count really restarted: the next failure is free again.
	b.Fail("dana")
	if ok, _ := b.Allow("dana"); !ok {
		t.Fatal("first failure after forgetting was not free")
	}
}

// A spray of invented names must not push the account under attack out of
// memory, which would reset its wait.
func TestBackoffSprayCannotEvictTheTarget(t *testing.T) {
	b, _ := testBackoff()
	b.Cap = 100
	for i := 0; i < b.Free+4; i++ {
		b.Fail("dana")
	}
	for i := 0; i < 1000; i++ {
		b.Fail(fmt.Sprintf("spray-%d", i))
	}
	if len(b.entries) > b.Cap {
		t.Fatalf("%d entries, cap %d", len(b.entries), b.Cap)
	}
	if ok, _ := b.Allow("dana"); ok {
		t.Fatal("the targeted account was evicted by a spray and its wait reset")
	}
}

func TestInsideNetwork(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:5000":      true,
		"[::1]:5000":          true,
		"192.168.1.20:5000":   true,
		"10.0.0.4:5000":       true,
		"172.16.9.9:5000":     true,
		"[fd12:3456::1]:5000": true,
		"[fe80::1]:5000":      true,
		"203.0.113.5:5000":    false,
		"[2001:db8::1]:5000":  false,
		"not-an-address":      false,
	}
	for addr, want := range cases {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.RemoteAddr = addr
		// A forwarded header claiming to be inside must change nothing.
		r.Header.Set("X-Forwarded-For", "192.168.1.1")
		if got := InsideNetwork(r); got != want {
			t.Errorf("%s: inside=%v, want %v", addr, got, want)
		}
	}
}
