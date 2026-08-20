// Package peer is federation between two LANcast servers: the invite that
// introduces them, and (from here on) the record of who has been introduced.
//
// This file is the invite — [ADR 0044](../../docs/adr/0044-server-identity-and-peering.md)
// §3, "introduction is out-of-band, and mutual". A single copyable string
// carrying a fingerprint, a display name and one or more addresses, exchanged
// however two people who already know each other talk. LANcast provides no
// channel for it and should not: any channel it provided would be a third
// party, which is the whole thing the no-phone-home principle rules out.
//
// # An invite is not a credential, and is not authenticated
//
// Anybody can write one. That is fine, and it is worth saying why rather than
// leaving the next reader to wonder whether it is an oversight.
//
// The fingerprint is the anchor. An attacker can hand you an invite claiming
// Georgia's fingerprint but pointing at their own address — and it buys them
// nothing, because the connection to that address is mutually authenticated
// against the pinned key (ADR 0044 §4) and they do not have Georgia's private
// half. The handshake fails. The address was only ever a hint (§5); the
// fingerprint is the identity.
//
// So the invite needs no signature. What it does need is to survive being
// pasted by a person, and to refuse anything malformed loudly rather than
// half-accepting it — which is what the parsing below is mostly about.
//
// # Receiving one does nothing
//
// Parsing an invite is not pairing. Pairing exists when each side has added the
// other, and not before: a relationship one party can create alone is one that
// can be created *at* you. This file only turns a string into a struct.
package peer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"

	"lancast/internal/identity"
)

/*
 * The wire format: a scheme-ish prefix, then base64url of a small JSON object.
 *
 * The prefix does two jobs. It makes the string recognisable as a LANcast
 * invite when it turns up pasted in a chat window with no context, and it
 * carries the version — so a blob from a future LANcast is rejected with "this
 * was made by a newer version" rather than by whatever a JSON decoder happens
 * to say about fields it has never seen.
 *
 * JSON inside rather than a packed encoding, because the thing most likely to
 * happen to this format is a field being added to it, and JSON is what the rest
 * of this project's contracts are written in.
 *
 * Base64url rather than base32: the invite is copied and pasted, never read
 * aloud, so compactness beats the read-aloud properties that make the
 * *fingerprint* base32. And url-safe rather than standard, because the one
 * place a long opaque string reliably gets mangled is when something decides it
 * looks like a URL.
 */
const (
	prefix  = "lancast-invite-v1:"
	version = 1

	// Bounds. Everything here arrives from outside and is displayed to a
	// person, so each field is capped at something no honest invite reaches.
	maxBlobLen  = 4096
	maxNameLen  = 64
	maxAddrs    = 8
	maxAddrLen  = 255
	fingerprint = 52 // characters, per ADR 0044
)

var (
	ErrNotAnInvite   = errors.New("that does not look like a LANcast invite")
	ErrWrongVersion  = errors.New("that invite was made by a newer version of LANcast")
	ErrMalformed     = errors.New("that invite is damaged or incomplete")
	ErrNoAddress     = errors.New("that invite carries no address to reach the server on")
	ErrBadFingerpint = errors.New("that invite does not carry a valid fingerprint")
)

// Invite is what one server tells another about itself, out of band.
type Invite struct {
	// Fingerprint is canonical (ungrouped, upper case) once parsed, whatever
	// form it arrived in.
	Fingerprint string
	// Name is a display name, chosen by the sender and therefore not to be
	// trusted for anything but display.
	Name string
	// Addrs are host:port hints, in the order the sender listed them. Plural
	// because a peer that moves is still the same peer, and because a machine
	// on an overlay network usually has more than one way to be reached.
	Addrs []string
}

// wire is the JSON that actually travels. Short keys because the whole thing is
// pasted by hand and every character is one somebody can truncate.
type wire struct {
	V     int      `json:"v"`
	FP    string   `json:"fp"`
	Name  string   `json:"name"`
	Addrs []string `json:"addrs"`
}

// Encode builds the invite string for this server.
//
// The caller supplies addresses because *this package cannot know them*: which
// of a machine's addresses a peer can actually reach is a fact about the
// network the operator built, not about LANcast.
func Encode(id identity.Identity, name string, addrs []string) (string, error) {
	in := Invite{Fingerprint: id.Fingerprint(), Name: name, Addrs: addrs}
	return in.Encode()
}

