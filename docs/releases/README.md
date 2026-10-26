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
