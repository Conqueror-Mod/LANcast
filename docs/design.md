# Design system

> **Status:** direction settled, implementation pending. This is the reference
> when building any UI component. If you need an answer this document does not
> give, the document is incomplete — fix it here rather than deciding locally.

## Identity

Every self-hosted media server converges on the same dark grey rectangle, and it
is the main reason they read as utilities rather than as places worth spending
time. LANcast commits to something specific instead:

**A deep-space field — gradient blues with wispy, desaturated violets — with
minimalist gold as the only accent.** Star Trek's design sensibility (precision,
restraint, luminous instrumentation against darkness) without cosplaying LCARS.

Artwork is the content; chrome recedes. The room lights dim around the media.

## One canonical look

There is no skin system, no user themes, no alternate palettes. See
[ADR 0003](adr/0003-one-canonical-theme.md) for the full reasoning; the short
version is that every theming system taxes every component forever, and the
weakest supported theme sets the ceiling on how confident any visual choice can
be. One look lets the design be *specific*.

Customizability is kept — it lives at the **functional** layer (libraries,
scrapers, plugins, sort, filter, density), not the cosmetic one.

## The gold discipline rule

**Gold is earned, not ambient.** This is the most important constraint in the
system and the easiest to violate by accident.

| State | Treatment |
|---|---|
| At rest | Gold hairline, ~20% opacity. Structural, barely noticed. |
| Hover / focus / selected | Full-strength gold + soft outer ring at ~13%. |
| Playing | `--gold-bright`. |

**Gold means *where you are*, and nothing else.** Never use it for favorite,
new, unwatched, 4K, or any other state. The moment gold means two things the
focus signal is dead, and the whole system collapses into decoration. States
that need attention use type weight, opacity, or a small neutral badge.

Never: gold fills, gold text blocks, or gold on two elements at once inside a
single focus scope.

This rule is also why the eventual TV client is cheap — a focus indicator this
legible already works from ten feet away.

## Tokens

Defined once in `web/src/styles/tokens.css`. These exist for internal
consistency, not for user theming.

```
--space-void:      #05070f    page floor
--space-deep:      #070b18    base canvas
--space-raised:    #0a0f22    elevated surface

--nebula-blue:     #2658a8    field gradient, primary
--nebula-violet:   #5a4a9e    field gradient, wisp
--nebula-indigo:   #4a368c    field gradient, depth

--gold:            #c6a35a    canonical accent
--gold-bright:     #e3c179    active / playing
--gold-dormant:    rgba(198,163,90,0.20)    resting borders
--gold-ring:       rgba(198,163,90,0.13)    focus halo

--text-primary:    #f2f5fb
--text-secondary:  #c3cbe2
--text-muted:      #7d88a8
--text-faint:      #6f7a99

--radius-tile:     6px
--radius-panel:    12px

--elev-1           resting tile / control
--elev-2           hover, focus, raised panel
--elev-3           the hero poster, and nothing else so far

--dur-fast:        120ms      hover / focus
--dur-base:        280ms      page and panel transitions
--dur-ambient:     75s        nebula drift cycle
```

## The nebula field

Three `radial-gradient` ellipses over a `linear-gradient` base — cheap,
resolution-independent, zero image assets.

```css
background-color: var(--space-deep);
background-image:
  radial-gradient(ellipse 90% 60% at 15% -10%, rgba(90,74,158,0.38), transparent 60%),
  radial-gradient(ellipse 70% 50% at 85% 20%,  rgba(38,88,168,0.35), transparent 62%),
  radial-gradient(ellipse 120% 80% at 50% 120%, rgba(74,54,140,0.30), transparent 65%),
  linear-gradient(170deg, var(--space-raised) 0%, var(--space-deep) 55%, var(--space-void) 100%);
```

**Drift.** A `transform: translate3d()` animation on the gradient layer over
`--dur-ambient`, GPU-composited. It must be imperceptible when watched directly
and noticeable only if you look away and back. If you can see it moving, it is
too fast.

```css
@media (prefers-reduced-motion: reduce) { /* no drift, at all */ }
```

## Depth

[ADR 0027](adr/0027-depth-in-the-canonical-look.md). The field is deep, so the
objects on it should be too — but depth is built from elevation and layering,
**never from colour**.

**Shadows are cast in the void colour.** Every step is `rgba(5,7,15,α)`, the
page floor, so a raised object reads as further from the same field rather than
as a grey object on a blue one. Two layers per step: a tight contact shadow that
anchors the edge, and a wide ambient one that gives the drop.

**Gold is not an elevation cue.** Nothing glows, ever. Where a focused object
also lifts, the lift is additional to the gold ring and never a substitute — a
lift alone is not an accessible focus indicator, which is why the reduced-motion
query removes the lift and leaves the ring exactly as it is.

**Artwork is tinted into the field, not laid on it.** A full-bleed backdrop is
screened with `--nebula-violet` and `--nebula-blue` at low alpha and vignetted
at the edges, so it belongs to the same room as the chrome. Untinted, the
identity stops at the artwork's edge and the screen becomes a photograph with an
app around it.

**Depth that moves is opt-out.** Parallax and focus lifts are motion; they go
under `prefers-reduced-motion`.

## Typography

One geometric/humanist sans throughout. **Tracking is the Trek signal**, not
the typeface:

- Section labels — 11px, uppercase, `letter-spacing: 0.18em`, `--text-muted`
- Titles and body — sentence case, normal tracking, calm

