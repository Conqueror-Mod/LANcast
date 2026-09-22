package guestticket

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

var (
	t0   = time.Unix(1_700_000_000, 0)
	soon = t0.Add(2 * time.Minute)
)

func TestANonceSpendsOnce(t *testing.T) {
	n := NewNonceStore(0)

	if !n.Spend("georgia", "abc", soon, t0) {
		t.Fatal("first use refused")
	}
	if n.Spend("georgia", "abc", soon, t0) {
		t.Error("second use accepted; the ticket is replayable")
	}
}

/*
 * The property that makes per-issuer keying necessary rather than tidy.
 *
 * Keyed by nonce alone, a peer could spend a value another peer was about to
 * use — at worst by guessing, at best by watching — and refuse that peer's
 * admissions at will. One friend could lock out another.
 */
func TestOneIssuerCannotSpendAnothersNonce(t *testing.T) {
	n := NewNonceStore(0)

	if !n.Spend("mallory", "abc", soon, t0) {
		t.Fatal("first use refused")
	}
	if !n.Spend("georgia", "abc", soon, t0) {
		t.Error("georgia refused a nonce only mallory had used")
	}
}

/*
 * Over the allowance it refuses. Evicting the oldest to make room is the
 * intuitive move and it reopens replay: the evicted nonce becomes spendable
 * again, so anybody able to push entries through could replay at will.
 *
 * Refusing is a denial of service against one peer. Evicting is a replay
 * vulnerability for every peer. This test is the difference.
 */
func TestOverTheAllowanceItRefusesRatherThanEvicts(t *testing.T) {
	const max = 8
	n := NewNonceStore(max)

	for i := range max {
		if !n.Spend("georgia", fmt.Sprintf("n-%d", i), soon, t0) {
			t.Fatalf("nonce %d refused below the allowance", i)
		}
	}
	if n.Spend("georgia", "one-too-many", soon, t0) {
		t.Error("accepted past the allowance")
	}

	// The first nonce must still be spent. If it were evicted to make room,
	// this would come back fresh — which is the vulnerability.
	if n.Spend("georgia", "n-0", soon, t0) {
		t.Error("the oldest nonce was evicted and is spendable again")
	}
}

// One peer filling its allowance must not affect anybody else's.
func TestAFloodingPeerDoesNotRefuseTheOthers(t *testing.T) {
	const max = 4
	n := NewNonceStore(max)

	for i := range max + 10 {
		n.Spend("mallory", fmt.Sprintf("n-%d", i), soon, t0)
	}
	if !n.Spend("georgia", "hello", soon, t0) {
		t.Error("georgia refused because mallory was noisy")
	}
}

// Expired entries free the allowance again, so a peer that was once busy is
// not permanently reduced.
func TestExpiryFreesTheAllowance(t *testing.T) {
	const max = 2
	n := NewNonceStore(max)

	n.Spend("georgia", "a", t0.Add(time.Minute), t0)
	n.Spend("georgia", "b", t0.Add(time.Minute), t0)
	if n.Spend("georgia", "c", soon, t0) {
		t.Fatal("accepted past the allowance")
	}

	later := t0.Add(2 * time.Minute)
	if !n.Spend("georgia", "c", later.Add(time.Minute), later) {
		t.Error("still refused after the earlier nonces expired")
	}
}

// A nonce whose ticket has already expired is not fresh, and storing it would
// be keeping something that can never be used.
func TestAnAlreadyExpiredNonceIsRefused(t *testing.T) {
	n := NewNonceStore(0)

	if n.Spend("georgia", "old", t0.Add(-time.Second), t0) {
		t.Error("accepted a nonce that had already expired")
	}
	if n.Outstanding("georgia") != 0 {
		t.Error("stored it anyway")
	}
}

func TestSweepForgetsQuietPeers(t *testing.T) {
	n := NewNonceStore(0)
	n.Spend("georgia", "a", t0.Add(time.Minute), t0)

	n.Sweep(t0.Add(2 * time.Minute))
	if got := n.Outstanding("georgia"); got != 0 {
		t.Errorf("outstanding = %d after sweep, want 0", got)
	}
}

// Admission happens on request goroutines, so two arriving together must not
// both succeed with the same nonce.
func TestConcurrentSpendsAdmitExactlyOne(t *testing.T) {
	n := NewNonceStore(0)

	const racers = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()
			if n.Spend("georgia", "same", soon, t0) {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if won != 1 {
		t.Errorf("%d of %d spends succeeded, want exactly 1", won, racers)
	}
}
