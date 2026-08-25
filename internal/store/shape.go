package store

/*
 * Whether a row's *shape* is settled, as distinct from its identity (ADR 0041).
 *
 * These are two different questions and conflating them cost a real afternoon.
 * A file that lost its `EP1` marker parsed as a film, landed top-level with no
 * parent in a shows library, and was then confirmed against a same-named film
 * — which locked a wrong identity onto a row that was in the wrong place to
 * begin with. Locking is meant to stop a rescan re-litigating a decision; here
 * it stopped a rescan *fixing* one, and turned a two-minute filename correction
 * into a dead end.
 *
 * The remedy ADR 0041 chose is not reparenting — a file in the wrong place is
 * corrected on disk, where the wrongness lives. What it asks of the code is
 * only this: **do not lock a row whose shape is still wrong.** Either the shape
 * is settled first, or the row stays reviewable.
 */

/*
 * ShapeUnsettled reports whether an item sits in a place that contradicts its
 * library.
 *
 * Deliberately narrow. It is *not* "a shows library contains a film", which is
 * ordinary and legitimate — an extras folder, a documentary shipped beside a
 * series, the case ADR 0041 calls Case 2 and resolves by moving the file to a
 * film library. `shapecheck.go` already declines to cry wolf about that at the
 * library level, for the reason a check that cries wolf gets ignored.
 *
 * What this catches is the narrower thing that actually traps: a *parentless*
 * film row in a shows library. A film with a parent has been placed
 * deliberately; a film with none, in a library declared to hold shows, is the
 * exact shape a lost episode marker produces.
 *
 * Pure, and takes the library's kind rather than a library, so the rule is a
 * table test rather than a fixture.
 */
func ShapeUnsettled(libraryKind string, it Item) bool {
	return libraryKind == "show" && it.Kind == "movie" && it.ParentID == nil
}
