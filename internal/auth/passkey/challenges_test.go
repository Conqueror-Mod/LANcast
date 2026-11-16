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
	id, ch, err := c.Issue(PurposeRegister, "u_1", "10.0.0.1")
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
	id, _, _ := c.Issue(PurposeLogin, "", "10.0.0.1")
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
	id, _, _ := c.Issue(PurposeLogin, "", "10.0.0.1")
	*clock = clock.Add(c.TTL)
	if _, _, ok := c.Take(id, PurposeLogin); ok {
		t.Fatal("an expired challenge was accepted")
	}
}

// Registrations are bounded with a refusal; expired entries make room before
// anything is refused.
func TestTheRegistrationBudgetIsBounded(t *testing.T) {
	c, clock := testChallenges()
	c.Cap = 3
	for i := 0; i < 3; i++ {
		if _, _, err := c.Issue(PurposeRegister, "u_1", "10.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := c.Issue(PurposeRegister, "u_1", "10.0.0.1"); !errors.Is(err, ErrTooManyChallenges) {
		t.Fatalf("a full store issued another: %v", err)
	}
	*clock = clock.Add(c.TTL)
	if _, _, err := c.Issue(PurposeRegister, "u_1", "10.0.0.1"); err != nil {
		t.Fatalf("expired challenges did not make room: %v", err)
	}
}

// One address filling the sign-in budget pushes out its own challenges, not
// somebody else's, and is never refused either (R01-6).
func TestAFloodingAddressEvictsOnlyItsOwn(t *testing.T) {
	c, _ := testChallenges()
	c.PerClient = 4
	mine, _, _ := c.Issue(PurposeLogin, "", "192.168.1.20")
	for i := 0; i < 100; i++ {
		if _, _, err := c.Issue(PurposeLogin, "", "203.0.113.9"); err != nil {
			t.Fatalf("issue %d refused: %v", i, err)
		}
	}
	if n := c.count(PurposeLogin, "203.0.113.9", true); n != 4 {
		t.Errorf("the flooding address holds %d, want 4", n)
	}
	if _, _, ok := c.Take(mine, PurposeLogin); !ok {
		t.Fatal("somebody else's flood evicted a real sign-in")
	}
}

// A full sign-in budget, filled from many addresses, drops its oldest rather
// than refusing the newest, and never touches the registration budget.
func TestAFullSignInBudgetDropsTheOldest(t *testing.T) {
	c, _ := testChallenges()
	c.Cap = 3
	reg, _, _ := c.Issue(PurposeRegister, "u_1", "10.0.0.1")
	first, _, _ := c.Issue(PurposeLogin, "", "10.0.0.2")
	for i := 0; i < 3; i++ {
		if _, _, err := c.Issue(PurposeLogin, "", "10.0.1."+string(rune('3'+i))); err != nil {
			t.Fatalf("a full sign-in budget refused: %v", err)
		}
	}
	if _, _, ok := c.Take(first, PurposeLogin); ok {
		t.Error("the oldest sign-in challenge was kept")
	}
	if _, _, ok := c.Take(reg, PurposeRegister); !ok {
		t.Error("a sign-in flood evicted a registration")
	}
}
