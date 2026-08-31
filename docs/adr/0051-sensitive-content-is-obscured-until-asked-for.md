# ADR 0051 — Sensitive content is obscured until asked for

**Status:** proposed
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

### 3. The server does not serve the thumbnail

A CSS blur is a picture of a privacy feature. The bytes arrive, the element is
in the DOM, and anything that turns off styles — a stuck stylesheet, a
screen-reader view, a devtools panel, a slow first paint — shows the image the
mark exists to not show. The failure mode is the image appearing for a moment
on a page somebody else is looking at, which is the whole scenario.

So `/api/artwork` returns the placeholder for a sensitive item unless the
request carries an acknowledgement. The client blurs as well, because it looks
better than an empty tile, but the blur is decoration over an image that was
never sent. Server owns truth; the client is thin.

## Acknowledgement

Accepting is per person, per device, and lasts the session. Closing the app
re-obscures.

A permanent "yes I know" is easy to build and defeats the purpose within a
month: the folder stops being marked in any way the person notices, and the
next time somebody else is in the room it is just a folder. Session scope keeps
the acknowledgement close to the act of choosing to look.

It is stored client-side per device rather than on the account. Nothing about
who looked at what needs to reach the server, and once it is there it is in
backups and in the audit log for ever.

## The setting and the gesture

**Per library, in that picture library's settings:** *Allow marking folders as
sensitive.* Off by default. Turning it off does not erase existing marks — it
stops new ones and stops the obscuring, which makes it recoverable rather than
destructive; a toggle that discards data the second time you press it is a
toggle nobody can experiment with.

**With it on**, a folder's context menu gains *Mark sensitive* / *Unmark*.

Marking is available to anyone who can edit the library. Unmarking is the same
permission — a mark that only an admin can remove turns a courtesy into an
argument with the software.

## Open questions for Chris

1. **Does the folder's own name show?** A blurred tile labelled with the folder
   name gives the game away for any folder named after what is in it. Options:
   show the name (simplest, leaks), replace with *Sensitive* until accepted
   (safest, makes it hard to find your own folder), or make the name part of
   what marking hides, as a second tick. **Recommendation: hide the name too,
   as part of the mark** — a folder you marked is a folder you know how to find.

2. **One photo, or only folders?** The gesture as described is folder-level.
   Photo-level marking is the same field and no extra schema, but a lot more
   surface for a case that may not exist. **Recommendation: folders only for
   now**, and add photos if a real one turns up — the same rule ADR 0049 used
   for alternate cuts.

3. **Does a sensitive folder appear in search?** Blurred and named, blurred and
   unnamed, or not at all. **Recommendation: blurred, using whatever answer
   question 1 gets** — excluding it from search makes the library lie about what
   it contains, and the person searching is usually the person who marked it.

## Cost

Small. One nullable column and a migration, ancestry resolution in the item
query, one guard in the artwork handler, a context-menu entry, a library
setting, and a session store in the client. No provider work, no scanner work,
no new endpoint.

The integration test that matters: **a rescan does not clear a mark**, in the
same file and with the same standing as the locked-fields test.
