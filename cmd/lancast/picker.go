package main

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"strings"
)

/*
 * The server picker is a page this program carries, not a page a server
 * serves.
 *
 * It has to be, and the reason is the whole situation: this screen is shown
 * when there is no server to ask. Either nothing is trusted yet, or the
 * remembered server has been forgotten, or the one that was there is refusing
 * to be what it was. A picker fetched from a server could not appear in any of
 * those cases -- which are the only cases it exists for.
 *
 * It is given to the web view with NavigateToString, so it has no origin, no
 * cookies and no network of its own. Everything it does it does through the
 * bindings in servers.go, which is why those bindings decide and this page
 * only draws.
 */

//go:embed picker.html
var pickerHTML string

// pickerPage is the picker with its opening state substituted in.
//
// The state is a placeholder in the HTML rather than a binding call, because
// the first thing this page must do is say *why* it is on screen, and a page
// that renders blank and then explains itself a moment later reads as a fault.
func pickerPage(reason string) string {
	page := strings.Replace(pickerHTML, "__REASON__", htmlEscape(reason), 1)
	return strings.ReplaceAll(page, "__PICKER_TOKEN__", pickerToken)
}

/*
 * pickerToken is a secret only the picker page carries (review finding
 * R11-2).
 *
 * Every binding is injected into every document the window shows, and since
 * ADR 0070 that includes remote servers' pages. A script on one could call
 * lancastServerAccept and make this computer trust, and switch to, a server
 * nobody looked at. The bindings that decide trust -- listing, probing,
 * accepting, connecting, forgetting -- now refuse a caller that cannot name
 * this value. It is written into the page this program hands the view with
 * NavigateToString, and a server's page cannot read another document, so
 * holding it means being the picker.
 *
 * Per process, never stored and never logged. 128 bits from crypto/rand.
 */
var pickerToken = func() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("client: no randomness for the picker token: " + err.Error())
	}
	return hex.EncodeToString(b)
}()

// fromPicker reports whether a binding call carried the picker's token.
func fromPicker(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(pickerToken)) == 1
}

// notFromPicker is the answer to a trust binding called from anywhere else.
func notFromPicker() map[string]any {
	return map[string]any{"error": "only the server picker can do this"}
}

// htmlEscape is small on purpose: the only untrusted thing substituted into
// this page is a reason string built by this program, and pulling in
// html/template for one replacement would hide that fact rather than state it.
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
