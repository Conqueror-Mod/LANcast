# ADR 0004 — Spatial keyboard focus model, built at M3

Date: 2026-07-22 · Status: accepted

## Context

The reference client is a desktop browser, driven by mouse and keyboard. A TV
client is wanted eventually but is explicitly an M4-or-later concern.

The tempting sequence is: build the desktop client with ordinary web
accessibility (tab order, focus rings, escape to close), then build TV navigation
when the TV client is actually started.

That sequence is a trap. A TV remote is a **spatial** input device — up, down,
left, right, select. Tab order is *linear*. They are not the same model, and
retrofitting spatial navigation onto a component tree built for linear tab order
means touching every interactive component in the application. It is the kind of
work that gets deferred forever, and it is the reason so many self-hosted media
servers have a web UI that is unusable from a couch.

## Decision

Build a **central roving-tabindex controller with arrow-key spatial resolution**
from the first component of the React client at M3 — not when the TV client
starts.

Components declare themselves focusable and register their position; the
controller owns spatial resolution and key handling. Components do not implement
their own arrow-key logic.

## Consequences

**Good.** The TV client becomes a restyle rather than a rewrite. Larger type,
bigger tiles, simplified chrome — but the navigation model, the focus
controller, and the component tree carry straight over.

**Good.** Focus handling is centralized and therefore consistent. Divergent
per-component key handling is the usual source of "focus went somewhere
unexpected" bugs, and this design makes that class of bug structurally
impossible.

**Good.** It composes with the gold discipline rule. The gold focus ring is
already the strongest signal on screen, which is exactly the requirement for
ten-foot viewing — the aesthetic decision and the TV requirement turn out to be
the same decision.

**Good.** Desktop power users get real keyboard navigation and a command palette
as a side effect, at close to no additional cost.

**Cost.** Meaningful upfront work in M3 for a benefit that is not collected
until M4 or later. This is the entire trade, and it is accepted because the
alternative is not "do it later" but "never do it".

**Cost.** Every new interactive component must register with the controller.
This needs to be caught in review; a component that handles its own arrow keys
is a defect, not a shortcut.

## Verification

Navigate the entire application — home, browse, detail, playback, settings —
without touching the mouse. **Any dead end is a bug, and specifically a bug that
would block the TV client.** This test runs before any M3 work is called done.
