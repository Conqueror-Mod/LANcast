# ADR 0078 — A place is the town a photo was taken in

Date: 2026-10-09 · Status: **accepted** · Amends [ADR 0028](0028-pictures-library.md)

## Context

ADR 0028 said **GPS is not read**: it is location data about the user,
LANcast had no use for it, and the safest way never to leak it was never to
load it. `exif.go` was written so that refusal was a missing parser rather than
a skipped call.

The roadmap reopened it on 2026-10-04 — *GPS may be read* — and left one
question open: a map needs map imagery, and fetching tiles from a public map
service breaks no-phone-home. So the places view is either a list grouped by
place with no map, or a map drawn from imagery the owner hosts.

Before deciding anything, every photograph in the reporting library was read
with the parser this ADR adds, read-only, against a copy of the live database:

| | photos | carry GPS |
|---|---|---|
| all | 3,073 | **461 (15%)** |
| `.jpg` | 2,954 | 461 |
| `.heic` | 19 | 0 — the tag is not there; the container was read correctly |
| `.png`, `.bmp`, `.webp`, `.jpeg` | 100 | 0 |

Grouped to the nearest town (below), the 461 fall into **ten places**, and
none is more than 50 km from a town. That is a list somebody reads in one
glance. A map of ten pins would be a large dependency — tiles to host, a
renderer, a projection — in service of a picture the list already gives.

A side finding, not fixed here: the 19 HEIC photographs carry a capture time
that `readEXIF` never reads, because it only finds EXIF in a JPEG or a TIFF.
All 19 are therefore missing from the Timeline. It is the same container walk
this ADR adds for location, and it wants its own change because it also moves
those photographs' place in every date-sorted view.

## Decision

**A place is a list, not a map.** The Places view groups a picture library's
photographs by the town they were taken in, under their region and country,
and opens a place into a grid exactly the way the Timeline opens a month.
A map stays possible later from owner-hosted imagery, and nothing here
assumes one.

**Off until somebody turns it on.** `photo_places` is a server setting and it
defaults to off. Nothing reads a coordinate until an administrator chooses
"Read where photos were taken", on the Places view's empty state or in
Settings. Turning it off **deletes every stored location and place**, not
just hides them — so off means what ADR 0028 meant, and on is a decision
somebody made rather than one an update made for them.

**The parser stays apart from the thumbnail pass.** `location.go` holds the GPS
reader, and only the location pass calls it. `readEXIF` still reads two tags,
and a test asserts a GPS-only block is still "no EXIF" to it.

**Places are named locally.** The towns come from GeoNames' populated places
of 1,000 people or more (CC BY 4.0), trimmed and embedded in the server —
about 3 MB. Naming a coordinate with any online geocoder would send where a
family's photographs were taken to somebody else's server, which is the exact
thing no-phone-home exists to refuse. Provenance and the reproducible build of
the tables are in `third_party/geonames/PROVENANCE.md`.

**The nearest town is weighted by its size.** Nearest-point alone is wrong
exactly where people take photographs: on the real gazetteer, Times Square was
Weehawken, the Eiffel Tower was "Paris 16 Passy" and the Opera House was
The Rocks. Each town is given a reach — the radius of a disc holding its
population at 3,000 people per km², at least half a kilometre — and a photo
goes to the town where distance ÷ reach is smallest. A city claims its own
streets; a small town keeps the ground around its centre (Wolfforth, 16 km
from Lubbock, stays Wolfforth). The cost is stated rather than hidden: a photo
in a small place right beside a big city's centre goes to the city.

**Fifty kilometres is the limit of "near".** A photograph further than that
from any town has no place. It keeps its coordinates and is simply not
grouped: a boat named after a town two hundred kilometres away is a wrong
answer presented as a right one.

**Its own pass, reading only the start of each file.** EXIF sits near the
front of a JPEG, and in the reporting library's HEIC files at about 18 KB. The
pass reads at most 512 KB a photo and decodes nothing: the whole library took
about 11 seconds. Its queue is a query, like every other worker's — a photo
with no `photo_location` row, in a picture library, while the setting is on —
and it is **kicked at startup**, after a scan, and when the setting is turned
on, because a stamp nothing kicks is a queue nothing drains.

**Schema revision 67 adds two tables and changes no existing shape.**

- `photo_location (item_id, lat, lon, place_id, read_at)` — one row for every
  photo the pass has read, with `lat`, `lon` and `place_id` NULL when the photo
  carries nothing. A side table like `photo_hash`, because only the location
  pass writes it and nothing that lists items wants a coordinate. Like the
  hash it is a fact about bytes, so it is deleted when a rescan finds the file
  changed, and the pass reads it again.
- `photo_place (id, name, region, country_code, country)` — the towns
  photographs were filed under, keyed by GeoNames id. Names are copied in at
  assignment, so listing places never loads the gazetteer; it is loaded only
  while the pass runs.

**What leaves the server is a name and a count.** The API lists places with
their counts and lists a place's photographs. It never returns a coordinate:
nothing in the client needs one without a map, and a field that is not sent
cannot be scraped.

**Marked folders are not in it** (ADR 0051, amended), for the Timeline's
reason. A cover cannot be lifted here, and a covered tile under a town still
says where the marked photographs were taken, which is much of what marking a
folder is trying not to say.

**Not offered to paired servers.** The places routes are not on the
federation guest allow-list. A library shared with a friend's server shares
its photographs, which carry their own EXIF; it does not share an index of
where the family goes.

## Consequences

- **The originals still carry GPS, and always did.** The original-file route
  serves the photograph's bytes as they are, so anybody who can open a photo
  could always read its location from it. This ADR does not change what a
  person with access can learn. It changes whether the *server* derives and
  indexes it, and that is what the switch governs.
- About 3 MB is added to `LANcast-Server.exe` (27 MB before).
- The gazetteer is a snapshot. A town renamed after 2026-10-09 keeps its old
  name until the tables are rebuilt and the locations read again.
- Districts are not places. A city is one place, however large.

## Not decided here

- **A map**, which needs imagery the owner hosts.
- **Renaming a place**, such as calling a town "Home". If it comes, it is an
  edit, and edits are locked.
- **HEIC capture time**, above.
- **Bursts and RAW**, which remain open on the roadmap.
