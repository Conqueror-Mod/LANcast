# ADR 0064 — What the plugin ABI promises

Date: 2026-09-08 · Status: **accepted** 2026-09-08

Built in the same change. `MinABIVersion` makes the window a constant rather
than a rule somebody remembers, and `Plugin.HasExport` makes an optional
export genuinely additive rather than aspirational — see decision 4, which
was written knowing the code did not yet support it.

Extends [ADR 0020](0020-plugin-isolation-boundary.md) (the isolation boundary)
and [ADR 0063](0063-a-provider-is-the-second-plugin-shape.md) (which set
`ABIVersion = 2`). The sibling for the HTTP surface is
[ADR 0018](0018-api-contract-and-versioning.md), and this deliberately reuses
its shape — the two contracts have different audiences and the same problem.

## Context

`docs/writing-a-plugin.md` now exists, which changes the question. Until a
contract is written down for somebody outside, "what do we promise" is a
question nobody is entitled to ask. Once it is published, the answer is either
written or improvised, and improvised means discovered by somebody's plugin
breaking.

What exists today is one line:

```go
if m.ABI != ABIVersion {
    return m, fmt.Errorf("manifest: abi %d unsupported (host implements %d)", m.ABI, ABIVersion)
}
```

An exact match, gating the whole contract rather than each function. ADR 0063
chose that deliberately and the reasoning holds: per-function negotiation means
the host supporting every combination anybody ever shipped, which is the cost
that buys a contract nobody can reason about.

**But exact match is not a versioning policy, it is the absence of one**, and it
has a consequence nobody decided. Under it, *every* change to the contract is
breaking — including changes that could not possibly break a plugin. Adding an
optional field to a request payload, which a guest's `json.Unmarshal` would
ignore, would require bumping `ABIVersion`, which would refuse every plugin in
the world until each was rebuilt. So the contract can only ever evolve by flag
day, and the pressure that creates is to never evolve it at all — or to sneak
changes in without bumping, which is worse because then the version number is
lying.

ADR 0018 has already solved this shape of problem for the HTTP API, with an
additive-safe rule and a written list of what breaks. The plugin ABI needs the
same, and the parallel is close enough that the differences are the interesting
part:

- **Both directions are code we do not control.** For the HTTP API, the server
  is ours and the client is not. Here the *host* is ours and the *guest* is not,
  and the host both sends and receives — so the additive rule has to hold in two
  directions rather than one.
- **The audience is smaller and the coupling is tighter.** A plugin is a
  compiled artifact for a specific ABI, installed by an operator who may not
  know what an ABI is. When it stops loading they do not read a changelog; they
  see an add-on vanish after an update.
- **There is no negotiation channel.** A plugin declares one number in a signed
  manifest and the host takes it or refuses. That is the whole protocol.

## Decision

### 1. The ABI is additive-safe within a version

These are **non-breaking** and may ship inside an ABI at any time:

- Adding a field to a **request** payload the host sends a guest.
- Adding a field to a **response** payload the host reads back.
- Adding an optional field to `plugin.json`.
- Adding a new `kind`.
- Adding a new host function.
- Adding an **optional** export the host calls only when a module has it.
- Adding a new value to an open set the contract already describes.

These are **breaking** and require a new `ABIVersion`:

- Removing or renaming a payload field, or changing its type, units or meaning.
- Making a previously optional field required, in either direction.
- Removing or renaming a host function, or changing its signature.
- Making an existing export mandatory when it was not, or changing what an
  existing export is called with or must return.
- Changing the calling convention: the packed `(ptr<<32)|len` return, the
  `alloc` contract, or the response envelope's shape.
- Changing the meaning of an existing `kind`.

**Two guest obligations make the additive rule safe**, and they are part of the
contract rather than folklore, exactly as ADR 0018's client obligations are:

- A guest **must ignore unknown fields** in what the host sends it. Go's
  `encoding/json` does this by default and the SDK relies on it; an author who
  reaches for `DisallowUnknownFields` is opting out of every future addition.
- A guest **must tolerate a field it expected being absent**, because a host
  that has nothing to say about something omits it.

### 2. The host supports the current ABI and the one before it

When ABI *N* ships, the host keeps accepting *N−1* for **at least one
subsequent release**, and the removal is called out in release notes.

This is ADR 0018's deprecation rule, adopted for the same reason and with more
force. A self-hosted operator cannot be upgraded in lockstep with anything: they
update LANcast when they update it, and the plugin's author may be asleep, busy,
or gone. Breaking every add-on the moment the server updates makes an ABI bump a
thing that silently removes features people were using, which is a worse outcome
than any tidiness it buys.

