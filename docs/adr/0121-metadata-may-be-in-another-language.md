# ADR 0121 — Metadata may be in another language than the interface

Date: 2026-11-08 · Status: **accepted** 2026-11-08 · Related: [ADR 0019](0019-external-ratings.md), [ADR 0110](0110-lancast-carries-its-own-provider-keys.md), [ADR 0120](0120-a-setting-belongs-to-what-it-describes.md), [docs/settings-3-plan.md](../settings-3-plan.md)

## Context

TMDB is asked for one language, `en-US`, fixed in
`internal/meta/tmdb/tmdb.go`. The comment beside it gave the reason: LANcast's
interface is English, and a metadata language that disagreed with the
interface "would be a stranger result than either on its own". It added that
when there was a UI language there should be a setting, and that this constant
was the one place it would go.

That reasoning ties two things together that people do not tie together. A
film's title and synopsis are *content*, like its audio track and its
subtitles. Somebody who reads German summaries comfortably does not need the
Settings screen in German to want them, any more than they need it in German
to want German subtitles, which LANcast already lets them choose. Settings 2.0
listed metadata language as its last unbuilt P1 item, and the Settings 3.0
plan (§7, decision 5) chose to build it with an ADR saying why the constant
can go.

Three things made the constant safer than it looked, and a setting has to keep
them:

- **Matching.** Search results are scored against the filename. With English
  filenames, an `en-US` search returns English titles and the scores are
  right. Asked in German, TMDB returns "Der Pate" for a file called
  `The Godfather (1972).mkv`, and a title score near zero.
- **The response cache.** Responses are cached by path and parameters, and the
  language was added to the request after the cache key was made. With one
  language that never mattered; with two, a cached English answer would be
  served for a German question for the cache's lifetime.
- **Gaps.** TMDB's translations are uneven. A film with no German synopsis
  comes back with an empty overview in German, where `en-US` had one.

## Decision

**The metadata language is a server setting, `metadata_language`, defaulting to
`en-US`.** It is a language tag TMDB accepts (`de-DE`, `pt-BR`, `ja-JP`),
chosen from a list the server serves, the same way certification countries
are. It applies to every library. A per-library language, for a "Films (FR)"
library beside an English one, is the media's own setting under ADR 0120 and
comes with per-library options (settings-3-plan step 6).

Not per account. Metadata is stored once per title and every account reads
that one copy; a per-person language would need a copy per language, which is
a different data model for a preference most households share.

**What changes with it, so that the three safeguards above survive:**

1. **Every TMDB request carries the language, and the cache key includes it.**
   The language is set before the key is made, so an answer in one language is
   never served for another.
2. **English fills the gaps.** When the chosen language is not English and a
   title, tagline or synopsis comes back empty, the same record is asked for in
   `en-US` and the empty fields are taken from it. One extra request, cached,
   and only for records with a gap.
3. **Matching scores every name a candidate has.** A search returns the title
   in the chosen language, and the scorer also tries the original-language
   title and, when the chosen language is not English, the English title from
   an `en-US` search of the same query. The best of them is the title score.
   An English filename and a German one both match.
4. **Trailers are not limited to the chosen language.** `/videos` filters by
   language, so a German request returns only German trailers, which many
   films have none of. The request asks for the chosen language, then English,
   then untagged.

**A refresh in the new language is offered, never run by itself.** Changing
the setting changes what is fetched from then on. Titles already stored keep
their text until somebody asks for them again. The Metadata pane says this and
offers it: a count of the titles that would be fetched again, and a button.

That refresh is a new scope, `language`, beside `all`, `unmatched` and
`settled`. Like `settled`, it fetches each title from **its own recorded TMDB
id**: nothing is searched, nothing is scored, no match state moves, and the
result is applied under the locks the row already carries. A locked field
stays as it was; only the text the provider owns changes. Unlike `settled`,
it covers every TMDB-identified title, matched or locked, because a change of
language is news about all of them.

**Region is not a second setting.** What a region chooses in LANcast is whose
age certificate to show, and that is already the server's certification
country. TMDB's `region` parameter only filters release dates in search, which
the scorer does not use. The Metadata pane puts the two side by side.

## Consequences

- An English-language server behaves exactly as before: `en-US` is the
  default, the gap fill never runs, the extra search never runs, and the
  scorer's extra names are only the original titles, which can only raise a
  score.
- A non-English server costs more on a first match: two searches instead of
  one, and a second detail request for records with a gap. Both are cached.
- With `write_nfo` on, NFO sidecars are written in the chosen language, like
  everything else that is stored.
- A refresh of a large library in a new language is slow, at five requests a
  second. The count shown beforehand makes that a decision, as it does for
  every other refresh.
- The interface stays English. If it ever gains translations, this setting is
  not its language setting: the two answer different questions.
