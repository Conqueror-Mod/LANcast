# ADR 0059 — Telling the machine who somebody is not

**Status:** accepted
**Date:** 2026-09-03
**Supersedes:** nothing. Extends ADR 0052 (face grouping).

## Context

Face grouping could be told who somebody **is**. Naming a group is an edit, it
locks, and a re-cluster may add faces to a named person but may never rename or
dissolve one — the locked-fields rule applied to identity, and the reason
naming is safe to do.

There was no way to say the opposite. A group could contain a face that is
plainly somebody else, and the only controls on the screen were *name this
group* and *accept this near-miss*: both ways of agreeing with the machine.
Reported directly — dozens of photographs of one person appearing under a
different person's name, with nothing to do about it.

`SameFaceCosine` is deliberately biased against merging, because attaching
somebody's face to somebody else's name is much worse than leaving a face on
its own. That bias reduces how often this happens and cannot remove it: a
threshold is a single number applied to every face in the library, and some
faces are genuinely ambiguous to any number.

### Why detaching is not the answer

The obvious implementation is to set `face.cluster_id` back to `NULL`.

It fixes the screen and nothing else. The embedding is unchanged, so the next
pass computes the same similarity against the same centroid and puts the face
straight back where it was taken from. The correction survives until somebody
runs Find faces again, at which point it silently disappears — and a correction
that a re-cluster undoes is not a correction, it is a delay.

This is the same failure the naming rule already prevents, arriving from the
other direction. LANcast exists partly because the software it replaces
re-litigates decisions on every scan.

## Decision

**A refusal is stored, and it outranks similarity.**

A new table, `face_rejection(face_id, cluster_id)`. Clustering consults it
before scoring: a face is never placed in a group it has been refused from,
whatever the cosine says. Similarity is evidence; this is testimony; where they
disagree the person wins.

Three properties follow, and each was a decision:

**Keyed on the pair.** A face removed from one person is not removed from every
person. It is usually removed *because* it belongs to somebody else, so barring
it from the whole library would replace one wrong answer with a worse one — a
face that can never be grouped at all.

**The face is left ungrouped, not moved.** Where it does belong is a question
the clustering can answer on its own once it is no longer permitted the wrong
answer. Choosing for it would substitute one unrequested decision for another.

**Dismissing a suggestion is the same fact, written per face.** A suggestion is
an *unnamed* group, and a re-cluster may dissolve it and scatter its faces into
new groups with new ids. A refusal stored against the group would vanish with
the group, and the same faces would be offered again under a different number.
Stored against the faces, the answer survives regrouping — which is the only
way "I have already said no to this" can mean anything.

A group holding any refused face is not offered again, whole. A suggestion is
offered as a group and accepted as a group, so re-offering a partly-refused one
asks somebody to accept a face they have already declined.

### Lifetime

`ON DELETE CASCADE` on both sides. When the photograph goes, the rejection is
about nothing. When an unnamed cluster is dissolved, the group the rejection
referred to no longer exists.

Named clusters are never dissolved, so **a refusal from a person is permanent**
— which is the case the table was built for, and the one the report was about.

## Consequences

Removing a face is now a correction rather than a tidy-up, with the same
standing as a name. It survives a re-cluster, and the test that proves it
(`TestARejectedFaceIsNotRegroupedByTheNextPass`) is the one that fails if this
is ever quietly reduced back to a detach.

Suggestion lists get shorter with use. Previously the near-misses that were
genuinely somebody else were exactly the ones that stayed near, so they were
re-offered on every visit, and a question that returns after being answered
reads as a broken feature rather than a careful one.

The cost is one row per correction a human actually makes, and one query per
clustering pass. Both are negligible against a pass that compares every face
with every centroid.

### What this does not do

It does not merge two named groups, split a group in half, or move a face from
one person to another in a single action. Each of those is a larger question
about what a group *is*, and each is reachable today by removing and re-naming.
Adding them before anybody has asked would be designing against an imagined
complaint rather than the one that was made.

It also does not learn. A refusal is a fact about one face and one person, not
a signal fed back into the threshold. Adjusting `SameFaceCosine` from
corrections would make the model's behaviour depend on the order in which
somebody happened to look at their photographs, and would put the number that
governs whose face goes under whose name outside anybody's view.
