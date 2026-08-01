# ADR 0021 — Plugin distribution and trust

Date: 2026-08-01 · Status: proposed

## Context

The plugin runtime is built and proven ([ADR 0020](0020-plugin-isolation-boundary.md);
the OMDb plugin produces ratings byte-identical to the native source across the
WASM boundary). What is not yet decided is how a plugin gets *onto* a server and
why the operator should trust it — the last piece of M4, and the one the roadmap
names by its failure mode: **Kodi's**. Kodi's extensibility was its best idea and
its worst wound: add-ons installed from arbitrary repositories, unsigned, with
effectively unlimited access to the host. "Everything is a plugin" became
"anything can do anything."

Two things make LANcast's position different, and the trust model must build on
both rather than reinventing either:

- **Authority is already bounded ([ADR 0020](0020-plugin-isolation-boundary.md)).**
  A plugin can only do what its manifest declares and the host grants — a named
  HTTP host, a named secret, nothing else. It cannot touch the database, the
  filesystem, or a socket. So the worst a *malicious* plugin can do is already
  small, before any question of who wrote it.
- **No phone-home is a principle, not a setting.** LANcast fetches nothing the
  operator did not ask for. A default plugin marketplace that reaches out on
  startup would violate that outright.

The format the trust model attaches to is now stable: a manifest
(name, version, abi, kind, capabilities) plus a `.wasm`, validated end to end by
a real plugin. Deciding distribution now is deciding it against something real,
not speculation — the condition the scope doc set for doing this last.

## Decision

Trust is **two independent layers**, and neither substitutes for the other. This
is the explicit answer to Kodi, where a trusted-looking add-on carried unlimited
authority.

### 1. Authority — the capability grant (the load-bearing layer)

At install, the operator is shown exactly what the plugin's manifest declares —
which hosts it may reach, which secrets it may read — and must **explicitly grant
it**. Nothing is granted by default. A plugin whose manifest later changes its
capabilities is a different grant and must be re-approved; because the manifest
is covered by the signature below, a capability change invalidates the signature
and forces the operator back through approval. **Authority never escalates
silently.** This layer holds *regardless of who wrote the plugin* — it is why an
unsigned plugin is a bounded risk, not an open door.

### 2. Provenance — signing over a content digest

A plugin is distributed as a **signed bundle**: the manifest, the `.wasm`, and a
detached signature over a canonical digest of both. The loader recomputes the
digest and verifies the signature **before compiling the module**; a mismatch
refuses to load. Two roots of trust:

- **First-party plugins** are signed by the LANcast project key, whose public
  half is embedded in the binary. The OMDb plugin ships this way.
- **Third-party plugins** are verified against a publisher key the operator
  **explicitly pinned**, or installed as **"unsigned — you accept the risk,"** a
  state the UI names plainly. Unsigned is permitted (self-hosting must not be
  gated on a signing bureaucracy) but never the silent default, and the capability
  grant still applies in full.

Signing proves *integrity and origin*; it does not expand authority. A signed
plugin still gets only the capabilities the operator granted.

### 3. Installation — deliberate, local, admin-only

Install is an **admin action on a local bundle file** the operator provides.
There is **no default remote marketplace and no auto-update** — both would breach
no-phone-home. An operator *may* explicitly add a plugin repository they trust;
even then, installing from it still requires the capability approval and
signature check above. Updates are opt-in and re-verified like a fresh install.

### 4. Revocation

- The operator can **disable or remove** any plugin; disabling drops its registry
  registration on the next rebuild.
- **Revoking a capability grant disables the plugin** — authority and presence are
  the same switch.
- A shipped **name+digest blocklist** can neutralise a known-bad first-party
  build across updates, and the project signing key can be rotated (old key
  retired in the embedded set) if it is ever compromised.

## Consequences

**Good — Kodi's failure is structurally excluded.** Even a signed, trusted-looking
plugin has only granted authority; even an unsigned one cannot exceed its
capabilities. The two layers fail independently, so a lapse in one (a leaked
signing key, a careless grant) is not total.

**Good — no-phone-home survives contact with extensibility.** Nothing installs or
updates without an explicit local action. The absence of a built-in marketplace
is a feature, consistent with how remote access and metadata keys already work.

**Good — the capability prompt is honest.** Because the manifest is signed and the
grant is per-capability, "this plugin wants to reach www.omdbapi.com and read your
OMDb key" is a true and complete statement of what it can do — the thing Kodi
never surfaced.

**Cost — an install UX to build.** Showing capabilities, recording the grant,
verifying signatures, and handling the unsigned path is real surface, admin-only.
Deferred to its own build; this ADR fixes the format and the rules it implements.

**Cost — key management.** A project signing key must be generated, guarded, and
its public half embedded; a rotation story must exist before third parties rely on
it. Real operational weight, accepted as the price of verifiable first-party
provenance.

**Cost — manifest and bundle format additions.** Signature, publisher, and a
digest belong in the distributed bundle, and a recorded grant belongs in the
operator's data. These are the expensive-to-retrofit fields, which is why the
format is decided now rather than after an install flow already exists.

## Alternatives considered

- **A central plugin marketplace / registry.** Rejected: a default outbound
  service is a no-phone-home violation and a curation burden a self-hosted project
  should not carry. An operator-added repository is allowed; a built-in one is not.
- **Sandbox only, no signing.** Rejected: the ADR 0020 sandbox bounds *authority*
  but says nothing about *integrity or origin* — supply-chain tampering and silent
  updates still matter. Signing and sandboxing answer different questions; dropping
  either leaves a real gap.
- **Signing only, trust-by-provenance.** Rejected outright: this *is* the Kodi
  trap in reverse — "it's signed, so let it do anything." Authority must be bounded
  independent of who signed it.
- **Mandatory signing (no unsigned plugins).** Rejected: it gates self-hosting and
  local development on a signing process, against the project's bring-your-own,
  own-your-instance ethos. Unsigned is allowed, named, and still capability-bound.

## Revisit if

- **A community forms around third-party plugins.** A discovery mechanism may be
  wanted; it stays operator-opt-in and never a startup default, and the two-layer
  trust model above does not change — only how a bundle is found.
- **The capability set grows past HTTP and secrets** (filesystem scratch, a
  library-write capability). Each new capability is a new line in the install
  prompt and a new entry in the grant record; the model holds, the surface widens.
- **A first-party signing key is compromised.** The rotation and blocklist paths
  named above are exercised; if they prove inadequate, this decision reopens.
