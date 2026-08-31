# ADR 0051 — Sensitive content is obscured until asked for

**Status:** accepted, amended 2026-08-31
**Date:** 2026-08-30

A picture library can hold a folder whose contents are private in a way the
rest of the library is not — not shameful, not hidden, but not something to be
walked past at thumbnail size on the way to holiday photos. LANcast currently
has one setting for that: don't put it in the library.

This proposes a mark that obscures such a folder's thumbnails until a person
asks to see them.

## What it is not

**It is not access control.** A sensitive mark obscures; it does not restrict.
Anyone who can see the library can still open the folder — they just have to
mean it. "Georgia can see this and a guest cannot" is a different feature with
a different mechanism (per-user visibility), and building one while calling it
the other produces a privacy control that quietly does not work. If per-user
visibility is wanted later it layers on top; it does not come free with this.

**It is not a content classifier.** Nothing inspects an image. A person marks a
folder, and that is the only way a mark appears.

## The decision

Three parts.

### 1. The mark is a field on the item, not a rule the gallery screen applies

This is the part that decides whether the feature works.

The obvious implementation is a check in the gallery grid: if this folder is
marked, blur its tiles. That covers the screen it was written for and no other
— and a photo library's thumbnails appear on Recently Added, in search
results, in Continue, in the picture-of-the-day hero, and in whatever the next
screen is. A mark that only the gallery honours is a mark that ambushes you on
the home page, which is worse than no mark at all, because it was believed.

So `sensitive` is a resolved boolean on the item as the API returns it. Every
surface that draws a tile gets it without knowing the feature exists, and a new
surface added in a year gets it too.

### 2. It is inherited, and it is a lock

Marking a folder marks everything beneath it — photos and sub-folders — because
the alternative is marking 400 photos one at a time and missing three.

Stored on the gallery row (`media_item.sensitive`, nullable, so an unmarked
folder is `NULL` and not a decision anyone made) and **resolved by ancestry on
read**, not stamped onto descendants. Stamping means a photo added to the
folder next week is unmarked, which is exactly the case that matters.

The mark is a human decision about content, so it obeys the locked-fields rule:
no rescan, refresh, merge or provider may clear it. Scanning marks missing
rather than deleting, so a folder on an unmounted drive keeps its mark and gets
it back when the drive returns.

### 3. The client does not ask for the thumbnail

A CSS blur is a picture of a privacy feature. The bytes arrive, the element is
in the DOM, and anything that turns off styles — a stylesheet that has not
loaded, a reader view, a devtools panel, a slow first paint — shows the image
the mark exists to not show. The failure mode is the photograph appearing for a
moment on a page somebody else is looking at, which is the whole scenario.

So a covered tile does not build the artwork URL at all. No `<img>`, no request,
nothing to un-blur. There is a test asserting exactly that, because it is the
one claim the feature rests on.

**This was going to be a server-side guard and could not be.** The plan was for
`/api/artwork` to return the placeholder unless the request carried an
acknowledgement — server owns truth, client stays thin. Artwork is addressed by
*content hash* and served `Cache-Control: immutable`: a placeholder returned
under the real hash would be cached under that hash, for a year, for every
viewer and every item sharing it. The guard would have been a cache-poisoning
bug wearing a privacy feature's clothes.

The decision still belongs to the server, which is the part that mattered:
`sensitive` is computed there and arrives on the item. The client is left with
"do not ask for this", which is not a judgement.

## Acknowledgement

Accepting is per person, per device, and lasts the session. Closing the app
re-obscures.

A permanent "yes I know" is easy to build and defeats the purpose within a
month: the folder stops being marked in any way the person notices, and the
next time somebody else is in the room it is just a folder. Session scope keeps
the acknowledgement close to the act of choosing to look.

It is stored client-side per device rather than on the account. Nothing about
who looked at what needs to reach the server, and once it is there it is in
backups and in the audit log for ever. Signing out forgets it, or the next
person to sign in inherits what the last one agreed to look at.

Acknowledging a folder reveals what is inside it. One level, which is the
structure that exists — photographs hang off a gallery — and the alternative is
being asked two hundred times by the contents of a folder you just opened,
which is the version somebody turns off.

## The setting and the gesture

**One server setting**, in Libraries: *Allow folders and photos to be marked
sensitive.* Off by default.

