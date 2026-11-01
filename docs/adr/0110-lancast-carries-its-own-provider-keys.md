# ADR 0110 — LANcast carries its own TMDB and OpenSubtitles keys

Date: 2026-11-01 · Status: **accepted** · Amends the README's *Getting a TMDB key*

## Context

Metadata and subtitle search each needed a key the person had to fetch for
themselves: TMDB for matching and artwork, OpenSubtitles for subtitle search,
OMDb for outside ratings. For the person who wrote LANcast that is three
sign-ups done once. For a friend setting up their first server it is the
point where the library stays a wall of filenames, because nothing on screen
says why or how to fix it without leaving the app.

Two of the three are not, in fact, the person's to get:

- **TMDB** issues a key to an *application*. Its limits are per IP address,
  not per key, so a thousand servers sharing one key are a thousand separate
  allowances, not one exhausted one. Non-commercial use is free, with
  attribution.
- **OpenSubtitles** (the v1 REST API) also issues its key to an application.
  What it limits per *person* is downloads, and it does that through a user
  login, not through the key.

**OMDb is different.** Its free key is about a thousand requests a day, per
key. One key shared across every install would be spent within hours of the
first busy evening, and the tiers above it are paid. It stays bring-your-own.

## Decision

**Release builds carry a TMDB key and an OpenSubtitles key.** They are put
in at build time, by `-ldflags -X` from repository secrets
(`LANCAST_TMDB_KEY`, `LANCAST_OPENSUBTITLES_KEY`), into
`internal/config.BuiltinTMDBKey` and `BuiltinOpenSubtitlesKey`. They are
never committed. A `go build` from a checkout carries neither and behaves
exactly as LANcast always has, with no key until one is entered.

**The person's own key always wins.** `Settings.TMDBKeyInUse` and
`OpenSubtitlesKeyInUse` are the one place that choice is made: your key if
you set one, otherwise the built-in one, otherwise nothing. Every caller asks
those two methods. A second place deciding would eventually disagree with the
first.

**The built-in keys can be switched off.** `builtin_keys_off` in Settings.
With it on, a server with no key of its own fetches nothing from TMDB or
OpenSubtitles, as before. The README's *No phone-home* principle says the
server does no network things you did not ask for. Adding a library is
asking for its metadata, and it always was once a key had been entered; this
removes the sign-up, not the choice. The switch is what keeps "nothing leaves
this machine" possible without deleting anything.

**An OpenSubtitles login is optional.** A username and password in Settings,
stored write-only beside the keys, raise the daily download allowance from
the anonymous one to the account's. The server logs in when it first needs
to and keeps the token in memory for most of a day. Without a login, search
and download work on the allowance OpenSubtitles gives the key alone.

**Plugins never read the built-in keys.** `BuiltinSecret`, which hands a
granted plugin the server's `tmdb_key` or `opensubtitles_key`, still returns
only a key the person entered. The built-in key was issued to LANcast for
LANcast's own requests; a plugin spending it would make its traffic count as
LANcast's, and an abusive one could get the key revoked for every install.
A plugin that wants TMDB asks the person for a key, as it did before.

**TMDB is credited in Settings › About.** The terms ask for it, and it is
true: much of what the library shows came from there.

**OMDb points the way.** Its row in Settings links to omdbapi.com's key page
and says what a key adds, so getting one is a click rather than a search.

## Consequences

- A fresh install matches films and shows, and can search subtitles, with no
  sign-up at all.
- The keys are in every release binary and anybody can read them out. That
  is true of every client that ships a key, and both services expect it. It
  is why they go in at build time rather than into git: a key in a public
  repository is scraped within hours, and one in a binary can be replaced by
  shipping a release.
- If a built-in key is revoked, every install without its own key loses that
  provider until the next release. The person's own key is the way around it,
  and the Settings row says which key is in use, so the fix is visible.
- A server whose owner turned the built-in keys off behaves exactly as
  before this ADR, which is the test that the switch is real.
- A local or devseed build has no built-in keys. Testing the built-in path
  means a release build, or `-ldflags -X` by hand.