// Encode renders an invite. It validates its own output, so a malformed invite
// cannot be produced here and then diagnosed at the far end, on somebody else's
// machine, from a paste.
func (in Invite) Encode() (string, error) {
	clean, err := in.validated()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(wire{
		V: version, FP: clean.Fingerprint, Name: clean.Name, Addrs: clean.Addrs,
	})
	if err != nil {
		return "", fmt.Errorf("encode invite: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(body), nil
}

/*
 * Parse reads an invite somebody pasted.
 *
 * Tolerant about how it arrived and strict about what it says. A string that
 * has been through a chat window picks up surrounding whitespace and sometimes
 * a wrapped line; none of that changes what was meant, so it is stripped. What
 * is *inside* gets no such latitude, because it is the one place another
 * person's bytes enter this system.
 */
func Parse(s string) (Invite, error) {
	s = stripWhitespace(s)
	if len(s) > maxBlobLen {
		return Invite{}, ErrMalformed
	}
	// Case-insensitive on the prefix only: a chat client that sentence-cases
	// the start of a line should not cost somebody an evening.
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return Invite{}, ErrNotAnInvite
	}

	body, err := base64.RawURLEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		return Invite{}, ErrMalformed
	}

	var w wire
	if err := json.Unmarshal(body, &w); err != nil {
		return Invite{}, ErrMalformed
	}
	// Checked before anything else is read, so a future format cannot be
	// partially understood by this one — the failure mode where half the fields
	// mean what they used to and half do not.
	if w.V != version {
		return Invite{}, ErrWrongVersion
	}

	return Invite{Fingerprint: w.FP, Name: w.Name, Addrs: w.Addrs}.validated()
}

// validated returns a cleaned copy or an error. Shared by both directions so
// the rules cannot drift apart — an encoder that permits what the decoder
// rejects is a bug that only shows up on somebody else's machine.
func (in Invite) validated() (Invite, error) {
	fp := identity.Normalize(in.Fingerprint)
	if len(fp) != fingerprint || !isBase32(fp) {
		return Invite{}, ErrBadFingerpint
	}

	name := cleanName(in.Name)
	if name == "" {
		// Not fatal to the introduction — the fingerprint is the identity — so
		// rather than refuse, stand something in. A blank row in a peer list is
		// a worse outcome than a placeholder.
		name = "LANcast server"
	}

	addrs, err := cleanAddrs(in.Addrs)
	if err != nil {
		return Invite{}, err
	}

	return Invite{Fingerprint: fp, Name: name, Addrs: addrs}, nil
}

// Grouped is the fingerprint in its readable form, for showing the person who
// pasted this what they are about to add.
func (in Invite) Grouped() string { return identity.Group(in.Fingerprint) }

/*
 * cleanName makes a stranger's display string safe to put on a screen.
 *
 * The name is chosen by whoever wrote the invite and appears in a peer list, so
 * it is untrusted display text in the ordinary sense. Control characters go —
 * a newline turns one row into two and an escape sequence can repaint a
 * terminal — and the length is capped so a peer cannot occupy the whole list.
 *
 * Deliberately *not* stripping anything else. Names are people's, they are in
 * every script there is, and a filter that only likes ASCII would mangle most
 * of the world's.
 */
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))

	runes := []rune(s)
	if len(runes) > maxNameLen {
		s = strings.TrimSpace(string(runes[:maxNameLen]))
	}
	return s
}

/*
 * cleanAddrs validates the reachability hints.
 *
 * Each must be host:port, because a peer connection needs a port and guessing
 * one is how you get a confusing failure against something else that happens to
 * be listening. Duplicates are dropped, order is kept — the sender listed the
 * one most likely to work first, and that is worth preserving.
 *
 * The host itself is deliberately not resolved or judged here. Whether an
 * address reaches anything is a question for the reachability poll, and a
 * parser that refused a name it could not resolve today would reject a peer
 * whose machine is merely switched off.
 */
func cleanAddrs(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || len(a) > maxAddrLen {
			continue
		}
		host, port, err := net.SplitHostPort(a)
		if err != nil || host == "" {
			continue
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			continue
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
		if len(out) == maxAddrs {
			break
		}
	}
	if len(out) == 0 {
		return nil, ErrNoAddress
	}
	return out, nil
}

func isBase32(s string) bool {
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z') && !(r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

// stripWhitespace removes every space, tab and line break, so an invite that
// arrived wrapped across two lines still parses. Base64url's alphabet contains
// none of these, so nothing meaningful can be removed.
func stripWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
