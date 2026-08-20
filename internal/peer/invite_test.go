package peer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"lancast/internal/identity"
)

func testIdentity(t *testing.T) identity.Identity {
	t.Helper()
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRoundTrip(t *testing.T) {
	id := testIdentity(t)

	s, err := Encode(id, "Georgia's LANcast", []string{"10.121.240.21:8080", "192.168.1.9:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, prefix) {
		t.Errorf("invite does not carry the prefix: %q", s)
	}

	got, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Fingerprint != id.Fingerprint() {
		t.Errorf("fingerprint = %q, want %q", got.Fingerprint, id.Fingerprint())
	}
	if got.Name != "Georgia's LANcast" {
		t.Errorf("name = %q", got.Name)
	}
	if len(got.Addrs) != 2 || got.Addrs[0] != "10.121.240.21:8080" {
		t.Errorf("addrs = %v, want both in the order given", got.Addrs)
	}
}

/*
 * What an invite survives on the way through a chat window.
 *
 * These are not hypothetical: a long opaque string gets wrapped, gets a stray
 * space, arrives with the quotes somebody typed around it, and gets
 * sentence-cased at the start of a line by a well-meaning client. None of that
 * changes what was meant, so none of it should cost an evening.
 */
func TestParseToleratesPasting(t *testing.T) {
	id := testIdentity(t)
	s, err := Encode(id, "Aither", []string{"10.121.240.235:8080"})
	if err != nil {
		t.Fatal(err)
	}

	half := len(s) / 2
	for name, pasted := range map[string]string{
		"leading and trailing space": "   " + s + "  ",
		"a trailing newline":         s + "\n",
		"wrapped across lines":       s[:half] + "\n" + s[half:],
		"wrapped with indentation":   s[:half] + "\n    " + s[half:],
		"internal spaces":            s[:half] + " " + s[half:],
		"uppercase prefix":           strings.ToUpper(prefix) + strings.TrimPrefix(s, prefix),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(pasted)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Fingerprint != id.Fingerprint() {
				t.Error("survived parsing but produced the wrong fingerprint")
			}
		})
	}
}

