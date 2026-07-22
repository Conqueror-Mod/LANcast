# ADR 0003 — One canonical theme, no skin system

Date: 2026-07-22 · Status: accepted

## Context

This is the sharpest Kodi-versus-Plex tension in the whole project, and it sits
uncomfortably against the stated goal of getting multimedia freedom back.

Kodi's skin system let skins control layout, components, and navigation — not
merely color. It produced genuinely remarkable community work. It also meant
skins broke on nearly every release, that the core team could not change a
component without breaking third-party work, and that a large share of user-
visible bugs were really skin incompatibilities. Plex went the other way: one
look, minimal knobs, high polish, and no way to make it yours.

Options considered were a full skin system, a design-token layer (color, type,
spacing, density) with fixed layout, several curated theme sets, and a single
canonical look.

## Decision

**One canonical look.** No skins, no user themes, no alternate palettes. The
identity is fixed: deep-space gradient field in blues and desaturated violets,
with minimalist gold as the only accent.

Design tokens still exist in `web/src/styles/tokens.css`, but they are for
**internal consistency**, not a user-facing theming surface.

## Consequences

**Good.** The design can be *specific*. Exact gradient stops, an exact gold, a
focus treatment tuned per component. A theming system taxes every component
forever — each new element must be proven against N themes, and the weakest
supported theme sets the ceiling on how confident any visual choice can be.
Removing that constraint is what makes the aesthetic possible at all.

**Good.** The gold discipline rule (see `docs/design.md`) is only enforceable
under a single look. Under theming, "gold means where you are" becomes "the
accent color means where you are", and the moment a user picks a low-contrast
accent the focus indicator — which is also the accessibility affordance —
silently degrades.

**Good.** One look means one set of contrast obligations to verify, not N.

**Cost.** This is straightforwardly the Plex-side choice on the axis the project
was founded to push back on, and it will disappoint people who wanted Kodi's
skinning. That cost is accepted knowingly.

**Mitigation.** The customizability promise is kept, relocated to the
**functional** layer where it actually determines what users can do: libraries,
scrapers, metadata providers, plugins, sort, filter, and density. LANcast's
freedom claim is about control over your library and your data, not about
recolorable chrome — and the M4 plugin system is where that claim gets paid off.

## Revisit if

The project goes public and skinning becomes the dominant request. If so, the
staged path is design tokens first (color and density only, layout still fixed),
never a layout-level skin system — layout skinning is the specific mechanism
that made Kodi skins fragile.
