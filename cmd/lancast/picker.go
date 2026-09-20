package main

import (
	_ "embed"
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
	return strings.Replace(pickerHTML, "__REASON__", htmlEscape(reason), 1)
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
