# ADR 0039 — Organising a large channel list

Date: 2026-08-17 · Status: proposed

## Context

Live TV was designed against a channel list and tested against one. A real
server now has **1,862 channels from a single provider**, and a second playlist
under evaluation beside it. Both appear on one page, merged, with no way to ask
for one and not the other.

The page is not broken. It is *unusable at this size*, which is a different
failure and a harder one to see from the code: every element works, the counts
are right, search works, and the whole thing is still a wall.

Measured on the running app:

```
1,862 channel tiles rendered at once
11,388 DOM nodes
   ~60 group chips, wrapping to five rows before a single channel is visible
```

The group chips — the organising idea the page was built around
([ADR 0036](0036-epg.md)) — were the right answer for six hundred channels from
one source. At this size they are themselves the obstruction: the filter row is
taller than the content it filters.

Two facts shape everything below.

**A channel already knows which playlist it came from.** `channel.source_id` is
recorded at import, because refreshing a source replaces its channels and
nothing else. The grouping the user wants most is one the database already has
and the API does not expose.

**The guide is empty and will stay empty for now.** Listings attach on `tvg-id`
alone, and no provider in use publishes XMLTV. Any design that leans on
programme data solves nothing today.

## Decision

Four changes, in the order their value divided by their cost puts them.

### 1. Filter by playlist — `GET /api/channels?source_id=`

The smallest change with the largest effect, and the only one here that is a
contract change.

`/api/channels` grows an optional `source_id` filter, and the response for each
channel carries its `source_id` and the source's name. The Live TV page gets a
source selector *above* the group chips: one level up, the same pattern, no new
interaction to learn.

With one playlist the selector is absent — a control that offers a single choice
is furniture. It appears when a second source exists, which is exactly when the
question "which list is this from" starts being asked.

This is proposed first because it is the question actually being asked out loud:
*"everything from each playlist appears on the same screen."*

### 2. Groups that open rather than filter

The chip row becomes collapsible sections: a group is a heading and a count, and
its channels appear when it is opened. The first source's first group opens by
default so the page is never empty.

Filtering answers "show me only sport". Sections answer "what is in here" — which
is the question a list of sixty groups from an unfamiliar provider actually
raises, and it puts channels above the fold instead of below five rows of chips.

### 3. Hidden and favourite channels, per device

Two providers overlap heavily: the same channel twice at two qualities, plus
hundreds of countries nobody in this house will watch.

- **Hide** removes a channel from browsing without deleting anything. A hidden
  channel is still playable by URL and still counted in Settings, because
  hiding is a view preference and not a claim that the channel is gone.
- **Favourite** pins a channel to the top of the page.

Per device, in local storage, alongside the other device preferences
([ADR 0034](0034-multi-root-libraries.md) draws the same line for libraries):
which channels matter is a fact about the room the screen is in. The television
in the lounge and the phone in a pocket do not want the same twenty channels,
and syncing them would make one wrong to fix the other.

This is what turns 1,862 channels into a usable page day to day, rather than a
navigable one in principle.

### 4. A guide-first view is deferred, explicitly

The obvious "make it like a television" answer is a grid of channels against
time. It is deferred, and the reason is not effort.

ADR 0036 already refused the grid for the schedule strip, on the grounds that it
needs horizontal scrolling and a time ruler and makes a three-minute bulletin
unreadable. That reasoning has not changed. And a guide-first *page* is worth
nothing while the guide is empty: it would render 1,862 rows of "no listings",
which is worse than the wall it replaced.

Revisit when a provider in use publishes XMLTV. Not before.

## Consequences

- **`/api/channels` gains a filter and two response fields.** A contract change,
  so `docs/api.md` moves with it. The filter is optional and the fields are
  additive, so an existing client keeps working.
- **Rendering stays as it is.** Virtualising 1,862 tiles is the reflex fix and it
  is aimed at the wrong problem: images are already lazy, the page is not
  visibly slow, and nobody is asking to scroll 1,862 tiles faster — they are
  asking not to be shown 1,862 tiles. Sections and filters remove the nodes that
  virtualisation would merely render cheaply. If the DOM count becomes a
  measured problem after that, it can be solved then, against a measurement.
- **Hidden channels create a place for something to disappear into.** Every
  count in Settings therefore stays a count of what exists, not of what this
  device shows, and the page says how many are hidden with a way back. A filter
  that cannot be seen or undone is indistinguishable from a bug.
- **Nothing here depends on the EPG**, which is deliberate. Live TV should be
  usable with a channel list alone, because that is what providers actually
  supply.
- The source selector is the first UI in the app whose *presence* depends on
  data — one playlist, no selector. That is a small precedent worth naming: it
  is a control that would otherwise teach a lie, since a filter offering one
  option implies others exist.
