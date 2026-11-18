package devicepair

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
)

// The code is a wire format the Roku app reproduces in BrightScript, so it is
// pinned to a value worked by hand rather than to whatever Code returns today.
func TestCodeIsTheDocumentedHash(t *testing.T) {
	nonce := bytes.Repeat([]byte{0x01}, 16)
	cert := []byte("a certificate")
	h := sha256.New()
	h.Write([]byte("LANcast pairing v1\x00"))
	h.Write(nonce)
	h.Write(cert)
	sum := h.Sum(nil)

	// 40 bits, eight groups of five, most significant first.
	var want strings.Builder
	bits := uint64(sum[0])<<32 | uint64(sum[1])<<24 | uint64(sum[2])<<16 | uint64(sum[3])<<8 | uint64(sum[4])
	for shift := 35; shift >= 0; shift -= 5 {
		want.WriteByte(Alphabet[(bits>>uint(shift))&31])
	}
	if got := Code(nonce, cert); got != want.String() {
		t.Fatalf("Code = %s, want %s", got, want.String())
	}
}

// The whole defence: a TV that was served a different certificate shows a
// different code.
func TestADifferentCertificateIsADifferentCode(t *testing.T) {
	nonce := []byte("0123456789abcdef")
	if Code(nonce, []byte("the server's")) == Code(nonce, []byte("an impostor's")) {
		t.Fatal("two certificates gave one code; a machine in the middle would pass")
	}
}

func TestTheAlphabetShowsNothingAmbiguous(t *testing.T) {
	if len(Alphabet) != 32 {
		t.Fatalf("alphabet has %d symbols, want 32", len(Alphabet))
	}
	for _, r := range "OILU" {
		if strings.ContainsRune(Alphabet, r) {
			t.Errorf("alphabet shows %c", r)
		}
	}
}

func TestNormalizeReadsWhatPeopleType(t *testing.T) {
	for typed, want := range map[string]string{
		"ABCD-EF12":  "ABCDEF12",
		"abcd ef12":  "ABCDEF12",
		"ABCD-EFI2":  "ABCDEF12", // I read as 1
		"ABCD-EFL2":  "ABCDEF12", // L read as 1
		"OBCD-EF12":  "0BCDEF12", // O read as 0
		"ABCD-EF1":   "",         // too short
		"ABCD-EF12X": "",
		"ABCD-EFU2":  "", // U is not in the alphabet
	} {
		if got := Normalize(typed); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", typed, got, want)
		}
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newBook() (*Book, *clock) {
	b := NewBook()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	b.SetClock(c.now)
	return b, c
}

var cert = []byte("leaf")

func TestApprovedKeyIsCollectedExactlyOnce(t *testing.T) {
	b, c := newBook()
	s, err := b.Start("Living room", "Roku", "192.0.2.5", cert)
	if err != nil {
		t.Fatal(err)
	}
	code := Code(s.Nonce, cert)

	if p, err := b.Poll(s.Token); err != nil || p.State != Waiting {
		t.Fatalf("first poll = %v, %v; want waiting", p.State, err)
	}
	if _, err := b.Decide(code, true, "u_1"); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(Interval)
	p, err := b.Poll(s.Token)
	if err != nil || p.State != Approved || p.ApproverID != "u_1" {
		t.Fatalf("poll after approval = %+v, %v", p, err)
	}
	c.t = c.t.Add(Interval)
	if _, err := b.Poll(s.Token); !errors.Is(err, ErrUnknown) {
		t.Fatalf("second collection = %v, want ErrUnknown", err)
	}
}

func TestADeniedPairingEnds(t *testing.T) {
	b, _ := newBook()
	s, _ := b.Start("Den", "Roku", "192.0.2.5", cert)
	if _, err := b.Decide(Code(s.Nonce, cert), false, "u_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Poll(s.Token); !errors.Is(err, ErrUnknown) {
		t.Fatalf("poll after denial = %v, want ErrUnknown", err)
	}
}

func TestACodeIsDecidedOnce(t *testing.T) {
	b, _ := newBook()
	s, _ := b.Start("Den", "Roku", "192.0.2.5", cert)
	code := Code(s.Nonce, cert)
	if _, err := b.Decide(code, true, "u_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Decide(code, true, "u_2"); err == nil {
		t.Fatal("a second approval was accepted; it could hand the key to another account")
	}
	if _, err := b.Lookup(code); !errors.Is(err, ErrUnknown) {
		t.Fatalf("lookup after a decision = %v, want ErrUnknown", err)
	}
}

func TestAPairingExpires(t *testing.T) {
	b, c := newBook()
	s, _ := b.Start("Den", "Roku", "192.0.2.5", cert)
	c.t = c.t.Add(Lifetime)
	if _, err := b.Lookup(Code(s.Nonce, cert)); !errors.Is(err, ErrUnknown) {
		t.Fatalf("lookup after expiry = %v", err)
	}
	if _, err := b.Poll(s.Token); !errors.Is(err, ErrUnknown) {
		t.Fatalf("poll after expiry = %v", err)
	}
}

func TestPollingFasterThanTheIntervalIsRefused(t *testing.T) {
	b, c := newBook()
	s, _ := b.Start("Den", "Roku", "192.0.2.5", cert)
	if _, err := b.Poll(s.Token); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(time.Second)
	if _, err := b.Poll(s.Token); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("poll after 1 s = %v, want ErrTooSoon", err)
	}
	c.t = c.t.Add(Interval)
	if _, err := b.Poll(s.Token); err != nil {
		t.Fatalf("poll after the interval = %v", err)
	}
}

func TestTheBookHasACeiling(t *testing.T) {
	b, c := newBook()
	for i := 0; i < MaxPending; i++ {
		if _, err := b.Start("TV", "Roku", "192.0.2.5", cert); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	if _, err := b.Start("TV", "Roku", "192.0.2.5", cert); !errors.Is(err, ErrFull) {
		t.Fatalf("start past the ceiling = %v, want ErrFull", err)
	}
	// Room again once they expire.
	c.t = c.t.Add(Lifetime)
	if _, err := b.Start("TV", "Roku", "192.0.2.5", cert); err != nil {
		t.Fatalf("start after expiry: %v", err)
	}
}

func TestRestoreLetsTheTVCollectAgain(t *testing.T) {
	b, c := newBook()
	s, _ := b.Start("Den", "Roku", "192.0.2.5", cert)
	_, _ = b.Decide(Code(s.Nonce, cert), true, "u_1")
	p, err := b.Poll(s.Token)
	if err != nil {
		t.Fatal(err)
	}
	b.Restore(s.Token, p)
	c.t = c.t.Add(Interval)
	if p, err := b.Poll(s.Token); err != nil || p.State != Approved {
		t.Fatalf("poll after restore = %v, %v", p.State, err)
	}
}
