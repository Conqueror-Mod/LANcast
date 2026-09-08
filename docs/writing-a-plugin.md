# Writing a LANcast plugin

This is the author's side of the plugin contract. The decisions behind it are in
[ADR 0020](adr/0020-plugin-isolation-boundary.md) (the isolation boundary),
[ADR 0021](adr/0021-plugin-distribution-and-trust.md) (signing and consent) and
[ADR 0063](adr/0063-a-provider-is-the-second-plugin-shape.md) (the provider
shape and ABI 2); the server-side API for installing one is in
[api.md](api.md#plugins). This document is how you write the thing itself.

**Read [What does not work yet](#what-does-not-work-yet) first if you are not
one of us.** One thing is still missing that a third party needs, and it is
named there rather than discovered halfway through.

---

## What a plugin is

A `.wasm` module and a manifest declaring what it needs. The server compiles the
module with **only** the host functions your manifest asked for and an operator
approved — nothing else. There is no ambient filesystem, no socket, no database,
no environment.

You return data. The host owns every piece of persistence, and that is the
whole point: a plugin can be wrong, or hostile, without being able to reach
anything it was not handed.

Concretely, a plugin **cannot**:

- open a network connection (the host fetches for you, to hosts you declared)
- read or write any file
- read the library, the database, or another plugin's anything
- decide how confident a metadata match is (see [Ranking](#ranking-is-not-yours))
- make the host fetch an image from a host it was not granted

and **can**:

- ask the host to fetch a URL on a granted host
- read a configured secret it was granted by name
- write a log line, attributed to it
- return a result, or an error explaining why it could not

## The two kinds

| `kind` | exports | adapts to | what it adds |
|---|---|---|---|
| `rating_source` | `ratings` | `meta.RatingSource` | third-party scores for a title already identified by IMDb id |
| `provider` | `search`, `fetch` | `meta.Provider` | a metadata source: what a file *is*, and its record |

`rating_source` came first and is the narrow one. `provider` is the shape that
proved the contract generalises — building it is what found the two holes ADR
0063 records.

`LocalSource` is deliberately **not** a plugin kind: it reads files beside your
media, which means handing a plugin a filesystem path, the one authority the
boundary exists to withhold. That needs its own decision, not an entry on a list.

---

## A rating source, end to end

The smallest useful plugin. Ratings for a film, from a service that answers on
an IMDb id.

```go
package main

import (
	"encoding/json"
	"errors"

	"lancastplugins/sdk"
)

func main() {}

//go:wasmexport alloc
func alloc(size uint32) uint32 { return sdk.Alloc(size) }

//go:wasmexport ratings
func ratings(ptr, length uint32) uint64 {
	return sdk.HandleRatings(sdk.Input(ptr, length), lookup)
}

func lookup(imdbID string) ([]sdk.Rating, error) {
	if imdbID == "" {
		return nil, nil // nothing to look up: not a failure
	}
	key := sdk.Secret("example_key")
	if key == "" {
		return nil, nil // not configured is not a failure either
	}

	body := sdk.HTTPGet("https://api.example.com/v1/title/" + imdbID + "?key=" + key)
	if len(body) == 0 {
		// The host refused the fetch, or it failed. This one IS a failure.
		return nil, errors.New("example.com request failed or was denied")
	}

	var doc struct {
		Found bool    `json:"found"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("example.com returned something this plugin cannot read")
	}
	if !doc.Found {
		return nil, nil // looked, and there is no such title
	}
	return []sdk.Rating{{
		Source:  "example",
		Score:   doc.Score, // normalised 0–10
		Display: "8.1",     // whatever the service's own scale reads as
	}}, nil
}
```

`alloc` is required of every plugin — it is how the host writes your input into
your memory. Re-export it exactly as above and never think about it again.

Its manifest, `plugin.json` beside the module:

```json
{
  "name": "example-ratings",
  "version": "0.1.0",
  "abi": 2,
  "kind": "rating_source",
  "capabilities": {
    "http": ["api.example.com"],
    "secrets": ["example_key"]
  }
}
```

Build it:

```bash
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
```

Go 1.24 or newer, for `//go:wasmexport`. Getting `lancastplugins/sdk` to resolve
needs one more step today — see [What does not work yet](#what-does-not-work-yet).

Everything above is verified rather than illustrative: this plugin compiles,
loads into the runtime, reads its granted secret, fetches through the host to
its granted hostname, and returns the rating.

---

## Nothing is not the same as failure

If you take one thing from this document, take this one. It is the mistake the
ABI itself made until ABI 2, and it is the reason ABI 2 exists.

**Return no result and no error** to say *I looked, and there is nothing.*
**Return an error** to say *I could not look.*

They are different answers and the host acts on them differently. For a provider
the difference is severe: "no candidates" means *this item is unmatched*, which
the enricher records and stops re-asking, while "the API is down" means *ask
again later*. Collapse them and a two-hour outage writes a permanent conclusion
into somebody's library about every file it touched — the same class of mistake
as a scan deleting a file it could not read.

So:

| situation | return |
|---|---|
| no id to look up, nothing configured | `nil, nil` |
| the service answered "no such title" | `nil, nil` |
| the fetch failed or was denied | `nil, err` |
| the response would not parse | `nil, err` |
| your key was rejected | `nil, err` |

`sdk.HTTPGet` returning empty is **always** the second column. It cannot tell
you why (see [What does not work yet](#what-does-not-work-yet)), but it is never
a legitimate "no results" — a service with nothing to say still answers.

Your error message reaches the server log with your plugin's name against it.
Write it for the person reading that log at the time: *"example.com rejected the
key"*, not *"error 3"*.

---

## Writing a provider

A provider answers two questions, as two exports.

```go
//go:wasmexport search
func search(ptr, length uint32) uint64 {
	return sdk.HandleSearch(sdk.Input(ptr, length), find)
}

//go:wasmexport fetch
func fetch(ptr, length uint32) uint64 {
	return sdk.HandleFetch(sdk.Input(ptr, length), get)
}

func find(q sdk.Query) ([]sdk.Candidate, error) { … }
func get(ref sdk.Ref) (*sdk.Record, error)      { … }
```

`search` takes what the scanner guessed from the filename — a title, maybe a
year, maybe a series and season and episode — and returns candidates. `fetch`
takes one candidate's `ExternalID` back and returns the full record.

A manifest for a provider also declares `caps`:

```json
{
  "name": "example-provider",
  "version": "0.1.0",
  "abi": 2,
  "kind": "provider",
  "caps": { "movie": true, "show": true, "episode": true, "artwork": true },
  "capabilities": {
    "http": ["api.example.com", "images.example.com"],
    "secrets": ["example_key"]
  }
}
```

`caps` says what you can answer for, and it is read **before** your module is
started — that is why it is in the signed manifest rather than an export. A
provider declaring none of `movie`, `show` or `episode` is refused at install,
because it could never be asked anything.

### Ranking is not yours

`Candidate` has no score field. That is deliberate and permanent: match
confidence is the number the library's identity rests on, and a plugin able to
score its own candidates could promote itself over the built-in provider and
quietly decide what somebody's files are. Give the host good candidates —
title, year, popularity, overview — and let it choose.

`Record` has no `source` field either. The host fills it with your manifest
name, so a plugin cannot sign somebody else's name to its answers.

### Every field is a pointer, and nil means something

```go
rec.Fields.Title = sdk.Str("Arrival")
rec.Fields.Year = sdk.Int(2016)
// Overview left nil: this source has nothing to say about it
```

**nil** means *I have nothing to say about this field*. **A pointer to the zero
value** means *I say it is empty*. The host merges field by field across sources,
so those are different instructions — and if you set everything you have a
value for and leave the rest nil, a better source's overview survives instead of
being overwritten with `""`. Use `sdk.Str`, `sdk.Int`, `sdk.Num` and `sdk.Int64`
to build them.

### Images must be on a host you declared

Every URL in `Artwork`, `Credit.Image`, `Collection.Artwork` and
`Candidate.PosterURL` is checked against your own manifest's `http` list before
the host fetches any of it. A URL outside it is dropped and logged.

This catches authors out, so it is worth stating why. An artwork URL is the one
field where a plugin makes the **host** reach the network — unattributed, on a
schedule the plugin influences. That is a capability, so it is granted like one.
If your images come from a different hostname than your API, declare both. The
first-party TMDB plugin declares `api.themoviedb.org` **and** `image.tmdb.org`
for exactly this reason.

---

## Secrets

A plugin never holds a credential in its binary. It declares a name in the
manifest, the operator approves that name at install, and the operator types the
value in **Settings → Add-ons**. `sdk.Secret` reads it back:

```go
key := sdk.Secret("example_key")
if key == "" {
    return nil, nil // not configured is not a failure
}
```

Three names are the **server's own** provider keys — `omdb_key`, `tmdb_key`
and `opensubtitles_key`. They come from Settings, and a plugin granted one reads
the same value the built-in provider uses; nothing is stored per-plugin for
them. Any other name is your own credential, kept against your plugin
specifically: two plugins both asking for `api_key` get two different values,
because a credential is obtained *for* a plugin.

Until the operator gives you one, `sdk.Secret` returns `""`. **Treat that as
"not configured", not as a failure** — a server with no key for your service
is a working server, it is just one you have nothing to answer for. The
Add-ons page tells the operator which granted secrets are still waiting for a
value, so the state is visible to them rather than only to you.

Nothing can read a value back out: no endpoint returns one, and the audit log
records that a secret was set, never what it was set to. The only route out of
the database is the host handing it to the guest that was granted it.

## What the host does for you

Do not implement any of these. They are not yours to skip.

**Caching.** Every fetch is cached for 24 hours in the same store the built-in
providers use, keyed per plugin. A rescan of an already-enriched library costs
no API calls.

**Rate limiting.** Requests to any one remote host are paced to the operator's
configured rate, **shared across every plugin**, because the remote sees one
caller regardless of how many plugins are talking to it. A cache hit does not
spend a token.

**Redaction.** Your URLs are trimmed to scheme, host and path before they are
logged, because a query string is where an API key lives.

These live in the host and not in the SDK on purpose: a rate limit a plugin
implements is one a plugin can choose not to have, and the request goes out over
the *operator's* IP address. They would be the one banned, for something they
cannot see and did not do.

---

## Packaging and installing

A `.lcplugin` is a zip of `plugin.json`, `plugin.wasm`, and an optional detached
`signature` over a SHA-256 of the length-prefixed manifest and module together —
so neither can be swapped without changing the digest.

```bash
# a directory holding plugin.json and plugin.wasm
lcplugin sign -in ./example -out example.lcplugin            # unsigned
lcplugin sign -in ./example -out example.lcplugin -key my.key  # signed
lcplugin verify -in example.lcplugin
```

`lcplugin keygen -out my` makes an Ed25519 keypair. Signing is Ed25519 and the
signature is checked **before the module is ever compiled**.

Installing is deliberately two steps — upload to inspect, then grant to
activate — so approving capabilities is an explicit act rather than a side
effect of choosing a file. **Provenance and authority are independent**: being
signed by the project key gets you nothing extra, and being unsigned costs you
nothing except that the operator is told. Both get exactly the capabilities that
operator approved, and the *grant* is what authorises — your manifest can only
ask.

---

## Versioning

`abi` in the manifest is **2** today. The policy behind that number is
[ADR 0064](adr/0064-what-the-plugin-abi-promises.md); what it means for you is
below.

**The contract grows without breaking you.** These can appear in any release and
a plugin built before them keeps working:

- a new field in a payload, in either direction
- a new optional field in `plugin.json`
- a new plugin kind, or a new host function
- an export the host calls only when a module has it

**Two obligations on your side make that safe**, and they are part of the
contract rather than good manners:

- **Ignore fields you do not recognise.** Go's `encoding/json` does by default
  and the SDK relies on it; reaching for `DisallowUnknownFields` opts you out of
  every future addition.
- **Tolerate a field you expected being absent.** A host with nothing to say
  about something omits it.

**When the number does change**, it is for a real break — a payload field
removed or re-typed, a host function's signature changed, the calling convention
altered. Then you rebuild.

**You get a release to do it in.** When ABI *N* ships, the host keeps accepting
*N−1* for at least one subsequent release, and the removal is called out in the
release notes. That window exists because the ordering is otherwise impossible:
nobody can publish a build for an ABI that has not shipped yet, so a hard
cutover would strand every plugin on day one. It is one version deep, not
"everything ever shipped" — the host is not going to carry five readers.

ABI 1 is the exception and is refused outright. It was never public and its only
implementations were ours.

**The version gates the whole contract, not each function.** A module declaring
2 gets all of ABI 2. Per-function negotiation would mean the host supporting
every combination anybody ever shipped, which is the cost that buys a contract
nobody can reason about.

**You never ask the host what version it is.** You declare one and it either
runs you or refuses you, so a running plugin is always running the version it
was built for.

**Your module is checked against its kind at load.** A `provider` that exports
no `search`, or anything at all missing `alloc`, is refused when it is
installed — with a message naming the export — rather than installing cleanly
and failing the first time it is asked something.

---

## What does not work yet

Writing this document from the outside found two things that stopped a third
party. One is fixed — a plugin can now have its own credential, see
[Secrets](#secrets) — and the other is below. It is better to say so here than
to let somebody discover it at the compile step.

**1. The SDK is not fetchable.** It lives at `plugins/sdk` in the LANcast repo,
in a module declared as `lancastplugins` — which is not an import path anybody
can `go get`. Today the only ways in are to vendor the `sdk` package into your
own module or to use a `replace` directive against a local checkout. Put your
plugin beside the LANcast checkout and point at it **relatively**:

```
// go.mod, in a directory that is a sibling of the LANcast checkout
module exampleplugin

go 1.25

require lancastplugins v0.0.0

replace lancastplugins => ../LANcast/plugins
```

Two traps, both found by doing it rather than by writing it down. A `replace`
target **cannot be an absolute Windows path** — `D:/…` fails as a malformed
module path on the colon — and it **cannot contain a space**, which rules out
`../My Projects/LANcast/plugins`. A relative path with neither is the form that
works.

Publishing the SDK under a real module path is a prerequisite for third-party
authorship, not a nicety.

**Also missing:** a way to run a `.wasm` against the ABI without a server, so
you can test a plugin without installing it into a running LANcast. Until that
exists, the honest development loop is the one the first-party plugins use — an
equivalence test in `internal/plugin` that drives the module through the real
runtime with the network faked.

**And a smaller one:** `sdk.HTTPGet` returns empty for *denied*, *failed* and
*answered with nothing*, so a plugin cannot tell the operator which happened.
ABI 2 gave the guest a way to report failure upward; the host→guest direction
still collapses three answers into one.

---

## Reference

### Host functions

| SDK call | what it does |
|---|---|
| `sdk.HTTPGet(url) []byte` | fetch through the host; empty on denial or failure |
| `sdk.Secret(name) string` | a granted, configured secret; `""` if either is missing |
| `sdk.Log(msg)` | a log line, attributed to your plugin |

### Entry points

| export | required for | helper |
|---|---|---|
| `alloc` | every plugin | `sdk.Alloc` |
| `ratings` | `rating_source` | `sdk.HandleRatings` |
| `search` | `provider` | `sdk.HandleSearch` |
| `fetch` | `provider` | `sdk.HandleFetch` |

If you need to return something the helpers do not cover, `sdk.Ok(v)` and
`sdk.Fail(msg)` pack the ABI 2 envelope directly.

### First-party plugins to read

Both are complete, shipped, and tested against the native implementation they
mirror — which is the most useful documentation there is.

- `plugins/omdb` — a rating source. Small enough to read in one sitting.
- `plugins/tmdb` — a provider. Movies, shows, seasons and episodes, with the
  awkward parts of a real API in it.
