# ADR 0020 — Plugin isolation boundary: WebAssembly

Date: 2026-08-01 · Status: accepted

## Context

M4 is the plugin runtime — the milestone the whole architecture has pointed at.
[ADR 0007](0007-provider-and-localsource-split.md) made a promise: the plugin
runtime "registers into the same `Registry` with no downstream change." The
extension *shapes* are already discovered and stable — `Provider`,
`LocalSource`, `RatingSource`, `TrailerProvider` — so M4 is not about designing
an extension API. It is about **running a third party's implementation of those
interfaces without letting it read the library, the database, secrets, or the
filesystem.** The scope and ordering are in [m4-plugin-scope.md](../m4-plugin-scope.md);
this ADR decides the single load-bearing, expensive-to-retrofit piece: the
isolation boundary. Everything else in M4 is shaped by this choice.

Two constraints frame it:

- **The single-binary, no-cgo ethos ([ADR 0001](0001-go-and-pure-go-sqlite.md)).**
  LANcast is one static binary per platform, no runtime, no interpreter, no C
  toolchain. An extension mechanism that reintroduces "ship, sign, and manage
  separate executables" fights the project's stated identity.
- **The audit line ([ADR 0013](0013-transcode-pipeline.md)).** LANcast will not
  link unaudited third-party code into its build — that is why hls.js is not
  vendored. Plugins are third-party code *by definition*, so they cannot be
  linked at all: they **must** run behind a boundary the host controls, not as
  trusted in-process code.

The candidate mechanisms:

- **WebAssembly** via `wazero` (a pure-Go, no-cgo WASM runtime). A plugin is a
  `.wasm` module the host instantiates with an explicit, host-granted set of
  imported functions. Deny-by-default: the module can call only what it is
  handed, and has no ambient access to memory outside its linear memory, to the
  filesystem, or to the network.
- **Subprocess RPC** (the Hashicorp go-plugin shape). A plugin is a separate
  executable spoken to over stdio/gRPC. Authoring is trivial — a plugin is just
  a Go program implementing the existing interfaces — but the host now ships and
  manages N executables, and OS-level sandboxing of a child process is the
  host's problem, solved differently on every platform.
- **Native Go plugins** (`plugin` package) and **embedded scripting** are noted
  only to reject them below.

## Decision

**WebAssembly, via `wazero`, with a deny-by-default capability model.**

A plugin is a `.wasm` module plus a manifest declaring the capabilities it needs.
The host instantiates it with **only** the imported host functions those
capabilities map to; anything not granted does not exist inside the module.

- **No ambient authority.** The module cannot open a file, a socket, or the
  database. It has its own linear memory and the host functions it was given,
  nothing else. This is the property subprocesses do not get for free.
- **Host-mediated egress.** Network access is a host function restricted to the
  hosts named in the manifest (`http: [www.omdbapi.com]`), not a raw socket. The
  host makes the call; the plugin never holds the connection.
- **Secrets are passed, never read ambiently.** A plugin that needs the user's
  OMDb key receives it as a scoped input to its call — an explicitly granted
  capability — not by reading config. It sees the one secret it was granted and
  no other.
- **No database or filesystem, ever.** The permanent line from
  [m4-plugin-scope.md](../m4-plugin-scope.md): a plugin returns *data* (records,
  ratings) to the host, which owns all persistence. `store` never crosses the
  boundary. This is the same rule handlers already follow (CLAUDE.md), extended
  across the sandbox.

The runtime is validated by reimplementing **OMDb as a first-party `RatingSource`
plugin** ([ADR 0019](0019-external-ratings.md)) before the contract is made
public — it exercises exactly this model (one outbound host, one granted secret,
no DB, no filesystem) and proves the ADR 0007 promise end to end.

## Consequences

**Good — the single binary survives.** `wazero` is pure Go with no cgo, so the
host stays one static binary. Plugins are data files (`.wasm`) the host loads,
not executables it launches; distribution stays "one artifact per platform" for
the core.

**Good — isolation is a property of the mechanism, not the host's diligence.** A
WASM module has no capabilities it was not handed. With subprocesses, the same
guarantee is a pile of per-OS sandboxing (seccomp, job objects, sandbox-exec)
that the host must get right on every platform or the isolation is theatre. WASM
gives deny-by-default for free.

**Good — polyglot authoring.** Anything that compiles to WASM (Go, Rust, C, …)
can be a plugin, so the community is not fenced to one language.

**Good — the ADR 0007 promise is literal.** The host adapts a loaded module to
the existing `Provider`/`RatingSource` interfaces at the boundary; the merge
engine, enricher, and API see the same interfaces they see today.

**Cost — a hand-built ABI.** WASM passes numbers, not structs. The host↔module
marshalling (how a `Query` goes in and `[]Rating` comes out) is boilerplate we
own and version. This is the real tax, and it is why the first-party plugin comes
before the public contract: the ABI is designed against a real caller, not
guessed.

**Cost — a runtime dependency.** `wazero` is a genuine dependency, weighed
against the audit line. It is pure Go, single-purpose, and auditable — the
opposite of the ~300KB opaque browser library ADR 0013 refused — and it is the
*enabler* of the sandbox, not unaudited code running inside it. Accepted.

**Cost — authoring friction and debugging.** Writing a plugin against a
host-function ABI is harder than writing a plain Go program (the subprocess win),
and debugging across the WASM boundary is less pleasant than a stack trace. Real,
and the price of the isolation guarantee. Good tooling and a clean SDK for
first-party plugins mitigate it.

**Cost — no raw `net/http` inside the module.** Go compiled to WASM cannot open
sockets, so a plugin cannot just use `net/http`. This lands as host-mediated
egress — which is not a workaround but the point: the host controls and restricts
every outbound call.

## Alternatives considered

- **Subprocess RPC (go-plugin).** Rejected as the *default*. Its authoring
  simplicity is real and tempting, but it breaks the single-binary distribution
  story and makes isolation a per-OS host responsibility rather than a mechanism
  guarantee — trading the two things this project most wants to keep for author
  convenience. Held in reserve for a genuine exception (see below).
- **Native Go plugins (`plugin` package).** Rejected outright: no isolation at
  all (a native plugin *is* the host process), Linux/macOS only, and famously
  brittle across compiler and dependency versions. It fails every requirement
  this decision exists to meet.
- **Embedded scripting (Lua/JS/Starlark).** Rejected: picks a single language,
  and a general-purpose interpreter is a larger and less analysable sandbox than
  a WASM module with an explicit import table.

## Revisit if

- **A plugin needs capabilities WASM cannot carry performantly** — a heavy native
  codec, a large native dependency that will not compile to WASM. That specific
  plugin could be a **vetted, first-party, OS-sandboxed subprocess** as a
  deliberate exception — but the exception is decided on its own merits, not by
  making subprocesses the default and losing the guarantees above for everything.
- **The host-function ABI proves unworkable** against the first-party OMDb plugin.
  That is exactly what building the plugin *before* publishing the contract is
  meant to surface, and it would reopen this decision rather than paper over it.
