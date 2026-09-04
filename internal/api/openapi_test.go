package api

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

/*
 * docs/openapi.json against the router.
 *
 * apidoc_test.go already checks docs/api.md, and deliberately checks paths
 * only — the prose groups methods the way a reader wants them, and a
 * method-level matcher against prose spent its time crying wolf. That left the
 * half that actually breaks a third-party client unchecked: methods, request
 * and response shapes, status codes.
 *
 * The spec is where those live, and unlike prose it is machine-readable, so it
 * can be checked exactly. It is also what generates web/src/api/types.ts, which
 * is the other reason it has to be right: a wrong spec is now a wrong client,
 * silently, at build time.
 *
 * WHY A RATCHET RATHER THAN A BIG BANG
 *
 * 153 routes cannot be specified accurately in one sitting, and a spec written
 * fast is a spec written wrong — which is worse than no spec, because the
 * generated types would then be confidently incorrect. So the document is
 * written section by section, and pendingSpec lists what has not been reached.
 *
 * The list only ever shrinks, and three properties hold while it does:
 *
 *   - a NEW route is never silently unspecified: it would be in neither the
 *     spec nor the list, and TestRoutesAreSpecifiedOrPending fails;
 *   - an entry that has since been specified must be deleted, or
 *     TestPendingEntriesAreStillPending fails — so the list cannot rot into a
 *     place where a regression hides;
 *   - an entry whose route has been removed must also be deleted, for the same
 *     reason.
 *
 * That is the same shape as apidoc_test.go's allowlist, for the same reason.
 */

// specBase is servers[0].url in docs/openapi.json. Paths in the document are
// relative to it, so "/health" in the spec is "/api/health" on the router.
const specBase = "/api"

// pendingSpec is every route not yet in docs/openapi.json.
//
// One shared reason rather than one each: they are all the same reason, which
// is that the spec is being written a section at a time and this section has
// not been reached. An entry here is a promise, not an exemption — delete it
// when the endpoint is specified.
//
// Nothing new may be added to this list. A new endpoint gets specified in the
// commit that adds it, the way docs/api.md already has to be.
var pendingSpec = []string{
	"DELETE /api/channel-sources/{id}",
	"DELETE /api/items/{id}",
	"DELETE /api/items/{id}/locks/{field}",
	"GET /api/artwork/{hash}",
	"GET /api/channel-sources",
	"GET /api/channels",
	"GET /api/channels/{id}/guide",
	"GET /api/channels/{id}/hls/index.m3u8",
	"GET /api/channels/{id}/hls/{session}/{name}",
	"GET /api/channels/{id}/live",
	"GET /api/channels/{id}/stream",
	"GET /api/coverart",
	"GET /api/guide",
	"GET /api/items/{id}/candidates",
	"GET /api/items/{id}/continue",
	"GET /api/items/{id}/episodes",
	"GET /api/items/{id}/photo",
	"GET /api/items/{id}/trailer",
	"PATCH /api/channel-sources/{id}",
	"PATCH /api/items/{id}",
	"POST /api/channel-sources",
	"POST /api/channel-sources/{id}/refresh",
	"POST /api/coverart/refresh",
	"POST /api/items/{id}/match",
	"POST /api/items/{id}/refresh",
	"PUT /api/items/{id}/poster",
	"PUT /api/items/{id}/sensitive",
}

