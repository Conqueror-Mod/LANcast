# ADR 0024 — Music libraries

Date: 2026-08-02 · Status: accepted

## Context

The roadmap has carried "library types beyond video" since the beginning, with
the note that it *proves the taxonomy is open*. [ADR 0002](0002-one-wide-media-item-table.md)
chose one wide `media_item` table with a `kind` discriminator specifically so a
new media type would not need a new table, and [ADR 0010](0010-shows-as-media-items.md)
established that containers and their children are rows in that same table
related by `parent_id`. Music is the first real test of both claims.

The half-state today is worth naming: **`kind: "music"` is already accepted as a
library kind** by `POST /api/libraries`, and the settings UI offers it. Nothing
downstream knows what to do with it. The scanner admits a file only if
`media.IsVideo` says so, and `videoExts` contains no audio extensions, so
creating a music library produces a library that scans successfully and stays
permanently empty. That is a promise the API makes and does not keep.

Music differs from video in one way that shapes everything else. For video, the
**filename is a guess** — `internal/media` extracts a probable title and year,
and a provider corrects it ([ADR 0007](0007-provider-and-localsource-split.md)).
For music, the **file already carries the answer**: ID3v2 on MP3, Vorbis
comments on FLAC and Ogg, MP4 atoms on M4A. Artist, album, title, track number
and date are embedded, near-universally, by every tagger and every store.
Treating a music filename as the primary signal and a remote database as the
corrector would be inverting the reliable and the unreliable.

And the tags are already in reach: `ffprobe` reports them as `format.tags`, and
LANcast already runs ffprobe on every file and parses `format`. Reading them is
a struct field, not a dependency.

## Decision

**Music libraries, built on the existing taxonomy, with embedded tags as the
metadata source.**

### Taxonomy: artist → album → track

Three new `kind` values on `media_item`, related by `parent_id` exactly as
show → season → episode already are:

- `artist` — a container, no file.
- `album` — a container, no file, child of an artist.
- `track` — a row with a path, child of an album.

This is the shape ADR 0010 already built and ADR 0017 extended. The browse grid's
top-level rule (`parent_id IS NULL`) shows artists; opening one shows albums;
opening an album shows tracks. Play-all queues, watch state, artwork and locking
work on these rows because they work on `media_item`.

**Grouping is by album artist, falling back to artist.** A compilation has a
different `artist` per track and one `albumartist` for the record; grouping on
`artist` would shatter it into one album per performer. Where neither tag
exists, the containing folder name is the album and its parent folder is the
artist.

### Metadata: the file first

Embedded tags are read from `format.tags` in the existing probe output and
registered as a `LocalSource` alongside NFO ([ADR 0007](0007-provider-and-localsource-split.md)).
That places them in the merge engine as an authoritative local input, which is
what they are, and inherits field-level locking ([ADR 0008](0008-field-level-locking.md))
without new machinery.

Order of authority for a track, strongest first: **locked fields, embedded tags,
folder structure, filename.** No remote provider in this ADR — see below.

`internal/media` keeps its rule that filename guessing lives only there
([CLAUDE.md](../../CLAUDE.md)); music adds a `ParseTrack` beside the existing
video parsing for the fallback case. Tags are not filename guessing and do not
belong in that package — they are a source, and they live with the other
sources.

### File types

Scanned as music: `.mp3`, `.flac`, `.m4a`, `.aac`, `.ogg`, `.oga`, `.opus`,
`.wav`, `.aiff`, `.wma`, `.alac`.

`media.IsVideo` becomes kind-aware rather than gaining audio extensions. A movie
library must not absorb the MP3s in a soundtrack folder, and a music library
must not index the MKV a band shipped with an album. The scanner asks what the
library is for.

### Playback: audio containers are first class

`probe.Profile` today lists `mp4`, `webm`, `mov` as containers, so a `.flac`
file fails the container check, is judged a remux to MP4, and — because
fragmented MP4 cannot reliably carry FLAC ([v0.4.0](../roadmap.md)) — becomes a
needless transcode of a file every modern browser plays natively.