Written as per-library in the proposal, and built as one switch, because there
is no per-library settings mechanism to hang it on — every rule of this kind in
LANcast is a server setting, and inventing a second shape for one boolean would
have cost more than it bought. The gesture is offered on picture items only, so
a film library is unaffected either way. Turning it off does not erase existing marks — it
stops new ones and stops the obscuring, which makes it recoverable rather than
destructive; a toggle that discards data the second time you press it is a
toggle nobody can experiment with.

**With it on**, a folder or photograph's context menu gains *Mark sensitive*, and
*Not sensitive* where the mark is its own.

Marking is available to anyone who can edit the library. Unmarking is the same
permission — a mark that only an admin can remove turns a courtesy into an
argument with the software.

## The three questions, answered

Answered by Chris on 2026-08-30.

1. **Does the folder's own name show?** Yes. The recommendation was to hide it;
   the answer was to keep it, and keeping it is right for the reason the
   recommendation missed — the person who marked a folder knows what is in it,
   and a grid of identical unnamed rectangles makes them hunt for their own
   folder. The tile shows the name and the word *Sensitive*.

2. **One photo, or only folders?** Both. An individual photograph can be marked
   when the setting is on, not only the folder around it.

3. **Where does it apply?** Everywhere a thumbnail is drawn — the home page,
   the library grid, and search. This is what decision 1 was for; no screen
   opts in.

The directive the three add up to, in Chris's words: *if the option is enabled
in settings, and a folder or photo is marked sensitive, restrict its view until
acknowledged of its nature.*

## Amendment, 2026-08-31 — after using it

Two things were wrong in practice, and both were wrong in the same direction:
the design treated acknowledgement as a fact about the *item* when it is a fact
about *where you are standing*.

**A cover could be lifted anywhere, and stayed lifted.** Accepting a folder
uncovered it on every surface for the rest of the session — the home page
included. So the screen most likely to have somebody else glancing at it was
also the screen where one click uncovered the folder, permanently. That is the
exact scenario the feature exists to prevent, arrived at through the feature.

Acknowledgement is now scoped to the surface. **Two surfaces may lift a cover:
the picture library's own grid, and a folder's own page.** Home, the shelves,
the hero, search and collections show marked content covered and offer no way to
uncover it — pressing does nothing, and the tile does not invite the press.
Leaving the pictures forgets what was accepted, so returning later finds it
covered again.

The default is that a cover may *not* be lifted, and a surface opts in. That way
a screen added next year is safe by saying nothing, and the available mistake is
forgetting to permit — visible and harmless — rather than forgetting to forbid.

**Only folders can be marked.** Single photographs could be, and it produced
content that was covered everywhere and viewable nowhere: the only places a
cover may be lifted are the library grid and a folder, so a loose marked photo
had nowhere it could be seen. A person with a photograph to protect puts it in
a folder and marks the folder, which is something they can do and the software
cannot do for them.

Refused by the server, not merely hidden in the menu — the menu is not the only
way to reach the endpoint. **Unmarking anything is still allowed**, so a photo
marked before this rule can be cleared; refusing to let somebody undo a mark
because the mark should not have been possible is how data becomes permanent by
accident.

## What it cost

Two columns and a migration (revision 34), a recompute in the store, one
endpoint, a recompute call at the end of a scan, a server setting, a
context-menu entry, and a session store in the client.

Three things the estimate got wrong, all in the same direction — the parts that
looked free were the parts with the decisions in them:

- **Two columns, not one.** "Somebody marked this" and "this should be covered"
  are different facts, and only the first is a decision. One column loses the
  difference exactly when it matters: unmarking a folder would silently clear a
  photograph inside it that had been marked on its own.
- **A recompute, not ancestry resolution in the query.** Resolving on read means
  touching every item query in the project. Resolving on write means being right
  about ordering, and three orderings defeat it — an item is inserted before it
  is given a parent, a folder can be marked before the scan that fills it, and a
  rescan can move a file between folders. A whole-library recompute at the end of
  a scan cannot be stale for a reason nobody anticipated, and it costs one
  `COUNT` on a library with no marks, which is every library by default.
- **Scanner work after all.** That recompute has to be called from somewhere.

The integration test that matters: **a rescan does not clear a mark**, with the
same standing as the locked-fields test.

## What this does not change

Nothing about playback, providers, matching or scanning behaviour. An unmarked
library performs one extra `COUNT` per scan and is otherwise untouched, and the
two columns are omitted from the JSON when false.
