# Plugin distribution & install flow — build plan

Implements [ADR 0021](adr/0021-plugin-distribution-and-trust.md): turn the raw
"drop a plugin.json + plugin.wasm in a directory" loader into a verified,
consent-gated install flow with the two-layer trust model — **authority** (the
capability grant) and **provenance** (signing). Four reviewable increments.

**What exists today:** `Runtime.LoadAll(<data>/plugins)` scans subdirectories of
unsigned `plugin.json` + `plugin.wasm` and registers each. That is a developer
convenience, not a trust boundary. This build replaces the *how it gets there and
why it's trusted* while keeping the runtime and capability model (ADR 0020)
unchanged underneath.

## Increment 1 — bundle format, signing, verification (`internal/plugin`)

The load-bearing, crypto-heavy phase.

- **Bundle**: a `.lcplugin` is a zip (or tar) containing `plugin.json`,
  `plugin.wasm`, and `signature` — a detached signature over a **canonical
  digest** of the manifest bytes + the wasm bytes (SHA-256 of a length-prefixed
  concatenation, so neither field can be swapped independently).
- **Signing**: **Ed25519** (small, fast, no parameter choices to get wrong). A
  `cmd/lcplugin-sign` tool signs a bundle with a private key; first-party plugins
  are signed with the LANcast project key.
- **Verification**: `VerifyBundle(bundle) (Manifest, wasm, Signer, error)` —
  recomputes the digest and checks the signature **before the wasm is ever
  compiled**. Signer is `first_party` (the embedded project public key),
  `pinned` (an operator-added key), or `unsigned`. A tampered digest or a bad
  signature is a hard failure; `unsigned` is a valid, named outcome, not an error.
- **Embedded project public key**: the public half committed and compiled in;
  the private half is generated and held by the maintainer, never in the repo.
- **Tests**: sign→verify round-trip with a generated keypair, tamper detection
  (flip a manifest byte, flip a wasm byte — both rejected), unsigned classified
  as `unsigned`, wrong-key classified as not-first-party.

**Open question to settle here:** zip vs tar (leaning zip — random access to
members, ubiquitous); and where the project public key constant lives.

## Increment 2 — install lifecycle & grant store (`internal/store` + `internal/plugin`)

- **Schema (migration, rev 11)**: an `installed_plugin` table — name (unique),
  version, digest, signer, `enabled`, installed_at, and the **granted
  capabilities** (the http hosts + secrets the operator approved, stored as JSON
  so the grant is a record, not re-read from the manifest).
- **Install**: verify the bundle, then record the plugin and its grant, unpacking
  the wasm into a managed dir keyed by digest. Re-install of a changed manifest
  (new digest) requires a fresh grant — the ADR's "authority never escalates
  silently."
- **Lifecycle**: enable/disable (flips `enabled`; drops registration on the next
  rebuild), remove (row + unpacked files), and a name+digest **blocklist** check
  at load.
- **Loader change**: `LoadInstalled(store)` replaces directory-scanning — it loads
  only enabled, non-blocklisted, still-verifying installed plugins, and hands the
  runtime the **recorded grant** (not the manifest's raw request) as the effective
  capabilities. The manifest can ask; only the grant authorizes.
- **Tests**: install records the grant; a manifest change forces re-grant; disable
  hides it from the registry; blocklist refuses load; the effective capability set
  is the grant, not the manifest.

## Increment 3 — admin API (`internal/api`)

All admin-only; `docs/api.md` updated in the same commit.

- `GET /api/plugins` — installed plugins with version, signer, enabled state, and
  granted vs requested capabilities (so the UI can show "wants X, granted Y").
- `POST /api/plugins` — upload a `.lcplugin`; returns the **verification result
  and the capabilities it requests**, staged pending an explicit grant (install
  is two-step: inspect, then approve).
- `POST /api/plugins/{name}/grant` — approve the requested capabilities and
  activate. Separating upload from grant is what makes the consent real.
- `POST /api/plugins/{name}/enable` · `/disable`, `DELETE /api/plugins/{name}`.
- Additive per [ADR 0018](adr/0018-api-contract-and-versioning.md).

## Increment 4 — Add-ons page (client)

- A **Settings → Add-ons** page: list installed plugins with signer badge
  (first-party / pinned / unsigned) and enabled toggle.
- **Install** = choose a `.lcplugin`, then a **capability-approval dialog** that
  states plainly "this plugin wants to reach www.omdbapi.com and read your OMDb
  key," with the signer's provenance shown. Grant or cancel.
- Enable/disable/remove controls. The unsigned state is named, not hidden.
- Ties off the roadmap's placeholder **Add-ons page** backlog item.

## Sequencing & PRs

1. **PR 1 — bundle + signing + verification** (crypto core; the security-critical
   review).
2. **PR 2 — install lifecycle + grant store** (schema + loader change).
3. **PR 3 — admin API** (+ docs).
4. **PR 4 — Add-ons page** (client + the capability-approval UI).

Each is independently reviewable; the trust primitives land and are tested in
PR 1 before anything grants authority on top of them.

## Deliberately out of scope

- **A remote plugin repository / discovery.** ADR 0021: operator-opt-in only,
  never a startup default. Not this build.
- **Auto-update.** Opt-in, re-verified like a fresh install — a later,
  small addition once the manual flow is trusted.
- **New capability kinds** (filesystem scratch, library-write). Each is a new line
  in the grant prompt when a real plugin needs it.
- **Signing-key rotation tooling.** The embedded-key set supports it; the rotation
  workflow is written when first needed, not pre-built.

## What the maintainer must do (not code)

Generate the **project Ed25519 signing keypair**, commit the public half (I'll
wire where it goes), and guard the private half outside the repo — the first-party
signature depends on it. I can scaffold the keygen + sign tooling; the key
material and its custody are yours.
