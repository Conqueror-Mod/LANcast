package singleton

import "testing"

// The guard's whole contract: the first caller holds the name, a second is told
// it does not, and once released the name is available again.
func TestAcquireIsExclusiveAndReleasable(t *testing.T) {
	name := "LANcast-test-" + t.Name()

	release, held, err := Acquire(name)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !held {
		t.Fatal("first Acquire did not take the name")
	}

	_, held2, err := Acquire(name)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if held2 {
		t.Error("second Acquire took a name already held — the guard does not guard")
	}

	release()

	release3, held3, err := Acquire(name)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if !held3 {
		t.Error("name was not available again after release")
	}
	release3()
}

// The server and client names are distinct, so one running never blocks the
// other.
func TestServerAndClientNamesAreIndependent(t *testing.T) {
	rs, heldS, err := Acquire(Server)
	if err != nil || !heldS {
		t.Skipf("could not take the server name here (held=%v, err=%v)", heldS, err)
	}
	defer rs()

	rc, heldC, err := Acquire(Client)
	if err != nil {
		t.Fatalf("client Acquire: %v", err)
	}
	if !heldC {
		t.Error("holding the server name blocked the client name")
	}
	rc()
}

// Release is safe to call even when the name was not taken, so callers can defer
// it unconditionally.
func TestReleaseIsSafeWhenNotHeld(t *testing.T) {
	name := "LANcast-test-" + t.Name()
	first, _, err := Acquire(name)
	if err != nil {
		t.Fatal(err)
	}
	defer first()

	release, held, err := Acquire(name)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("expected the second Acquire not to hold")
	}
	release() // must not panic
}
