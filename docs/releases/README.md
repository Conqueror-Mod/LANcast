# Release notes, one file per release

Each release's notes live here as `v<version>.md` — the same text published on
the GitHub release, committed **before** the tag is pushed. The build embeds
this folder (`about.go` at the repository root), and Settings › About shows the
running version's notes, so the app always describes itself without asking
anybody's server.

A version with no file here shows no notes in About. Nothing breaks; the pane
says there are none.