Profiles gain audio containers, and the decision engine learns that a file with
no video stream is judged on its audio alone:

| Codec | Browser | Decision |
|---|---|---|
| MP3, AAC, FLAC, Opus, Vorbis, WAV | plays natively | direct play |
| ALAC | Safari only | direct play on `safari`, transcode elsewhere |
| WMA, APE, AIFF | no | transcode to AAC |

The existing `Decide` already treats a nil video stream as compatible, so this
is mostly extending the profile data rather than adding a branch.

### Artwork

Embedded cover art arrives as an attached picture, which ffprobe reports as a
video stream — and `probe.isCoverArt` already recognises and skips exactly that
so an audio file is not mistaken for a film. The same detection becomes the
artwork source, with `cover.jpg` / `folder.jpg` beside the album as the
fallback, feeding the existing content-addressed cache.

### Not in this ADR

- **MusicBrainz or any remote music provider.** Tags cover the overwhelming
  majority of a personal library, and the ordering principle says to plan an
  area immediately before building it. When untagged rips prove to be a real
  problem, that ADR gets written then, and the `Provider` interface is already
  the place it plugs in.
- **Playlists, lyrics, ReplayGain, gapless playback, crossfade.**
- **Music videos and audiobooks.** Different taxonomies wearing similar file
  extensions; each deserves its own decision.
- **A music player UI beyond the minimum.** Album view and a track list that
  plays. The 10-foot and shelf treatments are roadmap items in their own right.

## Consequences

**Good — the taxonomy claim gets tested rather than asserted.** ADR 0002 and
ADR 0010 both promised a new media type would not need new tables. If music fits
on `media_item` with three new kinds and one grouping rule, that promise holds in
shipped code. If it does not, better to learn it here than at photos.

**Good — no new dependency.** Tags come from a probe that already runs, artwork
from a stream already detected, containers from a profile already data-driven.
ADR 0001's pure-Go posture is untouched.

**Good — most music direct-plays.** The transcode engine exists for video codecs
browsers refuse; the common music formats are all natively playable, so the
expensive path is rarely reached.

**Cost — the browse grid gains a third container depth.** Artist → album → track
is one level deeper than movie and the same as show → season → episode, so the
UI work is a variation on a solved problem rather than a new one, but it is
still real work in the client.

**Cost — a scan that finds nothing changes shape.** Today an empty music library
is a silent bug. Once music scans, a folder of untagged rips produces artists
named after directories, which is *correct* and will still look wrong to
someone expecting names. The scan diagnostics that already report skipped files
should say when a track had no tags, or the failure is invisible — the mistake
this project keeps making.

**Cost — `IsVideo` changes signature and every caller.** Four call sites, one of
which is subtitle discovery, which must keep meaning *video* specifically.

## The thing that is easy to get wrong

**Letting a music library be judged by video rules.** The two places this bites:
a `.flac` transcoded because the profile only knew MP4 containers, and an album's
cover art parsed as the video stream of a film. Both are one-line consequences of
code that assumes every item has moving pictures, and both are already latent in
the current engine — the cover-art guard exists precisely because that assumption
was made once already.

The second trap is **grouping on the wrong tag**. `artist` is per-track;
`albumartist` is per-record. A compilation grouped by `artist` explodes into
dozens of one-track albums, and it will look like a scanner bug rather than a
tag-precedence choice.

## Revisit if

- **Untagged libraries turn out to be common**, which makes a remote provider
  and acoustic fingerprinting worth an ADR rather than a deferral.
- **The wide-table taxonomy strains**, e.g. if photos need per-kind columns that
  music did not — that is the signal ADR 0002 said to watch for.
- **Gapless or bit-perfect playback becomes a goal.** Both push against a
  browser-based player and would strengthen the case in
  [ADR 0023](0023-native-desktop-client.md).