// openapiDoc mirrors only the parts of the document these tests read.
type openapiDoc struct {
	OpenAPI string                                `json:"openapi"`
	Paths   map[string]map[string]json.RawMessage `json:"paths"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
}

var httpVerbs = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"patch": true, "head": true, "options": true, "trace": true,
}

func loadSpec(t *testing.T) (*openapiDoc, map[string]any) {
	t.Helper()
	b, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatalf("read docs/openapi.json: %v", err)
	}
	var doc openapiDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("docs/openapi.json is not valid JSON: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("docs/openapi.json is not valid JSON: %v", err)
	}
	return &doc, raw
}

// specOperations returns every "METHOD /api/path" the document declares.
func specOperations(t *testing.T, doc *openapiDoc) map[string]bool {
	t.Helper()
	if len(doc.Servers) == 0 || doc.Servers[0].URL != specBase {
		t.Fatalf("servers[0].url is not %q — every path in this test is built by "+
			"prefixing it, so a change there silently breaks the whole check", specBase)
	}
	ops := map[string]bool{}
	for p, item := range doc.Paths {
		for method := range item {
			// "parameters" is a sibling of the verbs, not one of them.
			if !httpVerbs[method] {
				continue
			}
			ops[strings.ToUpper(method)+" "+specBase+p] = true
		}
	}
	return ops
}

// routerOperations returns every "METHOD /api/path" the router serves.
func routerOperations(t *testing.T) map[string]bool {
	t.Helper()
	src := readOrSkip(t, "server.go")
	ops := map[string]bool{}
	for _, m := range routeRe.FindAllStringSubmatch(src, -1) {
		if !strings.HasPrefix(m[2], "/api/") {
			continue // static assets and the SPA fallback are not the contract
		}
		ops[m[1]+" "+m[2]] = true
	}
	return ops
}

// Path parameters are compared by name rather than normalised away: a spec that
// calls it {libraryId} where the router calls it {id} generates a client that
// builds the wrong URL, and that is exactly the class of fault this document
// exists to stop.
func TestSpecOperationsHaveRoutes(t *testing.T) {
	doc, _ := loadSpec(t)
	spec := specOperations(t, doc)
	routes := routerOperations(t)

	var phantom []string
	for op := range spec {
		if !routes[op] {
			phantom = append(phantom, op)
		}
	}
	sort.Strings(phantom)

	if len(phantom) > 0 {
		t.Errorf("%d specified operation(s) have no route:\n  %s\n\n"+
			"A client generated from this spec would call an endpoint that answers "+
			"404, which reads as a permissions or configuration problem rather than "+
			"a mistake in our contract.",
			len(phantom), strings.Join(phantom, "\n  "))
	}
}

func TestRoutesAreSpecifiedOrPending(t *testing.T) {
	doc, _ := loadSpec(t)
	spec := specOperations(t, doc)
	routes := routerOperations(t)

	pending := map[string]bool{}
	for _, p := range pendingSpec {
		pending[p] = true
	}

	var missing []string
	for op := range routes {
		if !spec[op] && !pending[op] {
			missing = append(missing, op)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("%d route(s) are in neither docs/openapi.json nor pendingSpec:\n  %s\n\n"+
			"An endpoint that is not in the spec is one no generated client can "+
			"reach. Specify it in the same commit that adds it — pendingSpec is for "+
			"the existing migration, not for new work.",
			len(missing), strings.Join(missing, "\n  "))
	}

	t.Logf("openapi.json covers %d of %d operations, %d pending",
		len(spec), len(routes), len(routes)-len(spec))
}

func TestPendingEntriesAreStillPending(t *testing.T) {
	doc, _ := loadSpec(t)
	spec := specOperations(t, doc)
	routes := routerOperations(t)

	seen := map[string]bool{}
	for _, p := range pendingSpec {
		switch {
		case spec[p]:
			t.Errorf("%s is in pendingSpec but is now specified — delete the entry.\n"+
				"A stale entry is a hole: it would let the endpoint fall back out of "+
				"the spec without anything failing.", p)
		case !routes[p]:
			t.Errorf("%s is in pendingSpec but is no longer a route — delete the entry.", p)
		}
		if seen[p] {
			t.Errorf("%s appears in pendingSpec twice", p)
		}
		seen[p] = true
	}
}

/*
 * Every $ref resolves.
 *
 * The document is hand-authored JSON, and a typo in a $ref is not a syntax
 * error — it is a dangling pointer that openapi-typescript resolves to
 * `unknown`. The client still compiles. The type is simply gone, and nothing
 * says so.
 */
func TestSpecRefsResolve(t *testing.T) {
	_, raw := loadSpec(t)

	var refs []string
	var walk func(any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			for k, child := range node {
				if k == "$ref" {
					if s, ok := child.(string); ok {
						refs = append(refs, s)
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(raw)

	if len(refs) == 0 {
		t.Fatal("no $refs found at all — the walk is broken, not the document")
	}

	for _, ref := range refs {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("%s is not a local reference; this document does not use external files", ref)
			continue
		}
		cur := any(raw)
		ok := true
		for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, isMap := cur.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			next, exists := m[seg]
			if !exists {
				ok = false
				break
			}
			cur = next
		}
		if !ok {
			t.Errorf("%s does not resolve — a dangling $ref becomes `unknown` in the "+
				"generated client without anything failing", ref)
		}
	}
}

// The version is load-bearing: openapi-typescript treats 3.0 and 3.1
// nullability differently, and this document uses 3.1's ["integer", "null"].
func TestSpecIsOpenAPI31(t *testing.T) {
	doc, _ := loadSpec(t)
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Errorf("openapi is %q; this document is written against 3.1 — nullable "+
			"fields use 3.1 type arrays and would silently change meaning under 3.0",
			doc.OpenAPI)
	}
}