**Two versions, not many.** The window is one, not "everything we ever shipped",
because a host carrying five payload readers is the thing ADR 0063 refused under
a different name. Two is the number that lets an author ship an update *after*
the host does, rather than needing to ship it first.

**ABI 1 is exempt and stays unsupported.** It is tempting to apply this rule
retroactively and it would be dead code: ABI 1 was never public, its only
implementations were ours, and ADR 0063 broke it precisely because that was
true. The policy starts at 2, and this paragraph exists so that nobody later
reads the rule, notices ABI 1 is refused, and files it as a bug.

### 3. A new plugin kind is not a new ABI

`RegisterInto` already skips a kind it has no registration path for, with a log
and not an error. That behaviour is now policy: **the set of kinds is open**, the
same way `kind` and `match_state` are open in ADR 0018.

It cuts both ways, and the second way is the useful one. A *newer* host meeting
an older plugin is the easy case. An *older* host meeting a manifest naming a
kind it has never heard of refuses that one plugin at load, which is correct and
local — it does not take down startup, and it is exactly what
`supportedKinds` already does.

### 4. An optional export is additive, which obliges a code change

The rule above says adding an optional export is non-breaking. **That is not
true today**: `Plugin.Call` returns an error when a module has no such export,
so a host calling a newly-added optional function would fail every plugin built
before it.

So this ADR obliges the host to be able to ask. `wazero.CompiledModule` reports
`ExportedFunctions()` at compile time, without instantiating anything, so the
host records what a module exports at load and calls an optional export only
when it is there. That mirrors how optional capability already works on the
native side — `TrailerProvider` is a type assertion, not a required method
answering "unsupported" (ADR 0007).

Writing this rule without that change would be a policy that lies, which is
worse than no policy: the next author to add an optional export would follow the
written rule and break every installed plugin.

### 5. There is no version-discovery call, and none is needed

A guest does not ask the host what version it is. It **declares** one and the
host either runs it or refuses it, so a running guest is always running the
version it was built for. The HTTP API needs `api_version` on `/api/health`
because a client can point at any server; a plugin cannot be loaded by a host
that disagrees with it.

## Consequences

**The contract can evolve without a flag day.** Fields can be added to payloads,
kinds can be added, host functions can be added — none of it disturbs a plugin
built last year. That is what makes it possible to publish the ABI at all, since
the alternative is a contract frozen by fear of its own version check.

**An ABI bump becomes a real event.** It costs everybody a rebuild and costs us
a version of overlap to carry, which is the correct amount of friction for a
change that invalidates other people's work. ABI 2 was worth it because there
was one implementation and all of it was ours; that argument is no longer
available.

**The host carries two readers during a window.** Concretely, when ABI 3 ships
the host has to keep whatever ABI 2 did alongside it. This is the direct cost of
the rule and it is bounded by the window being one version deep.

**A plugin author has a release to react in.** They can ship after the host
does. Without the window the ordering is impossible: no author can publish a
build for an ABI that has not shipped, so a hard cutover strands every plugin on
day one by construction.

**Cost: the additive rule is a discipline, not a type.** Nothing stops somebody
renaming a payload field without bumping. The same is true of ADR 0018 and the
mitigation is the same — review, and the equivalence tests, which compare a
first-party plugin against the native implementation and fail when the two
disagree about a shape.

**Cost: "at least one release" is a weak promise in a self-hosted world**, where
somebody may skip six. It is deliberately a floor and not a ceiling. The
alternative — promising a duration in months — is a promise about our release
cadence that we would then be bound by, and a promise we might not keep is worse
than a modest one we will.

## What would make this wrong

**If a breaking change turns out to be needed that cannot wait a version.** A
security fix in the calling convention, say. The rule above would have the host
carry a known-bad ABI for a release, and that is the wrong trade — the answer
would be to break immediately and say so loudly, and to record here that the
deprecation window has a security exception. It does not have one today because
inventing exceptions before the case exists is how a policy becomes unreadable.

**If two ABIs turn out to be more than two readers.** The window is affordable
because ABI 1 → 2 was one difference, the envelope. If a future bump changes the
calling convention itself, "support N−1" could mean two dispatch paths, two
memory protocols, and a test matrix that squares. If that is what a bump looks
like, this rule needs re-deciding rather than honouring at any cost.

**If nobody ever writes a third-party plugin.** Then this is ceremony, and the
honest response is to say so and simplify — but the cost of having decided it is
one document, and the cost of not having decided it is discovered by somebody
else's users.
