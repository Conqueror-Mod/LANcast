# ADR 0094 — Containment follows links

Date: 2026-10-26 · Status: **accepted**

## Context

CLAUDE.md makes one check non-negotiable: every handler that turns a database
row into a filesystem path re-verifies that the path is inside the library
location the row came from. That check was `filepath.Abs` then `filepath.Rel`,
which is about the *string*. An outside review pointed out, and it was
confirmed on 2026-10-25, that it never looked at the filesystem:

- the scanner's `filepath.WalkDir` callback imported a symlinked file by its
  extension like any other, so `Films/film.mkv` linking to any file on the
  machine became an item;
- `containedPath` found `Films/film.mkv` inside `Films`, which as a string it
  is;
- the stream handler opened it, which follows the link, and served it from a
  service running as LocalSystem.

Exploiting it needs write access to a library folder and the right to make
symlinks, so it is hardening rather than an open door. But the check exists
for exactly the case where the database, or the folder it was built from, is
not what it should be.

Surveying every containment helper turned up two more gaps of the same kind:
external subtitles went from their row to a file read with **no** check, and a
lyrics sidecar was checked by name only.

## Decision

**1. Containment is decided on disk.** `internal/contain` resolves both the
root and the target, following every link, and requires the target to be
inside the root both by name and once resolved. The path handed back is the
lexical one — the one that was checked, and the one a delete should unlink.
`containedPath` in `internal/api` is now a call to `contain.Within`.

**2. Resolution walks the path itself rather than calling
`filepath.EvalSymlinks`.** Measured on Windows with Go 1.26: `EvalSymlinks`
no longer follows junctions (Go 1.23) and fails with "path not found" for
every path *beneath* one. A library under a junctioned folder is an ordinary
Windows setup; with `EvalSymlinks`, a root reached through a junction stayed
unresolved while an in-library link whose target named the real folder
resolved past it — so a link to the library's own file read as outside. The
walk follows symlinks and junctions alike, on both sides.

**3. Not-yet-existing files and dangling links differ.** A file that does not
exist (an optimized copy about to be written, a sleeping drive) is judged by
its nearest existing folder. A link to nothing is refused: creating a file
through it creates one wherever it points.

**4. Removing a file judges the folder, not the link.** `os.Remove` removes a
link, not its target, so a delete checks the containing folder resolved and
the final name as written (`contain.EntryWithin`). A stray link out of the
library can still be deleted; a linked *folder* out of it cannot be deleted
through.

**5. The scanner skips links that resolve outside the location being walked**,
and links to nothing, and names both in the scan's issues. A link to a file
elsewhere in the same location is imported, with the file's size and time
rather than the link's. A link to a folder is skipped quietly, as before —
WalkDir does not follow them. This is not the boundary (the API re-checks
regardless); it keeps a link the boundary would refuse from becoming a tile
that cannot play, and keeps the server from probing, fingerprinting and
reading tags from a file outside the library. An item an outside link already
imported is marked missing by the next scan, never deleted.

**6. Lexical checks stay where links cross no privilege boundary.** The games
helpers (`games.contained`, `withinDir`, `IconTarget`, launch's executable
check) run in the desktop app as the signed-in user, so a link reaches nothing
they could not already open, and game folders moved behind a link or junction
are common. `store.within` decides whether two configured locations overlap,
which is configuration sanity rather than access. Both are noted in the code.

## Consequences

- An in-library link to elsewhere in the library still plays. An out-of-library
  link answers 404, like any other containment failure, and the server logs
  `stream containment check failed`.
- Not covered: the gap between the check and the open. A link swapped in
  between is a race against somebody who can already write the library folder;
  closing it needs `openat`-style walking that `os` does not offer on Windows.
- Not covered: hard links, which are indistinguishable from the file. Making
  one on Windows needs write access to the target already.
- A library root that is itself a symlink or a junction still scans nothing,
  as before this change: `WalkDir` does not descend into a link given as its
  root. Containment handles such a root correctly; the walk is a separate,
  pre-existing limitation.
- Tests: `internal/contain` (unit, plus a Windows-only junction test),
  `internal/api/symlink_containment_test.go` (stream and subtitle, out and in)
  and `internal/scan/symlink_test.go`. Each skips cleanly where `os.Symlink`
  fails; Linux CI always runs them.
