package api

import (
	"net/http"
	"strconv"
	"strings"
)

/*
 * The API contract, made checkable rather than merely written down.
 *
 * ADR 0018 has stated the policy since M3: `/api` is permanently version 1, a
 * breaking revision ships at `/api/v2`, additive changes may land at any time,
 * and clients must ignore unknown fields and tolerate unknown enum values. All
 * of that was documentation and nothing enforced it — `GET /api/health`
 * reported `api_version` and no other route mentioned versions at all.
 *
 * The roadmap keeps this under "dependencies that constrain ordering" with the
 * note *before any third-party client exists — cheap now, breaking later*. This
 * is the cheap version, and it is deliberately not a URL-space rewrite: moving
 * every route under `/api/v1` would break our own client today to buy a
 * property nobody is using yet, and ADR 0018 already promises `/api` never
 * changes meaning, which is the same guarantee at no cost.
 *
 * What it adds is the two halves a client actually needs:
 *
 *   - **Every response says what it is.** `X-LANcast-API-Version` on each /api
 *     response, so a client can log or assert the contract it was served
 *     without a separate call to /health.
 *   - **A client may state what it expects**, by sending the same header. If
 *     this server cannot serve that version it is refused *immediately and by
 *     name*, rather than the client discovering the mismatch as a field that is
 *     mysteriously absent three screens later.
 *
 * The refusal is the valuable half. A client built against v2 talking to a v1
 * server otherwise fails somewhere arbitrary, and the report that arrives is
 * "the library page is blank".
 */

// APIVersionHeader is both the request assertion and the response statement.
const APIVersionHeader = "X-LANcast-API-Version"

// SupportedAPIVersions is every contract revision this build can serve. It is a
// list rather than a number because a future v2 server is expected to keep
// answering v1 for at least one release (ADR 0018), and a client asking "can
// you still do 1" deserves a real answer.
var SupportedAPIVersions = []int{APIVersion}

func supportsAPIVersion(v int) bool {
	for _, s := range SupportedAPIVersions {
		if s == v {
			return true
		}
	}
	return false
}

/*
 * negotiateAPIVersion refuses a request that asks for a contract this build
 * does not serve, and stamps the served version on everything else.
 *
 * Absent header means "whatever you have", which is what every existing client
 * sends and must keep working — an opt-in assertion, not a required one. A
 * header that is not a number is a client bug worth naming rather than
 * ignoring, because silently serving it would hide the fault at exactly the
 * moment somebody is trying to find it.
 */
func negotiateAPIVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the API carries this. The embedded client is served from the
		// same origin and has no contract of its own.
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set(APIVersionHeader, strconv.Itoa(APIVersion))

		if raw := strings.TrimSpace(r.Header.Get(APIVersionHeader)); raw != "" {
			asked, err := strconv.Atoi(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_request",
					APIVersionHeader+" must be a whole number")
				return
			}
			if !supportsAPIVersion(asked) {
				// 400 rather than 406: the request is malformed with respect to
				// this server, and a client that asked for v2 cannot fix it by
				// renegotiating. The code is what a client switches on.
				writeError(w, http.StatusBadRequest, "unsupported_api_version",
					"this server speaks API version "+versionList()+
						"; the request asked for "+raw)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func versionList() string {
	parts := make([]string, 0, len(SupportedAPIVersions))
	for _, v := range SupportedAPIVersions {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ", ")
}
