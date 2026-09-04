package api

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"lancast/internal/backup"
	"lancast/internal/crashlog"
	"lancast/internal/enrich"
	"lancast/internal/faces"
	"lancast/internal/meta"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/together"
	"lancast/internal/transcode"
)

/*
 * Every field that reaches a client has a name somebody chose.
 *
 * WHY THIS EXISTS
 *
 * store.ResolutionBucket shipped with no json tags. Facets is written straight
 * to the response, so the wire carried `Key`, `Label`, `MinWidth`, `MaxWidth`
 * while docs/api.md documented `key`, `label`, `min_width`, `max_width` and the
 * client read the documented names. Every resolution chip rendered with an
 * undefined label, and pressing one sent `resolution=undefined` — which the
 * contract says is ignored rather than rejected. The filter was a silent no-op
 * for its entire existence.
 *
 * Two suites had a chance and neither took it, for a reason worth stating
 * because it generalises: the Go test asserted `b.MinWidth`, which is the
 * struct; the client test built its fixture in snake_case, which is what the
 * client believed. Each was correct about its own half and neither looked at
 * the bytes between them.
 *
 * An untagged exported field is not a decision. Its wire name is an accident of
 * how somebody spelled a Go identifier, and it changes if the field is renamed
 * for reasons that have nothing to do with the API. A field name is part of the
 * contract (ADR 0018), so it has to be written down where it is served from.
 *
 * WHAT THIS CHECKS, AND WHAT IT DOES NOT
 *
 * It checks that every exported field of every type reachable in a response
 * carries an explicit `json` tag, and that the names are lower_snake_case like
 * the rest of the contract.
 *
 * It does not check that the names match docs/api.md — nothing here reads that
 * document, and openapi_test.go plus docs/openapi.json are where a name is
 * compared against what was published. This is the cheaper half: it makes the
 * name deliberate. The spec makes it correct.
 *
 * It also cannot see most of this API. 77 of 121 responses are assembled as
 * map[string]any with the keys written out by hand at the call site, and a key
 * spelled there is already a deliberate choice — there is no Go identifier for
 * it to leak. What this reaches is the typed values: the ones passed to
 * writeJSON whole, and every struct nested inside a map response. That is where
 * the bug lived, because ResolutionBucket arrived on the wire inside a Facets
 * that was itself passed whole.
 *
 * So a type belongs in wireTypes only if it is serialized *as a type*. A struct
 * the handler unpacks into a map field by field is not on the wire; its fields
 * are, under names the handler chose. store.NextEpisode is the example — the
 * continue handler writes {resume, exhausted, episode} by hand, so the struct's
 * untagged fields never reach anybody, and listing it here reported three
 * problems that do not exist.
 *
 * THE REGISTRY IS THE POINT
 *
 * wireTypes is a hand-maintained list of what this API serializes, and that is
 * a feature rather than a chore: reflection cannot discover it, because most
 * responses are assembled as map[string]any and the typed values are nested
 * inside them. Adding a type here when a handler starts returning it is the
 * same obligation as documenting the endpoint, and it is cheap next to finding
 * out from a filter that quietly does nothing.
 */

