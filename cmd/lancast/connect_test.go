package main

import (
	"testing"

	"lancast/internal/knownserver"
)

const (
	remoteAddr = "192.168.1.66:8080"
	remotePin  = "3Xk0aBcD1efGh2IjKlMn3OpQrStUvWxYz0123456789="
	diskPin    = "LocalaBcD1efGh2IjKlMn3OpQrStUvWxYz0123456789="
)

func trusting(address, pin, name string) knownserver.List {
	return knownserver.List{}.Accept(knownserver.Server{Address: address, Pin: pin, Name: name})
}

/*
 * The default launch, on every machine that exists today.
 *
 * This is the case that must not change. A client that started asking which
 * server to open would be a regression for everybody who has exactly one.
 */
func TestWithNothingRememberedItOpensTheLocalServer(t *testing.T) {
	got, ask := resolve(facts{flagAddr: ":8080", localPin: diskPin})

	if ask {
		t.Error("asked which server to open when there is only one")
	}
	if !got.local {
		t.Error("did not open the local server")
	}
	if got.address != ":8080" {
		t.Errorf("address = %q, want the flag's value", got.address)
	}
	if got.pin != diskPin {
		t.Errorf("pin = %q, want the pin read off local disk", got.pin)
	}
}

// An operator who typed -addr meant it, and is talking about this machine.
func TestAnExplicitAddressIsAnInstruction(t *testing.T) {
	got, ask := resolve(facts{
		addrFlagGiven: true,
		flagAddr:      ":9000",
		localPin:      diskPin,
		// Even with a remembered remote server, which must not override it.
		lastServer: remoteAddr,
		known:      trusting(remoteAddr, remotePin, "Chris"),
	})

	if ask {
		t.Error("asked a question of somebody who had already answered it")
	}
	if !got.local || got.address != ":9000" {
		t.Errorf("target = %+v, want the local server at :9000", got)
	}
	if got.pin != diskPin {
		t.Errorf("pin = %q, want the local disk pin", got.pin)
	}
}

// The point of the feature: it opens where it opened last time.
func TestARememberedServerIsOpenedWithoutAsking(t *testing.T) {
	got, ask := resolve(facts{
		flagAddr:   ":8080",
		localPin:   diskPin,
		lastServer: remoteAddr,
		known:      trusting(remoteAddr, remotePin, "Chris"),
	})

	if ask {
		t.Error("asked about a server that is remembered and trusted")
	}
	if got.local {
		t.Error("opened the local server instead of the remembered one")
	}
	if got.address != remoteAddr {
		t.Errorf("address = %q, want %q", got.address, remoteAddr)
	}
	if got.pin != remotePin {
		t.Errorf("pin = %q, want the stored pin — not the local one", got.pin)
	}
	if got.name != "Chris" {
		t.Errorf("name = %q, want the name it was given", got.name)
	}
}

// The remembered address is matched after parsing, not as raw text, so a
// preference written in another form still finds its record.
func TestARememberedAddressIsCanonicalisedBeforeItIsLookedUp(t *testing.T) {
	got, ask := resolve(facts{
		flagAddr:   ":8080",
		lastServer: "https://192.168.1.66:8080/movies",
		known:      trusting(remoteAddr, remotePin, "Chris"),
	})

	if ask {
		t.Error("asked about a server whose record exists under its canonical address")
	}
	if got.address != remoteAddr || got.pin != remotePin {
		t.Errorf("target = %+v, want the record for %q", got, remoteAddr)
	}
}

/*
 * Forgetting a server has to be visible at the next launch.
 *
 * Quietly opening the local server would read as the remote one having been
 * lost, and quietly opening the remote one would undo the act of forgetting.
 * Neither is honest, so this is the one case that interrupts.
 */
func TestARememberedServerThatIsNoLongerTrustedAsks(t *testing.T) {
	got, ask := resolve(facts{
		flagAddr:   ":8080",
		localPin:   diskPin,
		lastServer: remoteAddr,
		known:      knownserver.List{}, // forgotten
	})

	if !ask {
		t.Fatal("did not ask after the remembered server was forgotten")
	}
	if !got.local {
		t.Error("the fallback should be the local server")
	}
	if got.pin == remotePin {
		t.Error("carried the forgotten server's pin")
	}
}

// A preference file holding nonsense is a preference about nothing, not a
// reason to refuse to open.
func TestAnUnparseableRememberedAddressFallsBackQuietly(t *testing.T) {
	got, ask := resolve(facts{
		flagAddr:   ":8080",
		localPin:   diskPin,
		lastServer: "ftp://nowhere",
	})

	if ask {
		t.Error("asked about an address that was never a server")
	}
	if !got.local {
		t.Error("did not fall back to the local server")
	}
}

/*
 * A loopback-only server has no certificate and needs no pin.
 *
 * Worth its own case because an empty pin is also what a *failure* to read one
 * looks like, and the two must not be told apart by guessing here: resolve
 * passes through whatever was read, and the caller that reads it owns the
 * retry.
 */
func TestALocalServerWithNoCertificateCarriesNoPin(t *testing.T) {
	got, ask := resolve(facts{flagAddr: ":8080"})

	if ask {
		t.Error("asked a question about a machine with one server")
	}
	if got.pin != "" {
		t.Errorf("pin = %q, want none", got.pin)
	}
}

// The remembered server's pin is the stored one, never the local machine's.
// Confusing the two would pin the wrong key and fail the handshake in a way
// that reads as the server being broken.
func TestARemoteTargetNeverBorrowsTheLocalPin(t *testing.T) {
	got, _ := resolve(facts{
		flagAddr:   ":8080",
		localPin:   diskPin,
		lastServer: remoteAddr,
		known:      trusting(remoteAddr, remotePin, ""),
	})

	if got.pin != remotePin {
		t.Errorf("pin = %q, want %q", got.pin, remotePin)
	}
	if got.local {
		t.Error("a remote target must not be marked local — it decides whether this process may start a server")
	}
}