// A truncated or edited invite must fail, not half-succeed. Half-succeeding
// here means pairing with a fingerprint nobody sent.
func TestDamagedInvitesAreRefused(t *testing.T) {
	id := testIdentity(t)
	good, err := Encode(id, "Aither", []string{"10.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}

	for name, bad := range map[string]string{
		"empty":              "",
		"no prefix":          strings.TrimPrefix(good, prefix),
		"another product":    "syncthing://" + strings.TrimPrefix(good, prefix),
		"prefix only":        prefix,
		"truncated payload":  good[:len(good)-8],
		"not base64":         prefix + "!!!! not base64 !!!!",
		"base64 of nonsense": prefix + base64.RawURLEncoding.EncodeToString([]byte("hello")),
		"json but not ours":  prefix + base64.RawURLEncoding.EncodeToString([]byte(`{"hello":"world"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(bad); err == nil {
				t.Fatal("a damaged invite parsed successfully")
			}
		})
	}
}

// A blob from a future LANcast is refused by version, before its fields are
// read — the failure where half the keys still mean what they used to.
func TestFutureVersionIsRefusedByVersion(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"v": 99, "fp": strings.Repeat("A", 52), "name": "Later",
		"addrs": []string{"10.0.0.1:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(prefix + base64.RawURLEncoding.EncodeToString(body))
	if !errors.Is(err, ErrWrongVersion) {
		t.Fatalf("err = %v, want ErrWrongVersion", err)
	}
}

func TestFingerprintMustBeAFingerprint(t *testing.T) {
	for name, fp := range map[string]string{
		"empty":            "",
		"too short":        strings.Repeat("A", 51),
		"too long":         strings.Repeat("A", 53),
		"outside base32":   strings.Repeat("A", 51) + "1",
		"lowercase filler": strings.Repeat("a", 52) + "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Invite{
				Fingerprint: fp, Name: "x", Addrs: []string{"10.0.0.1:8080"},
			}.Encode()
			// Lower case is *normalized*, not rejected — base32 has no lower
			// case, so folding cannot collide with another valid value.
			if name == "lowercase filler" {
				if err != nil {
					t.Fatalf("lower case should normalize, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrBadFingerpint) {
				t.Fatalf("err = %v, want ErrBadFingerpint", err)
			}
		})
	}
}

// The fingerprint is accepted however somebody typed it, and comes back
// canonical — this is what stops two screens disagreeing about a match.
func TestFingerprintIsNormalizedOnTheWayIn(t *testing.T) {
	id := testIdentity(t)
	in := Invite{
		Fingerprint: strings.ToLower(id.Grouped()),
		Name:        "Aither",
		Addrs:       []string{"10.0.0.1:8080"},
	}
	s, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != id.Fingerprint() {
		t.Errorf("fingerprint = %q, want the canonical %q", got.Fingerprint, id.Fingerprint())
	}
	if got.Grouped() != id.Grouped() {
		t.Errorf("Grouped = %q, want %q", got.Grouped(), id.Grouped())
	}
}

/*
 * The name is a stranger's display string.
 *
 * It lands in a peer list, so a newline that turns one row into two, or an
 * escape sequence that repaints a terminal, is the sender's choice and not
 * ours. Length is capped so one peer cannot occupy the whole list.
 */
func TestNameIsCleanedButNotMangled(t *testing.T) {
	id := testIdentity(t)
	enc := func(name string) Invite {
		t.Helper()
		s, err := Encode(id, name, []string{"10.0.0.1:8080"})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := enc("Georgia\nadmin").Name; strings.ContainsAny(got, "\r\n") {
		t.Errorf("newline survived: %q", got)
	}
	if got := enc("Bad\x1b[31mred").Name; strings.Contains(got, "\x1b") {
		t.Errorf("escape survived: %q", got)
	}
	if got := enc(strings.Repeat("x", 500)).Name; len([]rune(got)) > maxNameLen {
		t.Errorf("name is %d runes, want at most %d", len([]rune(got)), maxNameLen)
	}
	// Not an ASCII filter: names are people's, and most of the world's are not
	// ASCII.
	for _, name := range []string{"Georgia's Utopia", "Björn", "北京の家", "Дом"} {
		if got := enc(name).Name; got != name {
			t.Errorf("name %q was mangled to %q", name, got)
		}
	}
	// A nameless invite still introduces a server; the fingerprint is the
	// identity. A blank row is worse than a placeholder.
	if got := enc("   ").Name; got == "" {
		t.Error("an empty name produced an empty row rather than a placeholder")
	}
}

func TestAddressRules(t *testing.T) {
	id := testIdentity(t)

	// At least one usable address, or the invite cannot introduce anything.
	for name, addrs := range map[string][]string{
		"none":              nil,
		"empty strings":     {"", "   "},
		"no port":           {"10.0.0.1"},
		"port not a number": {"10.0.0.1:http"},
		"port out of range": {"10.0.0.1:70000"},
		"port zero":         {"10.0.0.1:0"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Encode(id, "x", addrs); !errors.Is(err, ErrNoAddress) {
				t.Fatalf("err = %v, want ErrNoAddress", err)
			}
		})
	}

	// Unusable entries are dropped rather than failing an invite that also
	// carries a good one.
	s, err := Encode(id, "x", []string{"nope", "10.0.0.1:8080", "", "10.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Addrs) != 1 || got.Addrs[0] != "10.0.0.1:8080" {
		t.Errorf("addrs = %v, want the one good address, deduplicated", got.Addrs)
	}

	// IPv6 needs its brackets, and must survive.
	s, err = Encode(id, "x", []string{"[fd00::1]:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Parse(s); len(got.Addrs) != 1 || got.Addrs[0] != "[fd00::1]:8080" {
		t.Errorf("IPv6 address did not survive: %v", got.Addrs)
	}

	// A hostname is not resolved here: a peer whose machine is switched off is
	// still a peer, and a parser that insisted otherwise would refuse it.
	if _, err := Encode(id, "x", []string{"utopia.example:8080"}); err != nil {
		t.Errorf("a hostname was refused: %v", err)
	}
}

func TestTooManyAddressesAreCapped(t *testing.T) {
	id := testIdentity(t)
	many := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		many = append(many, "10.0.0."+string(rune('0'+i%10))+":8080")
	}
	s, err := Encode(id, "x", many)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Addrs) > maxAddrs {
		t.Errorf("kept %d addresses, want at most %d", len(got.Addrs), maxAddrs)
	}
}

// A blob far larger than any honest invite is refused before it is decoded.
func TestOversizedBlobIsRefused(t *testing.T) {
	if _, err := Parse(prefix + strings.Repeat("A", maxBlobLen)); err == nil {
		t.Fatal("an oversized blob was accepted")
	}
}

// Encoding validates its own output, so a bad invite cannot be produced here
// and then diagnosed on somebody else's machine from a paste.
func TestEncodeRefusesToProduceSomethingUnparseable(t *testing.T) {
	if _, err := (Invite{Fingerprint: "nope", Name: "x", Addrs: []string{"10.0.0.1:8080"}}).Encode(); err == nil {
		t.Error("encoded an invite with an invalid fingerprint")
	}
}
