package store

import (
	"context"
	"sync"
	"testing"
)

func TestTwoFactorRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "u_1", "dana", "h", "admin"); err != nil {
		t.Fatal(err)
	}
	tf, err := s.TwoFactorFor(ctx, "u_1")
	if err != nil || tf.Enabled {
		t.Fatalf("a new account has two-factor: %+v %v", tf, err)
	}
	if err := s.EnableTwoFactor(ctx, "u_1", []byte("sealed"), []string{"a", "b"}, 100); err != nil {
		t.Fatal(err)
	}
	tf, _ = s.TwoFactorFor(ctx, "u_1")
	if !tf.Enabled || string(tf.Sealed) != "sealed" || tf.EnabledAt != 100 || len(tf.Recovery) != 2 {
		t.Fatalf("after enable: %+v", tf)
	}
	if err := s.DisableTwoFactor(ctx, "u_1"); err != nil {
		t.Fatal(err)
	}
	tf, _ = s.TwoFactorFor(ctx, "u_1")
	if tf.Enabled || tf.Recovery != nil {
		t.Fatalf("after disable: %+v", tf)
	}
	if _, err := s.TwoFactorFor(ctx, "u_nobody"); err != ErrNotFound {
		t.Fatalf("missing account: %v", err)
	}
}

// A step is spent once, even by two sign-ins racing with the same code.
func TestATwoFactorStepIsSpentOnce(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	s.CreateUser(ctx, "u_1", "dana", "h", "admin")
	if ok, _ := s.UseTwoFactorStep(ctx, "u_1", 5); ok {
		t.Fatal("a step was spent on an account without two-factor")
	}
	s.EnableTwoFactor(ctx, "u_1", []byte("x"), nil, 1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := s.UseTwoFactorStep(ctx, "u_1", 10); err == nil && ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("step 10 spent %d times", wins)
	}
	if ok, _ := s.UseTwoFactorStep(ctx, "u_1", 9); ok {
		t.Fatal("an earlier step was accepted after a later one")
	}
	if ok, _ := s.UseTwoFactorStep(ctx, "u_1", 11); !ok {
		t.Fatal("the next step was refused")
	}
}

func TestARecoveryCodeWorksOnce(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	s.CreateUser(ctx, "u_1", "dana", "h", "admin")
	s.EnableTwoFactor(ctx, "u_1", []byte("x"), []string{"h1", "h2"}, 1)
	if ok, err := s.UseRecoveryCode(ctx, "u_1", "h1"); !ok || err != nil {
		t.Fatalf("first use: %v %v", ok, err)
	}
	if ok, _ := s.UseRecoveryCode(ctx, "u_1", "h1"); ok {
		t.Fatal("a recovery code worked twice")
	}
	tf, _ := s.TwoFactorFor(ctx, "u_1")
	if len(tf.Recovery) != 1 || tf.Recovery[0] != "h2" {
		t.Fatalf("left: %v", tf.Recovery)
	}
}
