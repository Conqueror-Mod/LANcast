# ADR 0027 — Depth in the canonical look

**Status:** accepted
**Date:** 2026-08-09

## Context

The roadmap's feature backlog asks for a "more branded, thematic home page —
beyond functional shelves." The reference points are the streaming services:
Netflix, Prime, Hulu all open on a single large piece of artwork with the title,
a short synopsis and a play control over it, and shelves beneath.

LANcast's home page was shelves and nothing else. It was correct and it was
flat: every element sat on exactly one plane, so the screen read as a list of
rows rather than as a place. The brief for this change was explicitly visual —
"more 3D in nature, shadows, depth and rich colors", and "polished, not
developed".

That brief pushes on [ADR 0003](0003-one-canonical-theme.md) and on
[design.md](../design.md), which commit to one look built from a deep-space
field with **gold as the only accent, earned and never ambient**. A hero built
the obvious way — a glowing accent, a second colour for emphasis, a lit play
button — would break the one rule the design system says is load-bearing. The
gold ring *is* the focus indicator, so anything that dilutes gold degrades an
accessibility affordance, not just an aesthetic one.

## Decision

**Depth is added to the canonical look as a vocabulary of its own. It is built
from elevation, layering and parallax — never from colour.**

Concretely, four rules:

1. **Shadows are cast in the void colour.** Every elevation step is
   `rgba(5,7,15,α)` — the page floor — so a raised object reads as further from
   the same field rather than as a grey object sitting on a blue one. Three
   steps are defined as `--elev-1/2/3`, each a tight contact shadow plus a wide
   ambient one.

2. **Gold is not an elevation cue.** Nothing glows. Gold remains the focus
   signal exclusively: hairline at rest, full strength with its ring on
   hover/focus. Where a focused object also lifts, the lift is *additional* to
   the ring, never a replacement — a lift alone is not an accessible indicator.

3. **Artwork is tinted into the field, not laid on top of it.** A hero backdrop
   is screened with the existing nebula violet and blue at low alpha and
   vignetted at the edges. Without this the hero is a photograph with an app
   around it, and the deep-space identity stops at the hero's edge. The tint
   introduces no new colour: it is `--nebula-violet` and `--nebula-blue`, the
   same two the field is already made of.

4. **Depth that moves is opt-out.** The hero backdrop parallaxes at 0.28× scroll
   and tiles lift on focus; both are removed entirely under
   `prefers-reduced-motion`, while the gold ring is untouched by that query.

## Consequences

**The rule that was at risk is intact.** Nothing in this change adds a colour,
and gold still means one thing. The hero's Resume button is gold-ringed on focus
by the same mechanism as every poster tile, so the TV client inherits it.

**Depth is now a token, not a per-screen decision.** `--elev-1/2/3` live beside
the colours in `tokens.css`. A future screen that needs a raised surface uses the
same three steps, which is the difference between a system and one nice page.

**Poster tiles changed everywhere, not only on Home.** They gain a resting
`--elev-1` and a slightly stronger lift on focus, so the Browse grid picks the
change up too. That is intended — a design system that applies to one screen is
a stylesheet.

**The hero needs artwork and will not fake it.** It renders only when the chosen
item has fanart, and picks the first candidate that *has* fanart rather than the
first candidate. A hero built around a missing backdrop is a grey box with a
title in it, which is the look this change exists to escape. On a library with
no fanart at all, Home is exactly what it was before.

**Rejected: a second accent colour for the hero.** It was the obvious way to get
"rich colour" and it was declined. Two accents means the eye has to learn which
one means focus, and the first time gold appears next to it in the same scope,
the focus signal is gone. Richness here comes from the artwork itself plus the
nebula tint, which costs nothing from the accent budget.

**Rejected: a rotating spotlight.** Auto-advancing heroes are the streaming-
service convention, and they are also the part of that convention people
complain about. Resume-first is more useful and has no timer to fight with a
reduced-motion preference.

**Rejected: a server-picked featured item.** It is the truest reading of
"thematic home page" and it is an API contract change. When curation is wanted,
it should arrive as its own decision with its own endpoint, not smuggled in
behind a visual change.
