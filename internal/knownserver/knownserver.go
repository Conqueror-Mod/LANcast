// Package knownserver is the desktop client's record of which LANcast servers
// it trusts, and which public key it trusts each of them by.
//
// It exists because of an asymmetry. For a server on this machine the client
// reads the pin straight off local disk ([internal/certpin]), which is a
// stronger check than anything over a network can be: the key is read from the
// same filesystem the server wrote it to. A server on *another* machine leaves
// nothing on this disk, so the only way to learn its key is to be told it by
// whoever is answering at that address — which is trust on first use, and the
// decision [ADR 0070] makes about when that is acceptable.
//
// Everything in this file is pure: parsing an address a person typed, and
// deciding what a stored pin says about an offered one. The network half lives
// in fetch.go and the file half in store.go, for the reason probing is split
// the same way — these rules are worth testing against cases, not against a
// second machine.
//
// [ADR 0070]: ../../docs/adr/0070-the-desktop-client-can-trust-a-server-it-did-not-install.md
package knownserver

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is the port LANcast serves on, applied when somebody types a
// host and nothing else. Typing the port is the common case getting in the way
// of the common case.
const DefaultPort = 8080

// Server is one remembered server.
//
// The address is the identity: a pin means "this key, at this address", and
// the same key at a different address is a different entry because the
// question being answered is always "is this who I reached last time".
type Server struct {
	// Address is canonical host:port, as ParseAddress produces it.
	Address string `json:"address"`
	// Name is what the person called it. Cosmetic, and never matched on.
	Name string `json:"name,omitempty"`
	// Pin is the base64 SHA-256 SPKI of the TLS **serving** key, the same form
	// certpin produces. It answers "is this connection private", and it may be
	// replaced at any time: tlscert regenerates a missing or corrupt
	// certificate by design, and the bring-your-own-certificate path exists so
	// an operator can rotate one.
	Pin string `json:"pin"`
	/*
	 * Identity is the server's ADR 0044 fingerprint, once this client has been
	 * far enough into that server to read it.
	 *
	 * The anchor. It answers a different question from Pin, "is this the
	 * server I know" rather than "is this connection private", and it has a
	 * different lifetime: the identity key is generated only when none exists
	 * and is an error in every other case, so unlike a serving certificate it
	 * cannot quietly become somebody else.
	 *
	 * Empty until recorded, which is an ordinary state and not a fault.
	 * `GET /api/identity` is session-gated (ADR 0044 section 7), so it cannot
	 * be read before there is a trusted transport to sign in over: a record
	 * with no identity is one that has never been signed in to.
	 */
	Identity string `json:"identity,omitempty"`
	// Accepted is when somebody looked at that key and said yes. Kept so the
	// refusal path can say how long the old key had been trusted, which is the
	// difference between "you set this up this morning" and "this has been
	// stable for a year and changed today".
	Accepted time.Time `json:"accepted"`
}

// List is the whole record. A slice rather than a map because it is shown to
// people in an order, and the order somebody added servers in is a better
// default than whatever a map iterates in.
type List struct {
	Servers []Server `json:"servers"`
}

/*
 * Trust is what a stored record says about the key being offered right now.
 *
 * Four outcomes, because three distinct things can be true and collapsing any
 * two of them produces a prompt that lies.
 *
 * "No record" and "wrong key" are the same comparison failing, but one is a
 * question for somebody who has not been asked yet and the other contradicts
 * an answer already given.
 *
 * And a changed *serving* key is not, by itself, either of those. The serving
 * certificate is designed to regenerate: ADR 0044 rejected pinning it as an
 * identity for exactly this reason, and in this project deleting the
 * certificate and key is the documented repair for a certificate whose SANs
 * predate a network interface. A rule that fires on maintenance is worse than
 * no rule, because its refusal is the one people learn to click past. So when
 * an identity is on record, a changed serving key is reported as a rotation to
 * be confirmed rather than as an attack.
 */
type Trust int

const (
	// TrustUnknown means nothing is stored for this address. Ask, showing the
	// key.
	TrustUnknown Trust = iota
	// TrustMatch means the offered serving key is the one that was accepted.
	// Connect.
	TrustMatch
	/*
	 * TrustRotated means the serving key changed at an address whose
	 * *identity* this client knows.
	 *
	 * The likely explanation is a regenerated certificate. It is not proof of
	 * one, so it is confirmed rather than accepted: the client shows the
	 * identity it has and asks for it to be checked out of band. Nothing is
	 * sent to the new key before that, so an impostor answering in the
	 * server's place receives no session.
	 */
	TrustRotated
	/*
	 * TrustMismatch is a changed key that cannot be explained as a rotation:
	 * no identity on record to appeal to, so a replaced certificate and an
	 * impostor are indistinguishable. Refuse.
	 */
	TrustMismatch
)