// wireTypes is every type this API serializes into a response body, with where
// it appears. The walk recurses, so a type reachable from one listed here need
// not be listed itself — Item is enough to reach Credit, Artwork and Progress.
//
// Only types serialized *as types* belong here, and every entry was read out of
// its handler. Several plausible-looking ones are deliberately absent because
// the handler unpacks them into a map field by field: mediatools.Progress,
// probe.Stats, update.State, presence.State and identity.Identity all reach the
// client under keys written at the call site, so their Go field names are not
// on the wire and reporting them would be three false alarms each.
//
// store.Peer is the subtlest of them. peerJSON builds the map, and it does not
// merely rename: it adds fingerprint_display, which the struct has no field
// for, and it always writes last_seen where the struct carries omitempty. The
// contract says last_seen must be 0 rather than absent for a peer that has
// never answered — "never" and "three days ago" being different statements —
// so the map is right and serializing the struct would be wrong.
//
// store.User and store.ExternalSubtitle were listed here and should not have
// been, which is the same mistake in the other direction: listing a type that
// is not on the wire checks a shape nobody receives, and quietly leaves the one
// that *is* unchecked. listUsers builds its rows with userJSON and the other
// account routes answer managedUserView; the subtitle list answers
// subtitleTrack, which is now listed in ExternalSubtitle's place. Read the
// handler, not the package.
var wireTypes = []struct {
	where string
	value any
}{
	// Libraries and their locations.
	{"GET /api/libraries", store.Library{}},
	{"GET /api/libraries/{id}/roots", store.LibraryRoot{}},
	{"GET /api/libraries/{id}/scan · shape_warning", store.ShapeWarning{}},

	// Items, and everything hanging off one.
	{"GET /api/items", store.Item{}},
	{"GET /api/items/{id} · streams", store.MediaStream{}},
	{"GET /api/items/{id} · credits", store.Credit{}},
	{"GET /api/items/{id} · artwork", store.Artwork{}},
	{"GET /api/items/{id} · ratings", store.ItemRating{}},
	{"GET /api/items/{id}/progress", store.Progress{}},

	// Browse filters — the ones that were wrong.
	{"GET /api/libraries/{id}/facets", store.Facets{}},
	{"GET /api/libraries/{id}/facets · resolutions", store.ResolutionBucket{}},
	{"GET /api/libraries/{id}/facets · collections", store.CollectionFacet{}},
	{"GET /api/libraries/{id}/cast", store.CastMember{}},

	// Scanning.
	{"POST /api/libraries/{id}/scan", scan.Progress{}},
	{"GET /api/libraries/{id}/scan · issues", scan.Issue{}},
	{"GET /api/libraries/{id}/scan · roots_skipped", scan.SkippedRoot{}},

	// Playback.
	{"GET /api/items/{id}/playback · decision", probe.Decision{}},
	{"GET /api/transcode · encoder", transcode.Encoder{}},
	{"GET /api/transcode · sessions", transcode.SessionInfo{}},

	// Profile, history and ratings.
	{"GET /api/profile", profileResponse{}},
	{"GET /api/profile · stats", store.ProfileStats{}},
	{"GET /api/profile · history", store.HistoryEntry{}},
	{"GET /api/items/{id}/rating", store.Rating{}},
	{"GET /api/profile/ratings", store.RatedItem{}},

	// Reports.
	{"GET /api/libraries/{id}/trending", trendingResponse{}},
	{"GET /api/libraries/{id}/trending · items", store.TrendingItem{}},
	{"GET /api/libraries/{id}/timeline · buckets", store.TimelineBucket{}},
	{"GET /api/collisions", store.Collision{}},
	{"GET /api/collisions · members", store.CollisionMember{}},
	{"GET /api/audit", store.AuditEvent{}},

	// Accounts and peers.
	{"GET /api/users", managedUserView{}},

	// Subsystems.
	{"GET /api/faces/capabilities", faces.Capabilities{}},
	{"GET /api/together/{id}", together.Session{}},
	{"GET /api/together/{id} · members", together.Member{}},
	{"GET /api/backups", backup.File{}},
	{"GET /api/backups", backupsResponse{}},
	// Live TV. Parked as a feature (2026-08-29) but the endpoints still serve,
	// and a parked feature's contract is still a contract.
	{"GET /api/channels", store.Channel{}},
	{"GET /api/channel-sources", store.ChannelSource{}},
	{"GET /api/guide", store.Program{}},

	// Subtitles, plugins, people.
	{"GET /api/items/{id}/subtitles", subtitleTrack{}},
	{"GET /api/plugins", pluginView{}},
	{"GET /api/plugins · capabilities", capsView{}},
	{"GET /api/people", store.Person{}},
	{"GET /api/people/peers", store.RemotePerson{}},

	// Workers and status.
	{"GET /api/enrich", enrich.Stats{}},
	{"GET /api/crashes", crashlog.Report{}},
	{"GET /api/federation/presence", visible{}},
}

/*
 * untaggedByDesign is for types whose wire names are deliberately not
 * snake_case tags, with the reason. Every entry is a liability, so each one
 * says why it is not simply a bug.
 */
