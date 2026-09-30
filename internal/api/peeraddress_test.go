package api

import (
	"strings"
	"testing"
)

/*
 * Learning where a peer went (ADR 0044 §5).
 *
 * §5 says the address is a hint and the fingerprint is the identity, and then
 * says nothing about how a hint is corrected. It never was: addresses were
 * written once from an invite and never revisited, so a peer that moved became
 * unreachable with no way back except a fresh invite. That happened between two
 * real servers and was misdiagnosed for a day as a routing problem.
 *
 * The decision is a pure function because every interesting case is about
 * *which port*, and those are tedious to arrange over a real socket.
 */
func TestWhereAPeerConnectedFromBecomesAHint(t *testing.T) {
	for _, c := range []struct {
		name   string
		remote string
		known  []string
		want   string
	}{
		{
			// The whole point: they moved, and the port they serve on did not.
			name:   "a peer on a new address",
			remote: "192.0.2.77:51234",
			known:  []string{"198.51.100.4:8080"},
			want:   "192.0.2.77:8080",
		},
		{
			// IPv6 has to come back bracketed or it is not an address at all.
			name:   "an IPv6 peer",
			remote: "[2600:1702:5ac7:9200::30]:51234",
			known:  []string{"198.51.100.4:8080"},
			want:   "[2600:1702:5ac7:9200::30]:8080",
		},
		{
			// A peer serving somewhere unusual keeps serving there.
			name:   "a peer on a port of its own",
			remote: "192.0.2.77:51234",
			known:  []string{"198.51.100.4:9999"},
			want:   "192.0.2.77:9999",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := learnedAddress(c.remote, c.known)
			if !ok {
				t.Fatalf("learned nothing from %s", c.remote)
			}
			if got != c.want {
				t.Errorf("learned %q, want %q", got, c.want)
			}
		})
	}
}

/*
 * The ephemeral source port is never kept, and that is the trap this function
 * exists for.
 *
 * `RemoteAddr` carries the port the peer dialled *out* of. It is different on
 * every connection and listens for nothing, so an address built from it is an
 * address that can never answer — and the list would fill with them at one per
 * request.
 */
func TestThePortTheyDialledOutOfIsNeverKept(t *testing.T) {
	got, ok := learnedAddress("192.0.2.77:51234", []string{"198.51.100.4:8080"})
	if !ok {
		t.Fatal("learned nothing")
	}
	if strings.Contains(got, "51234") {
		t.Errorf("learned %q, which is the port they dialled out of", got)
	}
}

/*
 * Nothing is learned when there is nothing to learn from.
 *
 * With no recorded port there is nothing to build an address *with*, and
 * inventing a default would write one nobody has ever answered on. Loopback is
 * refused because a connection from this machine says nothing about where
 * another household is — and recording it would eventually point every peer at
 * ourselves.
 */
func TestSomeConnectionsTeachNothing(t *testing.T) {
	for _, c := range []struct {
		name   string
		remote string
		known  []string
	}{
		{"no port is known for them", "192.0.2.77:51234", nil},
		{"nothing usable is known", "192.0.2.77:51234", []string{"no-port-here"}},
		{"from this machine", "127.0.0.1:51234", []string{"198.51.100.4:8080"}},
		{"from this machine, v6", "[::1]:51234", []string{"198.51.100.4:8080"}},
		{"an unspecified address", "0.0.0.0:51234", []string{"198.51.100.4:8080"}},
		{"not an address at all", "nonsense", []string{"198.51.100.4:8080"}},
		{"a hostname rather than an IP", "peer.example:51234", []string{"198.51.100.4:8080"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := learnedAddress(c.remote, c.known); ok {
				t.Errorf("learned %q from %q, and should have learned nothing", got, c.remote)
			}
		})
	}
}

/*
 * A peer that has not moved causes no work.
 *
 * Presence and the roster are the two calls a peer makes most often — several
 * times a minute, for as long as both servers are on. If the ordinary case
 * reached the database this would be a write per request, for ever, to record
 * something that did not change.
 */
func TestAPeerThatHasNotMovedIsNotRelearned(t *testing.T) {
	known := []string{"192.0.2.77:8080", "198.51.100.4:8080"}
	if got, ok := learnedAddress("192.0.2.77:51234", known); ok {
		t.Errorf("learned %q from a peer that is already the best guess", got)
	}
}

/*
 * A peer that moved *back* is promoted rather than duplicated.
 *
 * Its address is known but is no longer the first guess, so the list has to be
 * reordered rather than appended to — otherwise the address that works sits
 * behind the one that does not, and every call pays a timeout to find out.
 */
func TestAPeerThatCameBackIsPromoted(t *testing.T) {
	known := []string{"198.51.100.4:8080", "192.0.2.77:8080"}
	got, ok := learnedAddress("192.0.2.77:51234", known)
	if !ok {
		t.Fatal("learned nothing from an address that is known but not first")
	}
	if got != "192.0.2.77:8080" {
		t.Errorf("learned %q, want the known address promoted", got)
	}
}
