package passkey

import (
	"errors"
	"testing"
	"time"
)

func testChallenges() (*Challenges, *time.Time) {
	c := NewChallenges()
	clock := time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	return c, &clock
}

func TestAChallengeIsUsedOnce(t *testing.T) {
	c, _ := testChallenges()
	id, ch, err := c.Issue(PurposeRegister, "u_1")
	if err != nil {
		t.Fatal(err)
	}
	got, user, ok := c.Take(id, PurposeRegister)
	if !ok || string(got) != string(ch) || user != "u_1" {
		t.Fatalf("take: %v %q %v", got, user, ok)
	}
	if _, _, ok := c.Take(id, PurposeRegister); ok {
		t.Fatal("a challenge was taken twice")
	}
}

func TestAChallengeIsBoundToItsPurpose(t *testing.T) {
	c, _ := testChallenges()
	id, _, _ := c.Issue(PurposeLogin, "")
	if _, _, ok := c.Take(id, PurposeRegister); ok {
		t.Fatal("a sign-in challenge completed a registration")
	}
	// And trying it the wrong way spent it.
	if _, _, ok := c.Take(id, PurposeLogin); ok {
		t.Fatal("a misused challenge survived")
	}
}

func TestAChallengeExpires(t *testing.T) {
	c, clock := testChallenges()
	id, _, _ := c.Issue(PurposeLogin, "")
	*clock = clock.Add(c.TTL)
	if _, _, ok := c.Take(id, PurposeLogin); ok {
		t.Fatal("an expired challenge was accepted")
	}
}

// The sign-in route is public, so the store is bounded; expired entries make
// room before anything is refused.
func TestTheStoreIsBounded(t *testing.T) {
	c, clock := testChallenges()
	c.Cap = 3
	for i := 0; i < 3; i++ {
		if _, _, err := c.Issue(PurposeLogin, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := c.Issue(PurposeLogin, ""); !errors.Is(err, ErrTooManyChallenges) {
		t.Fatalf("a full store issued another: %v", err)
	}
	*clock = clock.Add(c.TTL)
	if _, _, err := c.Issue(PurposeLogin, ""); err != nil {
		t.Fatalf("expired challenges did not make room: %v", err)
	}
}
