package main

import (
	"strings"
	"testing"
)

/*
 * What the window says when the server never arrives.
 *
 * The old behaviour said "the server did not come up at …" for every case,
 * having just spent twenty seconds trying to start a server it could never
 * start. Two different situations were described with one sentence, and the
 * sentence suggested the client had done something it had not.
 */

func TestAStoppedServiceIsNotDescribedAsStarting(t *testing.T) {
	msg := serviceWaitMessage(":8080", "stopped")

	// The action that actually fixes it, and who can take it.
	for _, want := range []string{"not running", "Windows Services", "administrator"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not mention %q:\n%s", want, msg)
		}
	}
	// A stopped service is not going to arrive on its own, and telling somebody
	// to wait for it is the advice that wastes their morning.
	if strings.Contains(strings.ToLower(msg), "still starting") {
		t.Errorf("a stopped service was described as starting:\n%s", msg)
	}
}

func TestAStartingServiceIsWorthWaitingFor(t *testing.T) {
	msg := serviceWaitMessage(":8080", "start pending")

	if !strings.Contains(msg, "start pending") {
		t.Errorf("the service state is not in the message:\n%s", msg)
	}
	if !strings.Contains(msg, "still starting") {
		t.Errorf("message does not say it may still be coming:\n%s", msg)
	}
	// Where it was looked for, so somebody can try it themselves.
	if !strings.Contains(msg, "8080") {
		t.Errorf("the address is not in the message:\n%s", msg)
	}
}

func TestTheTwoCasesDoNotShareAMessage(t *testing.T) {
	// The whole point of splitting them. One sentence for both is what shipped.
	stopped := serviceWaitMessage(":8080", "stopped")
	starting := serviceWaitMessage(":8080", "start pending")
	if stopped == starting {
		t.Fatal("a stopped service and a starting one are given the same words")
	}
}

func TestNoMessageBlamesTheClientForNotStartingAServer(t *testing.T) {
	/*
	 * The client cannot start a server on a service install: the data directory
	 * belongs to the system account, so the spawned server gets a read-only
	 * database and dies. Any wording implying it tried and failed sends people
	 * looking in the wrong place — which is where the twenty-second wait and
	 * the old message sent them.
	 */
	for _, state := range []string{"stopped", "start pending", "unknown"} {
		msg := strings.ToLower(serviceWaitMessage(":8080", state))
		if strings.Contains(msg, "could not start the server") {
			t.Errorf("state %q claims the client tried to start a server:\n%s", state, msg)
		}
	}
}
