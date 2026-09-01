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

/*
 * The three names are distinct, and that is the whole contract.
 *
 * A server, a client and a tray can all be running at once and none may block
 * another. The tray was added last, after two identical icons appeared in the
 * notification area for one service — it had been sharing nothing, because it
 * locked nothing.
 */
func TestTheNamesAreDistinct(t *testing.T) {
	names := []string{Server, Client, Tray}
	for i := range names {
		if names[i] == "" {
			t.Fatalf("name %d is empty", i)
		}
		for j := i + 1; j < len(names); j++ {
			if names[i] == names[j] {
				t.Errorf("%q is used twice; one of these would block the other",
					names[i])
			}
		}
	}
}

// A tray can be acquired while a server is held: on an installed machine the
// server is a service and the tray is a user-session process controlling it,
// and they run together by design.
func TestATrayDoesNotBlockAServer(t *testing.T) {
	relServer, heldServer, err := Acquire(Server)
	if err != nil {
		t.Skipf("locks unavailable here: %v", err)
	}
	defer relServer()
	if !heldServer {
		t.Skip("a LANcast server is already running on this machine")
	}

	relTray, heldTray, err := Acquire(Tray)
	if err != nil {
		t.Fatal(err)
	}
	defer relTray()
	if !heldTray {
		t.Error("holding the server name blocked the tray")
	}
}