func (t Trust) String() string {
	switch t {
	case TrustMatch:
		return "match"
	case TrustRotated:
		return "rotated"
	case TrustMismatch:
		return "mismatch"
	default:
		return "unknown"
	}
}

var ErrNoAddress = errors.New("no server address")

/*
 * ParseAddress turns what somebody typed into canonical host:port.
 *
 * Generous about the input and strict about the output, because this string
 * becomes a lookup key: "192.168.1.66" and "https://192.168.1.66:8080/" have
 * to reach the same entry, or the second one is a new server with no pin and
 * the person is asked to trust a machine they are already trusting.
 *
 * A scheme is accepted and discarded. An address is a host and a port; the
 * client decides the scheme by what the server answers with, and a stored
 * "http://" would be a claim about the transport made by whoever typed fastest.
 */
func ParseAddress(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrNoAddress
	}

	// Drop a scheme if one was pasted. Anything other than http/https is
	// somebody pasting the wrong thing entirely and is worth saying so.
	if i := strings.Index(s, "://"); i >= 0 {
		switch strings.ToLower(s[:i]) {
		case "http", "https":
			s = s[i+3:]
		default:
			return "", fmt.Errorf("%q is not an http address", raw)
		}
	}
	// Drop a path, query or fragment: a pasted deep link is still an address.
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	// Credentials in an address are not supported and must not be silently
	// dropped -- a person who typed them expects them to be used.
	if strings.Contains(s, "@") {
		return "", errors.New("a server address cannot carry a username or password")
	}
	if s == "" {
		return "", ErrNoAddress
	}

	host, port, err := splitHostPort(s)
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", ErrNoAddress
	}
	if strings.ContainsAny(host, " \t") {
		return "", fmt.Errorf("%q is not a server address", raw)
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		// An IPv6 literal is bracketed in host:port form, always, so that the
		// result can be joined to a URL without a second rule about when to
		// bracket it.
		return "[" + ip.String() + "]:" + strconv.Itoa(port), nil
	}
	return strings.ToLower(host) + ":" + strconv.Itoa(port), nil
}

// splitHostPort accepts "host", "host:port" and "[v6]:port", returning the
// default port when none was given.
func splitHostPort(s string) (host string, port int, err error) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, fmt.Errorf("%q has no closing bracket", s)
		}
		host = s[1:end]
		rest := s[end+1:]
		if rest == "" {
			return host, DefaultPort, nil
		}
		if !strings.HasPrefix(rest, ":") {
			return "", 0, fmt.Errorf("%q is not a server address", s)
		}
		port, err = parsePort(rest[1:])
		return host, port, err
	}

	// A bare IPv6 literal has several colons and no brackets. Treat it as a
	// host with no port rather than as a host:port with a strange port, which
	// is what a naive LastIndex would make of it.
	if strings.Count(s, ":") > 1 {
		if ip := net.ParseIP(s); ip != nil {
			return s, DefaultPort, nil
		}
		return "", 0, fmt.Errorf("%q is not a server address", s)
	}

	if i := strings.LastIndex(s, ":"); i >= 0 {
		port, err = parsePort(s[i+1:])
		return s[:i], port, err
	}
	return s, DefaultPort, nil
}

func parsePort(s string) (int, error) {
	if s == "" {
		return DefaultPort, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port", s)
	}
	return n, nil
}

// Find returns the stored record for an address, already parsed by the caller.
func (l List) Find(address string) (Server, bool) {
	for _, s := range l.Servers {
		if strings.EqualFold(s.Address, address) {
			return s, true
		}
	}
	return Server{}, false
}

/*
 * Check is the whole trust decision: what does the record say about the
 * serving key being offered at this address?
 *
 * The rule this encodes is the amendment to ADR 0070, and the part worth
 * defending here rather than in a UI is *which key is authoritative*. The pin
 * is over the TLS serving key, and a serving key legitimately changes:
 * tlscert regenerates a missing or corrupt certificate by design, an operator
 * may rotate a supplied one, and deleting the certificate is this project's
 * own documented repair for stale SANs. Treating that as an attack would spend
 * the strongest warning the client has on routine maintenance.
 *
 * So the identity decides. With one on record, a changed serving key is a
 * rotation to be confirmed against something that does not rotate. Without
 * one, the two explanations are indistinguishable and the refusal stands.
 *
 * Neither outcome is a "continue anyway" button. A rotation is confirmed by
 * checking a fingerprint out of band; a mismatch is escaped only by forgetting
 * the server, which is an act on the record rather than a button on an error.
 */
