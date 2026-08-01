# Plugin runtime — build plan

The first concrete M4 build: the WebAssembly plugin runtime
([ADR 0020](adr/0020-plugin-isolation-boundary.md)) and the first-party OMDb
plugin that validates it. Scope framing is in
[m4-plugin-scope.md](m4-plugin-scope.md); the isolation decision is ADR 0020.
This plan turns that decision into reviewable increments.

**Goal of the whole build:** prove [ADR 0007](adr/0007-provider-and-localsource-split.md)'s
promise — a plugin registers into the same `Registry` as a `RatingSource` with no
downstream change — by running OMDb across the WASM boundary and getting the
identical ratings the native source produces today. Distribution and trust
(signing, install-time grants) are a **separate later ADR**, deliberately not in
this build.

## Constraints carried in

- **wazero, pure Go** — the host stays one static binary ([ADR 0001](adr/0001-go-and-pure-go-sqlite.md)).
- **Deny-by-default** — a module gets only the host functions its manifest
  capabilities map to; no ambient file, socket, DB, or secret access (ADR 0020).
- **`store` never crosses the boundary** — plugins return data; the host owns
  persistence (the permanent line from the scope doc).
- Standard Go toolchain builds the guest: `GOOS=wasip1 GOARCH=wasm` with
  `go:wasmimport` host functions — **no TinyGo dependency** (confirmed on the
  repo's Go 1.26 toolchain).

## Phase A — Host runtime + ABI (`internal/plugin`)

The load-bearing, boilerplate-heavy phase. Everything after it is thin.

- **Runtime**: wraps a `wazero.Runtime`, compiles and instantiates a `.wasm`
  module with a host-function set derived from its manifest. One module instance
  per call is the simple default; a pooled instance is a later optimisation, not
  a Phase-A concern.
- **Manifest** (`plugin.json` beside the `.wasm`): `name`, `version`, `abi`
  version, `kind` (`rating_source` first), and `capabilities` —
  `http: ["www.omdbapi.com"]`, `secrets: ["omdb_key"]`. Unknown capability =
  refuse to load, not silently ignore.
- **Host functions** (the only authority a module gets):
  - `host_log(level, msg)` — diagnostics.
  - `host_http_get(url) -> bytes` — **restricted to manifest hosts**; the host
    makes the call, the module never holds a socket. A non-declared host is a
    hard error inside the call.
  - `host_secret(name) -> value` — returns only secrets named in the manifest and
    configured by the user; anything else returns empty.
- **ABI**: length-prefixed JSON over linear memory. The guest exports `alloc`
  (and the host reads the module's memory); calls pass `(ptr, len)` i32 pairs,
  return a packed `(ptr<<32)|len`. A small `abi` package holds the marshalling on
  both sides so host and guest cannot drift. JSON keeps the first ABI legible and
  debuggable; a tighter encoding is a later, measured change behind the `abi`
  version.
- **Tests**: a checked-in tiny fixture `.wasm` (built from a ~30-line guest in
  `internal/plugin/testdata/`, with its source and the one-line build command
  committed beside it) exercising each host function — a declared-host fetch
  succeeds, an undeclared one is refused, an ungranted secret returns empty. This
  keeps CI free of a wasm build step while keeping the fixture reproducible.

**Done when:** a fixture module round-trips a JSON request/response and each host
function enforces its capability, all under `go test` with no network and no
external toolchain.

## Phase B — `RatingSource` adapter + registry wiring

- `pluginRatingSource` implements `meta.RatingSource`: its `Ratings(imdbID)`
  marshals the id in, invokes the module's exported `ratings` entrypoint, and
  unmarshals `[]meta.Rating` out — the ADR 0007 adapter, made real.
- **Discovery**: at startup (and on the settings rebuild hook), scan a
  `plugins/` dir under the data dir for `plugin.json` + `.wasm` pairs, load each,
  and register discovered rating sources into the same `Registry` the native
  OMDb/TMDB sources use. A malformed or unloadable plugin logs and is skipped —
  one bad plugin never takes down startup.
- **No API or client change** — a plugin-provided rating is indistinguishable
  downstream from a native one, which is the whole point.

**Done when:** a loaded plugin's ratings flow through enrichment and appear on the
detail page exactly as native ratings do, proven by an integration test using the
Phase-A fixture as a stand-in rating source.

## Phase C — First-party OMDb plugin + guest SDK

- **Guest SDK** (`plugin/sdk`, its own module built to wasm): the guest side of
  the `abi` marshalling and typed wrappers over the `go:wasmimport` host
  functions, so a plugin author writes Go against a clean interface, not raw
  memory pokes.
- **OMDb plugin**: reimplement the OMDb parse/normalize logic (it already exists,
  pure, in `internal/meta/omdb`) as a guest plugin using `host_http_get` +
  `host_secret("omdb_key")`. Ship the built `.wasm` + manifest as a first-party
  plugin.
- **Validation test**: with the native OMDb source disabled and the plugin
  loaded, the same imdb id yields the same normalized scores. This is the ADR
  0007 promise demonstrated end to end — the real acceptance criterion for the
  whole build.
- **Native OMDb stays** for now; the plugin is the *proof*, not yet the
  replacement. Whether to retire the native source in favour of the plugin is a
  follow-up decision once the boundary has earned trust.

**Done when:** OMDb-as-plugin produces byte-identical ratings to the native path
against the same fixtures.

## Explicitly out of this build

- **Distribution, signing, install-time capability grants, revocation** — the
  Kodi-failure-mode area, its own ADR, built after the contract has stabilised
  against this first plugin.
- **Any client-facing Add-ons page** — an M4 consumer, not a prerequisite.
- **New `kind`s / new library types from plugins** — the first contract is
  "new source for an existing capability" only; widening it waits for a real
  second plugin that needs it.
- **Instance pooling / performance tuning** — correctness and the capability
  guarantees first; a one-instance-per-call baseline is fine to start.

## Sequencing & PRs

1. **PR 1 — Phase A** (`internal/plugin`: runtime, manifest, host functions, ABI,
   fixture tests). The bulk of the novel code.
2. **PR 2 — Phase B** (adapter + discovery + registry wiring + integration test).
3. **PR 3 — Phase C** (guest SDK + OMDb plugin + equivalence test).

Each is independently reviewable; the boundary and its guarantees land and are
tested in PR 1 before anything depends on them.

## Open questions to settle during Phase A

- **ABI versioning** vs the HTTP API's ADR 0018 — a distinct contract with its
  own version field in the manifest; confirm the mismatch policy (host refuses a
  module whose `abi` major it does not implement).
- **Where the built `.wasm` lives in the repo** — committed artifact + source +
  build command, vs a CI build step. Leaning committed-artifact to keep CI
  toolchain-free, matching how `internal/web/dist` is handled.
- **wazero version pinning** and its place in the ADR 0013 audit posture — record
  the version reviewed.
