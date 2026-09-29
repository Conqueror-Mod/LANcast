package tlscert

import (
	"slices"
	"testing"
)

/*
 * Which addresses an invite is allowed to carry.
 *
 * An invite is recorded once at pairing and never re-learned (ADR 0044 §5), so
 * an address that expires becomes an entry that no longer exists and is still
 * tried — a connect timeout on every peer operation. A real peer record was
 * found holding three of them.
 *
 * The rules are asserted over two lists rather than against this machine: what
 * addresses a computer holds is unknowable from here and different on CI.
 * `TestShowThisMachinesAddresses` is the other half, run by hand.
 */
func TestAnInviteDropsWhatIsGoingToDisappear(t *testing.T) {
	const (
		dhcp      = "2600:1702:5ac7:9200::2e"
		stable    = "2600:1702:5ac7:9200:a51e:ee8c:6fb1:966b"
		temporary = "2600:1702:5ac7:9200:e98b:9995:5005:6453"
		lan       = "192.168.1.66"
	)
	all := []string{dhcp, stable, temporary, lan}

	got := withoutTemporary(all, map[string]bool{temporary: true})

	if slices.Contains(got, temporary) {
		t.Error("the temporary address survived into an invite")
	}
	for _, want := range []string{dhcp, stable, lan} {
		if !slices.Contains(got, want) {
			t.Errorf("%s was dropped and should have been kept", want)
		}
	}
}

/*
 * Anything unclassified is kept, which is the direction the mistake has to go.
 *
 * A stale entry costs a timeout. A missing one can cost the pairing — and on
 * every platform that is not Windows nothing is classified at all, so this is
 * the ordinary case rather than an edge.
 */
func TestNothingKnownMeansNothingDropped(t *testing.T) {
	all := []string{"192.168.1.66", "2600:1702:5ac7:9200::2e"}

	for _, skip := range []map[string]bool{nil, {}} {
		got := withoutTemporary(all, skip)
		if !slices.Equal(got, all) {
			t.Errorf("with %v known, got %v, want everything", skip, got)
		}
	}
}

/*
 * A machine whose every address is temporary still gets to introduce itself.
 *
 * Returning nothing would produce ErrNoAddress — "this server has no address
 * another machine could reach it on" — which is a far worse answer than an
 * address that expires in a few days.
 */
func TestAMachineIsNeverLeftWithNoAddress(t *testing.T) {
	all := []string{"2600:1702:5ac7:9200:e98b:9995:5005:6453"}

	got := withoutTemporary(all, map[string]bool{all[0]: true})

	if len(got) == 0 {
		t.Fatal("every address was dropped; this server cannot introduce itself")
	}
	if !slices.Equal(got, all) {
		t.Errorf("got %v, want the lot back", got)
	}
}

/*
 * The certificate is not narrowed.
 *
 * It covers whatever somebody might type, and a temporary address is real
 * today — somebody can be looking at it right now. Only what is *written down*
 * is filtered. Asserted as a property, so a later tidy-up that points
 * LoadOrGenerate at StableIPs fails here.
 */
func TestTheCertificateStillCoversEverything(t *testing.T) {
	all := LocalIPs()
	stable := StableIPs()

	if len(stable) > len(all) {
		t.Fatalf("StableIPs returned more than LocalIPs: %d > %d", len(stable), len(all))
	}
	for _, ip := range stable {
		if !slices.Contains(all, ip) {
			t.Errorf("%s is in the invite list and not in the certificate list", ip)
		}
	}
}
