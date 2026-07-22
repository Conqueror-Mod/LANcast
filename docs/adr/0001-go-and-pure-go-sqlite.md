# ADR 0001 — Go with a pure-Go SQLite driver

Date: 2026-07-22 · Status: accepted

## Context

LANcast has to run unattended for years on hardware nobody wants to think about:
a Windows desktop, a Linux box, a Synology NAS, a Raspberry Pi. The install
experience is a real feature — self-hosted software dies from setup friction far
more often than from missing capabilities.

Candidates considered: Go, Rust, TypeScript/Node, Python.

The database question is not separable from the language question. The obvious
SQLite driver for Go, `mattn/go-sqlite3`, requires cgo, which means every build
needs a working C toolchain. On Windows that means MinGW or MSVC, and
cross-compiling to ARM for a Pi becomes an afternoon instead of a command.

## Decision

**Go**, with **SQLite in WAL mode** via **`modernc.org/sqlite`** — a pure-Go
translation of SQLite requiring no cgo.

## Consequences

**Good.** One static binary per platform, no runtime, no interpreter, no
dependency install for the user. Cross-compilation is `GOOS=linux GOARCH=arm64
go build`. Go's concurrency model fits the actual workload — many simultaneous
range-request streams plus a background filesystem walk — without async
plumbing. The database is a single file, which makes backup an ordinary file
copy.

**Cost.** `modernc.org/sqlite` is meaningfully slower than the C original on
write-heavy workloads. This is an acceptable trade: LANcast's write pattern is
bursty scans, not sustained transactional load, and reads dominate everything a
user actually experiences. If a scan on a very large library ever proves too
slow, the fix is batching inside a transaction, not swapping drivers.

**Cost.** Go is more verbose than Python or TypeScript. Accepted for the
deployment story.

**Cost.** Rust would give better raw performance and a stronger WASM plugin
story for M4. Rejected because iteration speed matters more for a project that
has to survive its own first year, and because Go's WASM host options
(`wazero`, also pure Go) are adequate.

## Revisit if

Scans on a 40k-item library become unacceptably slow after batching, or the M4
plugin sandbox needs guarantees `wazero` cannot provide.