var untaggedByDesign = map[reflect.Type]string{
	reflect.TypeOf(meta.Candidate{}): "" +
		"PascalCase is the published contract here, not an accident that " +
		"survived. docs/api.md documents this response as {\"Provider\": …, " +
		"\"ExternalID\": …, \"Title\": …} and the client reads those names, so " +
		"server, document and client already agree — which is exactly what " +
		"ResolutionBucket did not do. Renaming the fields would be a breaking " +
		"change under ADR 0018 for no gain beyond tidiness, and it would need " +
		"/api/v2. Worth fixing the day something else forces that revision; " +
		"not worth breaking a working client for on its own.",
}

// snakeCase matches the naming the rest of the contract uses. Digits are
// allowed inside a word (`pcm_s16le`, `hevc10`), and a single lowercase word is
// the common case.
func isSnakeCase(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			return false // leading, trailing or doubled underscore
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}

// walkWire visits every struct reachable from t, reporting through report.
func walkWire(t reflect.Type, path string, seen map[reflect.Type]bool, report func(string)) {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice ||
		t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	// time.Time and the like marshal themselves; descending into their private
	// fields would report nonsense.
	if t.PkgPath() == "time" {
		return
	}
	if seen[t] {
		return // Item.NextEpisode is an *Item; without this the walk never ends
	}
	seen[t] = true
	defer delete(seen, t)

	if why, ok := untaggedByDesign[t]; ok && why != "" {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // unexported: encoding/json ignores it
		}

		tag, hasTag := f.Tag.Lookup("json")
		name := strings.Split(tag, ",")[0]

		if f.Anonymous && !hasTag {
			// An embedded struct promotes its fields; walk it in place.
			walkWire(f.Type, path, seen, report)
			continue
		}

		switch {
		case !hasTag:
			report(fmt.Sprintf("%s.%s has no json tag", path, f.Name))
			continue
		case name == "-":
			continue // deliberately withheld
		case name == "":
			// `json:",omitempty"` keeps the Go field name, which is the same
			// accident as having no tag at all.
			report(fmt.Sprintf("%s.%s has a json tag that names nothing, so it "+
				"serializes as %q", path, f.Name, f.Name))
			continue
		case !isSnakeCase(name):
			report(fmt.Sprintf("%s.%s serializes as %q, which is not "+
				"lower_snake_case like the rest of the contract", path, f.Name, name))
		}

		walkWire(f.Type, path+"."+name, seen, report)
	}
}

func TestEveryWireFieldHasADeliberateName(t *testing.T) {
	var problems []string
	for _, entry := range wireTypes {
		rt := reflect.TypeOf(entry.value)
		walkWire(rt, rt.String(), map[reflect.Type]bool{}, func(msg string) {
			problems = append(problems, fmt.Sprintf("%s\n      (%s)", msg, entry.where))
		})
	}

	problems = dedupe(problems)
	if len(problems) > 0 {
		t.Errorf("%d field(s) reach a client under a name nobody chose:\n  - %s\n\n"+
			"An untagged exported field takes its wire name from the Go "+
			"identifier, so renaming the field for internal reasons silently "+
			"renames it in the API. That is how the resolution filter shipped "+
			"broken: the server sent MinWidth, the document promised min_width, "+
			"and nothing compared them.\n\n"+
			"Add the tag. If the name genuinely must stay as it is, put the type "+
			"in untaggedByDesign with the reason.",
			len(problems), strings.Join(problems, "\n  - "))
	}
}

// The allowlist is a liability if it outlives its reason, so an entry that no
// longer applies has to go.
func TestUntaggedByDesignEntriesStillApply(t *testing.T) {
	for rt, why := range untaggedByDesign {
		if why == "" {
			t.Errorf("%s is allowlisted with no reason", rt)
			continue
		}
		var found bool
		for i := 0; i < rt.NumField(); i++ {
			if _, tagged := rt.Field(i).Tag.Lookup("json"); !tagged {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is in untaggedByDesign but every field is now tagged — "+
				"remove the entry, or it will hide the next one that is not", rt)
		}
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
