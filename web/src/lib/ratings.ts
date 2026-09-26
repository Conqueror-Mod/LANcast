/*
 * The ceilings offered, in order, and there is exactly one list.
 *
 * A short list rather than every label internal/rating can place. The full
 * table carries six national systems so that *items* from any of them can be
 * judged; offering all of them would ask a household to choose between "15"
 * and "TV-14" as though the difference meant something to them. These are the
 * rungs somebody actually thinks in, and an item rated in another system is
 * still placed against whichever one is chosen.
 *
 * Shared rather than duplicated because
 * [ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §6 says a share's limit comes from "the same rungs an account ceiling
 * offers". Two lists that could drift apart would make that sentence quietly
 * false — a household would set "PG-13" for a friend and "PG-13" for a child
 * and have no reason to expect the two to mean different things.
 */
export const RATING_RUNGS = ["G", "PG", "PG-13", "TV-14", "R"];