Wide-tracked small caps for structure; quiet type for content. Never set a title
in wide tracking — that reads as a control panel, not a library.

## Screens

**Home.** A hero, then hub rows as horizontally scrolling shelves: continue
watching → recently added → per-library. Section headers pair a wide-tracked
label with a gold-to-transparent hairline rule trailing right. Continue-watching
tiles carry a 3px `--gold` progress bar on the tile's bottom edge.

The hero is the item you are part-way through, falling back to the newest
addition — resume is the likeliest reason someone opened LANcast, and a hero
advertising a film you are forty minutes into is worse than no hero. Full-bleed
fanart, tinted and vignetted into the field, parallaxing at 0.28× scroll behind
a floating poster at `--elev-3`. Title, meta line, three-line synopsis, progress,
then Resume and Details. **It renders only when the chosen item has fanart** —
and picks the first candidate that has fanart, not the first candidate. The item
in the hero is dropped from the shelf directly beneath it, because a home page
that shows you the same poster twice in 600px reads as generated rather than
arranged.

The deliberate fix for the main Plex complaint: library names in the top nav go
straight to the full grid. Hubs are a convenience, never a gate.

**Browse.** `repeat(auto-fill, minmax(160px, 1fr))` poster grid. Sort, filter,
and density controls persist. **Sort and filter state lives in the URL query
string** so any view is linkable and survives reload. Alpha-jump rail on the
right for large libraries.

**Detail.** Full-bleed. Item fanart fills the viewport at low opacity behind the
nebula field, scrimmed bottom-to-top for legibility. Poster left; title, year,
runtime, genre, and a gold-bordered play button right; synopsis, cast, then
season selector and episode list for shows. This is the page that plays theme
music.

**Player.** Chrome-free by default. Controls fade in on mouse movement, out
after 2.5s idle. Scrubber is a hairline that thickens on hover, filled `--gold`.
Everything else in the player is neutral — the video is the content.

**Settings.** Deliberately plain — this screen is a tool, and there are no
nebula theatrics on it.

Categories run down the left and one pane shows at a time, because settings are
looked up rather than read through and a single column got longer with every
release. The categories are grouped by *whose* setting it is rather than by
subject: **Server** (libraries, metadata, playback, users, add-ons, updates,
activity, logs) is shared and admin-only, **This device** (account, this app,
keyboard) affects nobody else. That distinction is the one that matters the
moment two people use the same server, and a flat list hides it completely.

The active category is marked the way every destination in this client is
marked: gold on the leading edge, and nothing else uses gold. A category is
offered only where its pane can act — "This app" is absent in a browser tab,
because the desktop settings behind it have nothing to say there.

## Keyboard model

Full keyboard navigation, built from the first component rather than bolted on.
The reason is structural: **an arrow-key spatial focus model is exactly what a
TV remote needs**, so building it now makes the TV client a restyle instead of a
rewrite. See [ADR 0004](adr/0004-keyboard-focus-model.md).

| Binding | Action |
|---|---|
| arrows | Spatial grid and shelf navigation |
| enter | Open focused item |
| escape | Back / close |
| `/` | Focus search |
| `ctrl+k` | Command palette |
| `space` `f` `m` | Play-pause, fullscreen, mute (player) |
| `←` `→` | Seek ∓10s (player) |
| `[` `]` | Cycle subtitle / audio track (player) |

One central roving-tabindex controller owns spatial resolution; components
declare themselves focusable rather than implementing their own key handling.

**Focus is never invisible.** The gold ring *is* the focus indicator — the
aesthetic affordance and the accessibility affordance are the same object, which
is why the gold rule cannot be relaxed for cosmetic reasons.

## Theme music

Behavioral spec; sourcing is in [ADR 0005](adr/0005-theme-music-sourcing.md).

- ~800ms delay before starting, so fast browsing stays silent
- 2s fade-in to ~35% volume
- 600ms fade-out on navigate away; immediate stop when playback starts
- one global mute toggle, persisted, visible in the detail-page header
- never plays twice for the same item in one session
- a settings preference for reduced audio, honored alongside reduced motion

Autoplay policy is satisfied because reaching a detail page always takes a click
or keypress. The exception is a cold deep-link straight to a detail page as the
session's first action — audio will be blocked there, and the UI must degrade
**silently**. Never show an error for absent theme audio.

## Technology

React + TypeScript + Vite. Plain CSS with custom properties — no Tailwind, no
CSS-in-JS. A single canonical look with hand-tuned gradients is better served by
real stylesheets, and it keeps the token file readable as the source of truth.

State: TanStack Query for server state, the URL for view state, a small context
for player and theme audio. No Redux.

Routes: `/` · `/library/{id}` · `/item/{id}` · `/watch/{id}` · `/settings`

## Verification

The honest tests, in order of how often they catch something:

1. **Gold calibration.** Put a real library on screen. If gold reads as
   decoration, dormant opacity is too high. If you cannot tell which tile is
   focused from three feet back, it is too low.
2. **Keyboard completeness.** Navigate the entire app without a mouse. Any dead
   end is a bug — and specifically a bug that will block the TV client.
3. **Motion restraint.** Confirm the drift is invisible under direct attention
   and fully stopped under `prefers-reduced-motion`.
4. **Contrast.** All text over the nebula field *and over fanart backdrops* must
   hold WCAG AA. Fanart is the risk case: a bright backdrop must not defeat the
   scrim.
