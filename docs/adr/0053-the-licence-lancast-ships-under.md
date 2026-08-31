# ADR 0053 — The licence LANcast ships under

**Status:** proposed — decision is Chris's
**Date:** 2026-08-31

LANcast is MIT today. The question raised is whether it should move to GPL or to
proprietary. This records what is actually possible, what each option costs, and
what has to be true before face grouping can ship either way.

**This is not legal advice.** It is the engineering shape of the decision, with
the facts checked rather than remembered.

## What is true right now

**The copyright is undivided.** `Conqueror-Mod` is the sole contributor —
1,041 commits, nobody else. That is the fact that makes this a real choice: a
project with outside contributors cannot be relicensed without their agreement,
and this one has none to collect.

**Nothing in the dependency tree forces a licence.** Every Go dependency is
permissive:

| dependency | licence |
|---|---|
| `fyne.io/systray`, `github.com/tetratelabs/wazero` | Apache-2.0 |
| `golang.org/x/{crypto,image,sys}`, `modernc.org/sqlite` | BSD-3-Clause |
| `go-humanize`, `go-isatty` | MIT |
| `godbus/dbus`, `google/uuid`, `bigfft` | BSD |
| hls.js (vendored, ADR 0013) | Apache-2.0 |

There is **no GPL anywhere in the tree**, so both directions are open. That is
not luck — a pure-Go, cgo-free tree is a permissively-licensed tree by
construction.

**The one thing to be careful with is ffmpeg**, which is GPL or LGPL depending
on the build. LANcast **shells out to it** and, since ADR 0048, **downloads it
onto the user's machine** rather than shipping it. Separate process, not linked,
not redistributed — which is the arrangement that keeps ffmpeg's terms off
LANcast's own. Bundling an ffmpeg binary into the installer would change that
and should not be done casually under any of the options below.

## What a relicence can and cannot do

**It is forward-only.** Every version already published stays MIT for ever.
v0.8.44 is on GitHub under MIT, and anyone who has it may fork it, close it,
sell it, and continue for as long as they like. Relicensing changes what happens
to *future* work; it retracts nothing.

That is worth sitting with before choosing, because it decides how much the
choice is actually worth. What it protects is the next year of work, not the
last one.

## The options

### Stay MIT

Anyone may take LANcast, close it, and sell it without giving anything back.
That is the deal MIT makes, and for a project whose goal is adoption it is the
right one.

### GPL-3.0 — or, for a server, AGPL-3.0

A fork must stay open. **For this project the AGPL is the version that means
anything**: plain GPL is triggered by *distribution*, and a media server can be
run for other people over a network without ever being distributed. AGPL closes
exactly that gap.

Note that Apache-2.0 dependencies are compatible with GPLv3 and **not** with
GPLv2, so if this route is taken it is v3.

### Proprietary

Source closed, terms whatever Chris writes.

**The cost is specific to this product, and it is not small.** LANcast's pitch —
the four principles in the README, no phone-home, everything on the box, face
grouping done locally as *"the sharpest available statement of the
difference"* — is a claim about trustworthiness. A self-hosted server that
handles somebody's family photographs, and now their faces, is asking for a
degree of trust that open source is the ordinary evidence for. Closing it does
not make the claim false, but it removes the way anybody checks it.

### Dual: AGPL plus a commercial licence

Available **only because the copyright is undivided**, and worth naming for that
reason. The project stays open and auditable; anybody who wants to build a
closed product on it buys terms. It is the standard arrangement for exactly this
situation and it forecloses nothing.

## Recommendation

**AGPL-3.0, with a commercial licence available if wanted.**

It matches what the project already says about itself, it protects the next year
of work in the way that actually applies to a server, and — because the
copyright is undivided — it leaves the commercial door open rather than closing
it. Proprietary buys control over something already public in full history, at
the price of the trust story that is the product's main differentiator.

But this is Chris's call, and the engineering consequences of all three are
manageable.

## What this decides about face grouping

Whichever way it goes, **a non-commercial model is out**.

Not because LANcast is commercial today, but because bundling
`shape_predictor_68_face_landmarks.dat` or InsightFace's `buffalo_l` would
quietly decide this ADR by making one of the options unavailable — and it would
do so in a `.dat` file nobody reads the terms of. ADR 0052 therefore chooses
YuNet (MIT weights) and SFace (Apache-2.0 weights), which are clean under every
option here.

**This belongs in a check.** Model files arrive as opaque binaries; their terms
live on a web page somebody read once. The `lancast-faces` build should record,
beside each model it ships, the licence it ships under — so that adding a model
with the wrong terms fails a build rather than a lawyer.

## Whatever is chosen, do these

1. **Decide before the next release**, so there is a clean version boundary
   rather than a licence that changed somewhere in the middle of a series.
2. **Keep `NOTICE`/`THIRD_PARTY` accurate** — Apache-2.0 requires attribution
   to be carried, and hls.js is already vendored under it.
3. **If proprietary or dual: require a CLA before accepting any outside
   contribution.** The undivided copyright above is an asset, and it is lost the
   first time somebody else's patch is merged without one.
4. **Do not bundle ffmpeg.** The download-it-on-first-run flow is not only a
   size decision; it is what keeps ffmpeg's terms at arm's length.
