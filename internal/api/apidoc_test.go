package api

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

/*
 * docs/api.md against the router.
 *
 * CLAUDE.md calls that document drifting from the handlers the most damaging
 * documentation failure in this project, because it is what third-party clients
 * build against. Nothing checked it for the first thirty ADRs, and when someone
 * finally did, it was wrong three ways at once: it documented
 * /api/settings/providers, which has never existed; it fully specified an
 * endpoint with no handler; and it omitted three live endpoints our own client
 * calls daily. A second audit a day later found five missing item kinds.
 *
 * Twice in two days is not bad luck, it is an unchecked invariant. So it is
 * checked here rather than when someone thinks of it.
 *
 * WHAT THIS TESTS, AND WHAT IT DELIBERATELY DOES NOT
 *
 * Paths, in both directions: every route is mentioned in the document, and
 * every /api path in the document exists as a route.
 *
 * Not methods. The document groups them the way a reader wants them —
 * "### `GET` / `PUT /api/settings`", "### `POST /api/plugins/{name}/enable` ·
 * `/disable`" — and a method-level matcher spent its time reporting those as
 * violations. A test that cries wolf gets deleted, and then nothing is checked
 * at all. Paths catch the failures that actually happened.
 */

var (
	routeRe = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/[^"]*)"`)
	docPath = regexp.MustCompile("`(?:[A-Z]+ )?(/api/[^`]*)`")
	paramRe = regexp.MustCompile(`\{[^}]*\}`)
)

// documentedElsewhere lists paths that appear in the router or the document and
// are deliberately not matched. Every entry needs a reason; an allowlist
// without one becomes a place to hide failures.
var documentedElsewhere = map[string]string{
	// Specified in full, with no handler: theme music is blocked on OST
	// identification (ADR 0005). The section carries a "Not implemented" note
	// saying requesting it is a router 404. Kept because the shape is decided
	// and worth keeping decided.
	"/api/items/{}/theme": "specified, unimplemented — ADR 0005, and the section says so",

	// Not endpoints at all: the versioning policy discusses what a future
	// breaking revision would be called (ADR 0018). They are prose about a
	// prefix that does not exist and is not supposed to.
	"/api/v2": "hypothetical future version, discussed in the versioning policy",
	"/api/vN": "the same, written generically",
}

func normalizePath(p string) string {
	p = strings.SplitN(p, "?", 2)[0]
	p = strings.TrimSuffix(p, ".vtt")
	p = paramRe.ReplaceAllString(p, "{}")
	p = strings.TrimSuffix(p, "/")
	return p
}

func readOrSkip(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestEveryRouteIsDocumented(t *testing.T) {
	src := readOrSkip(t, "server.go")
	doc := readOrSkip(t, "../../docs/api.md")

	documented := map[string]bool{}
	for _, m := range docPath.FindAllStringSubmatch(doc, -1) {
		documented[normalizePath(m[1])] = true
	}

	var missing []string
	for _, m := range routeRe.FindAllStringSubmatch(src, -1) {
		p := normalizePath(m[2])
		if !strings.HasPrefix(p, "/api/") {
			continue // static assets and the SPA fallback are not the contract
		}
		if documented[p] || documentedElsewhere[p] != "" {
			continue
		}
		missing = append(missing, m[1]+" "+m[2])
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("%d route(s) are not in docs/api.md:\n  %s\n\n"+
			"An endpoint that is not in the contract is one a third-party client "+
			"cannot know about. Document it in the same commit that adds it.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func TestEveryDocumentedEndpointExists(t *testing.T) {
	src := readOrSkip(t, "server.go")
	doc := readOrSkip(t, "../../docs/api.md")

	routes := map[string]bool{}
	for _, m := range routeRe.FindAllStringSubmatch(src, -1) {
		routes[normalizePath(m[2])] = true
	}

	seen := map[string]bool{}
	var phantom []string
	for _, m := range docPath.FindAllStringSubmatch(doc, -1) {
		p := normalizePath(m[1])
		if seen[p] || routes[p] || documentedElsewhere[p] != "" {
			continue
		}
		seen[p] = true
		phantom = append(phantom, p)
	}
	sort.Strings(phantom)

	if len(phantom) > 0 {
		t.Errorf("%d documented path(s) have no route:\n  %s\n\n"+
			"A client following the document gets a 404 — which reads as a "+
			"permissions or configuration problem, not a typo in our docs. "+
			"Either the path is wrong or the endpoint was removed.",
			len(phantom), strings.Join(phantom, "\n  "))
	}
}

// The allowlist is a liability if it outlives its reasons: an entry for an
// endpoint that has since been implemented would hide that endpoint going
// undocumented again.
func TestAllowlistEntriesAreStillNeeded(t *testing.T) {
	src := readOrSkip(t, "server.go")
	routes := map[string]bool{}
	for _, m := range routeRe.FindAllStringSubmatch(src, -1) {
		routes[normalizePath(m[2])] = true
	}
	for p, why := range documentedElsewhere {
		if routes[p] {
			t.Errorf("%s is allowlisted as %q but now has a route — "+
				"remove the entry and document it properly", p, why)
		}
	}
}