func (l List) Check(address, offeredPin string) Trust {
	known, ok := l.Find(address)
	if !ok {
		return TrustUnknown
	}
	// A stored record with no pin is a record of nothing. Treat it as unknown
	// rather than as a mismatch against the empty string, which would make a
	// corrupted file permanently unusable instead of merely forgotten.
	if known.Pin == "" {
		return TrustUnknown
	}
	if offeredPin != "" && known.Pin == offeredPin {
		return TrustMatch
	}
	if known.Identity != "" {
		return TrustRotated
	}
	return TrustMismatch
}

/*
 * CheckIdentity is the question the serving key cannot answer: is this the
 * server this record is about?
 *
 * Asked once a session exists, since ADR 0044 section 7 keeps
 * `GET /api/identity` behind one. A record with no identity yet accepts the
 * first answer it gets, which is what recording it means; after that a
 * different answer is the strongest refusal this package has, because an
 * identity key is generated only when none exists and never regenerated.
 */
func (l List) CheckIdentity(address, offered string) Trust {
	known, ok := l.Find(address)
	if !ok || known.Identity == "" {
		return TrustUnknown
	}
	if offered != "" && Normalize(known.Identity) == Normalize(offered) {
		return TrustMatch
	}
	return TrustMismatch
}

// Normalize makes two spellings of one fingerprint comparable: case, and the
// grouping separators a person may have typed or a screen may have shown.
func Normalize(fingerprint string) string {
	var b strings.Builder
	for _, r := range fingerprint {
		if r == ' ' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

/*
 * RecordIdentity stores the identity of a server this client has signed in to,
 * and is the moment a record gains its anchor.
 *
 * It never overwrites one. An identity that appears to have changed is a
 * refusal (see CheckIdentity), and a function that quietly wrote the new value
 * would turn that refusal into a formality, which is the whole failure this
 * amendment exists to correct.
 */
func (l List) RecordIdentity(address, fingerprint string) List {
	if fingerprint == "" {
		return l
	}
	out := List{Servers: make([]Server, 0, len(l.Servers))}
	for _, s := range l.Servers {
		if strings.EqualFold(s.Address, address) && s.Identity == "" {
			s.Identity = fingerprint
		}
		out.Servers = append(out.Servers, s)
	}
	return out
}

/*
 * AcceptRotation records a new serving key for a server whose identity was
 * confirmed, keeping everything else about the record.
 *
 * Separate from Accept because it is a different act with a different
 * precondition: Accept is somebody meeting a server for the first time, this
 * is somebody confirming that a server they already know has a new
 * certificate. Folding them together would let the first path silently replace
 * an identity.
 */
func (l List) AcceptRotation(address, newPin string) List {
	out := List{Servers: make([]Server, 0, len(l.Servers))}
	for _, s := range l.Servers {
		if strings.EqualFold(s.Address, address) {
			s.Pin = newPin
		}
		out.Servers = append(out.Servers, s)
	}
	return out
}

// Accept records a key as trusted, replacing any earlier record for the same
// address.
//
// Replacing rather than refusing: Accept is what the UI calls once somebody
// has looked at a fingerprint and said yes, and by then the decision has been
// made. Check is where a mismatch is refused, and the path from a mismatch to
// here goes through Forget on purpose.
func (l List) Accept(s Server) List {
	if s.Accepted.IsZero() {
		s.Accepted = time.Now()
	}
	out := List{Servers: make([]Server, 0, len(l.Servers)+1)}
	replaced := false
	for _, existing := range l.Servers {
		if strings.EqualFold(existing.Address, s.Address) {
			// Keep the name already given unless this call carries one, so
			// re-accepting a rotated key does not silently rename the entry.
			if s.Name == "" {
				s.Name = existing.Name
			}
			out.Servers = append(out.Servers, s)
			replaced = true
			continue
		}
		out.Servers = append(out.Servers, existing)
	}
	if !replaced {
		out.Servers = append(out.Servers, s)
	}
	return out
}

// Forget drops an address. It is the only route from a mismatch back to a
// connection, and it is deliberately a separate act.
func (l List) Forget(address string) List {
	out := List{Servers: make([]Server, 0, len(l.Servers))}
	for _, s := range l.Servers {
		if strings.EqualFold(s.Address, address) {
			continue
		}
		out.Servers = append(out.Servers, s)
	}
	return out
}

/*
 * Fingerprint formats a pin for somebody to read off a screen and compare with
 * another screen.
 *
 * Base64 in one run is unreadable at the only moment it matters -- two people
 * on the phone, one of them reading. Grouped, it can be read aloud and lost
 * place in without starting over.
 */
func Fingerprint(pin string) string {
	const group = 8
	var b strings.Builder
	for i, r := range pin {
		if i > 0 && i%group == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
