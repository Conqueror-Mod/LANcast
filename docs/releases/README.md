# Release notes, one file per release

Each release's notes live here as `v<version>.md` — the same text published on
the GitHub release, committed **before** the tag is pushed. The build embeds
this folder (`about.go` at the repository root), and Settings › About shows the
running version's notes, so the app always describes itself without asking
anybody's server.

A version with no file here shows no notes in About. Nothing breaks; the pane
says there are none.

Bump the status line at the top of the repository README to the same version
in the same commit. `internal/projectdocs/readme_test.go` fails until it
matches the newest file here, because a stale version is the first thing a
stranger reads.

## Release candidates are swap kits, never `-rc` tags

A 1.0 candidate (or any candidate) is tested as a test-build swap kit dropped
into an installed service, not published under a tag such as `v1.0.0-rc1`.
`parseVersion` in `internal/update/check.go` rejects any suffix on purpose, so
a stable install is never offered a candidate. The same rule cuts the other
way: a build stamped `1.0.0-rc1` cannot compare its own version, so it would
never be offered 1.0.0 and would sit on the candidate until reinstalled by
hand. `TestReleaseCandidatesAreNeverOffered` pins that behaviour, and
`TestTheStepToOnePointZero` pins that every 0.10.x and 0.11.x install is
offered 1.0.0.
