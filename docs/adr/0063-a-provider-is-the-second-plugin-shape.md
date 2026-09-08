# ADR 0063 — A provider is the second plugin shape, and it found two holes

Date: 2026-09-08 · Status: **accepted** 2026-09-08

Hole two was taken out of this decision and fixed on its own first, which
was the right order: the dangerous half of it was live in shipped code and
needed no provider to be reachable. See below.

Extends ADR 0007 (provider/local-source split), ADR 0019 (rating sources) and
ADR 0020 (the WASM isolation boundary). Supersedes nothing.

## Context

The plugin runtime is further along than "a sandbox exists". A guest SDK hides
the ABI, a first-party OMDb plugin runs as a `rating_source` holding no key and
no socket of its own, an equivalence test asserts it produces byte-identical
ratings to the host's native implementation, and bundles are signed, installed
and listed on an Add-ons page.

What it does not have is a second extension point. `rating_source` is the only
`Kind`, and ADR 0007 promised `Provider`, `LocalSource` and `TrailerProvider`
besides.

`m4-plugin-scope.md` says the catalog should be "written from what step 2
actually needed", and deferred the contract's versioning until a real plugin
existed. It now does. So the question is whether to publish what exists or to
build a second shape first.

**A second shape first**, and the reason is that publishing is the irreversible
half. The scope doc asserts that "if the plugin boundary can carry OMDb cleanly,
it can carry a metadata provider" — an assumption, never a result. OMDb is the
narrowest thing an extension point can be: one id in, some scores out, no
identity, no images, no failure worth distinguishing.

Trying a `Provider` against the existing ABI took an afternoon and found two
holes. Both are the sort that would have been found by a third party after the
contract was public.

### Hole one: the ABI cannot say "it went wrong"

`HandleRatings` returns `0` for an empty result, and the host reads `len(out) == 0`
as "no ratings". A plugin whose upstream API is down, rate-limited, or returning
nonsense has exactly the same way to say so as one that looked and found nothing.

For a rating source that is survivable: no scores today, scores tomorrow. For a
`Provider` it is not. "No candidates" means *this item is unmatched* — a
statement the enricher records and stops asking about — and "the API is down"
means *ask again later*. Collapsing them writes a permanent conclusion from a
temporary failure, which is the same class of mistake as a scan deleting a file
it could not read.

### Hole two: a plugin can make the host fetch any URL it likes

`Record.Artwork` is a list of URLs, and `artwork.Cache.Download` fetches them
with **no allowlist check at all**. That is correct today, because the only
things filling that field are first-party providers whose URLs come from our own
code.

Under a plugin it is a server-side request forgery with a delivery mechanism. A
plugin granted `api.example.com` in its manifest can return artwork pointing at
`http://127.0.0.1:8080/api/…`, a LAN address, or a cloud metadata endpoint; the
host fetches it, stores the bytes by content hash, and **serves them back as the
item's poster**. The plugin does not need to see the response — it can look at
it in the library like anybody else.

The capability model was built precisely to stop a plugin reaching the network
on its own. It does. This is the host reaching the network *on the plugin's
behalf*, through a field nobody thought of as a capability.

## Decision

### `provider` becomes a plugin kind, with search and fetch as separate exports

Two exports, `search` and `fetch`, mirroring the host interface. Multiple
exports need nothing new: the ABI is already one packed `(ptr,len)` per call and
the module may export as many as it likes.

**Ranking stays on the host.** `Candidate.Score` and `Breakdown` are filled by
`Rank`, never by the provider, and that is load-bearing rather than incidental
now: a plugin that could score its own candidates could promote itself over
TMDB, and matching confidence is the one number the library's identity depends
on.

### `Caps` is declared in the manifest, not exported by the module

The host needs to know whether a provider handles movies before deciding to load
it. A manifest is signed, and readable without instantiating anything; an export
means starting a WASM module to ask a question about whether to start it.

A plugin can misdeclare either way, so this trades no safety — only cost.

### The ABI grows an explicit failure, and that is ABI 2

A response becomes an envelope: a result, or an error with a message. Empty and
failed stop being the same answer.

This is a breaking change to a contract with exactly one implementation, all of
it ours. Doing it now costs a rebuild of the OMDb plugin. Doing it after
publication costs everybody else's, which is the entire argument for finding
this with a second shape rather than with a third party.

`ABIVersion` goes to 2 and the host keeps refusing anything it does not
implement, as it already does. **The version gates the whole contract, not each
function** — a plugin declaring ABI 2 gets all of it.

### Artwork a plugin returns is constrained to the hosts it declared

**Half of this shipped separately, ahead of this decision** — see
`internal/netguard`. Splitting it out turned up something this ADR had
understated: the manifest allowlist matches a *hostname*, and a hostname is
not a destination, so a plugin whose granted domain resolves to 127.0.0.1
could already reach the server itself. That needed no provider and no
artwork field — it was live for the rating plugins that exist today.
Outbound fetches now refuse private and local addresses, checked after
resolution and immediately before connect.

What remains is narrower and still worth doing. The guard stops a plugin
pointing the host at *internal* addresses; it does not stop one pointing the
host at an arbitrary **public** address. An artwork URL is a fetch the host
makes, unattributed, on a schedule the plugin influences — a serviceable
beacon, and a way to make the server talk to a host nobody granted.

So:

The URLs in a plugin's `Record.Artwork` are checked against that plugin's own
manifest allowlist before the host fetches any of them, using the same matching
`host_http_get` already applies. A URL outside it is dropped, and logged.

Three things follow, and they are worth stating because each is a place this
could be quietly weakened later:

- The check belongs to **the plugin's grant**, not to a global list. Two plugins
  may legitimately fetch from different image hosts, and a shared allowlist
  would be the union of everything anybody was ever granted.
- It happens **before the fetch**, not by filtering results afterwards. A
  request to an internal address has already done its damage when it is
  answered.
- First-party providers are unaffected. They are not plugins, their URLs come
  from our code, and putting them behind the same list would mean maintaining an
  allowlist for ourselves to protect against ourselves.

### What is deliberately not decided here

**`LocalSource` is not becoming a plugin kind.** It reads files beside the
media, which means handing a plugin a filesystem path — the one authority the
whole boundary exists to withhold. It needs its own decision and probably its own
mechanism, and folding it in here because it is on ADR 0007's list would be
letting a list make an architectural choice.

**`TrailerProvider`** is left until something wants it. It is small enough to add
whenever, and adding it now would be a third shape proving nothing the second
did not.

## Consequences

**A second first-party plugin**, so this is proven the way OMDb proved the first:
a real provider running as a plugin, with an equivalence test against the native
implementation. TMDB is the obvious candidate and the honest test, being the one
whose absence would be noticed.

**The OMDb plugin is rebuilt against ABI 2**, and its equivalence test keeps it
honest across the change.

**Only then** the author-facing documentation, the versioning policy, and a test
harness that runs a `.wasm` without the server — the things that make this
usable by somebody who is not us. They are worth writing once the contract has
survived a second shape, and not before.

**What would make this wrong.** If a provider plugin turns out to need something
the enricher can only give it by handing over library data — a list of what is
already matched, say — then the boundary is in the wrong place for providers and
the answer is not to widen the capability model quietly. It is another decision,
in public, about what a third party may read.
