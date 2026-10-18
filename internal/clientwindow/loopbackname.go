package clientwindow

import (
	"fmt"
	"strings"
)

/*
 * The server's public name, answered by this machine (passkeys, ADR 0082).
 *
 * A passkey belongs to a name, and this server's is its public one: passkeys
 * work only at that name, over the public certificate. The window used to open
 * the server as localhost, so it never offered one. It now opens the public
 * name when there is one, and this switch makes the window's browser resolve
 * that name to 127.0.0.1 itself: the page is on this machine, as before,
 * without asking DNS, without leaving the house, and without needing the
 * router to loop a connection back in.
 *
 * Only for this window's browser. Nothing else on the machine resolves the
 * name differently.
 */

// loopbackArg is the switch that maps name to this machine, or an error if
// name is not a plain DNS name: the value lands in a command line, and a
// comma or a space would let it add switches of its own.
func loopbackArg(name string) (string, error) {
	if !plainHostname(name) {
		return "", fmt.Errorf("client window: refusing a malformed host name %q", name)
	}
	return "--host-resolver-rules=MAP " + name + " 127.0.0.1", nil
}

// plainHostname is letters, digits, hyphens and dots, in labels of 1 to 63,
// and nothing else.
func plainHostname(name string) bool {
	if name == "" || len(name) > 253 || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
			if !ok {
				return false
			}
		}
	}
	return true
}
