# ADR 0005 — Theme music: identification separated from playback

Date: 2026-07-22 · Status: accepted

## Context

Opening a detail page should play that title's theme. Kodi's TvTunes did this
and people still miss it; it is the difference between a detail page that
*displays* an item and one that feels like arriving somewhere.

The sourcing problem is asymmetric, and the asymmetry is legal rather than
technical:

- **TV themes have a real, established source.** Both Kodi's TvTunes and Plex
  resolve TV themes from a TVDB-keyed theme endpoint. This is well-trodden.
- **Film score audio has no legal automated source.** No API streams film score
  audio on demand. MusicBrainz and TheAudioDB provide *identification* —
  track listings, composers, release metadata — but not audio, and no amount of
  engineering changes that.

The naive options were both bad. Skipping films entirely gives up the feature on
half the library. Extracting the first 90 seconds of the film with ffmpeg always
"works" but produces dialogue, studio idents, or a cold open — unpredictable in
a way that reads as broken.

## Decision

**Separate identification from playback.** They are different capabilities and
they fail independently.

- **Identification** always runs, for both films and shows. LANcast resolves the
  real main-title cue from the registered OST and displays it as metadata —
  "Main title — Jóhann Jóhannsson, *Arrival* OST".
- **Playback** resolves in strict order: local file → cached fetched theme →
  identified-but-silent → nothing.

Local file convention, checked first in all cases and always authoritative:

```
theme.mp3 | theme.m4a | theme.ogg    beside the movie file, or in the show root
```

Network theme lookup is **opt-in**, consistent with the no-phone-home principle.
Declining it does not break the feature; it limits it to local files.

## Consequences

**Good.** The film detail page reads as researched even when it is silent. The
correct cue and composer are named — the page has *knowledge* about the film,
which is most of the emotional payload the feature was after. Displaying the
right answer and honestly not playing it is far better than playing the wrong
thing.

**Good.** `available: false` with full identification metadata is a normal,
expected state rather than an error. The API documents it as such, and clients
must render it as silence, never as a failure. **Absent theme audio is never an
error message.**

**Good.** Users who own a soundtrack get full behavior by dropping one file next
to the movie. Local files always win, so a user's own choice cannot be
overridden by a provider.

**Good.** No legally dubious audio sourcing anywhere in the project.

**Cost.** Films are silent by default. Accepted — the identification metadata
carries the feature, and the alternative was extracting unpredictable audio from
the film itself.

**Cost.** Two code paths, TV and film. They genuinely are two problems.

**Cost.** Depends on M2 metadata for TVDB ids and OST lookup, so the feature
cannot ship earlier than M3.5.

## Related

Playback behavior — the 800ms delay, fade in and out, mute persistence, and the
autoplay-policy degradation rule — is specified in `docs/design.md`, not here.
