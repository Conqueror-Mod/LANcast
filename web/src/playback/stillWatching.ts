/*
 * "Are you still watching."
 *
 * Nothing stops playback when nobody is there. Autoplay walks a season, each
 * episode marks itself watched at the end, and a server left running overnight
 * has spent hours of transcode and rewritten the one piece of state people
 * actually care about — where they had got to.
 *
 * # Why this is not the idle reaper
 *
 * The server already kills a session nobody is *reading*. An unattended stream
 * is being read perfectly well: the television is on, the bytes are flowing,
 * and the only thing missing is a person. No amount of server-side idleness
 * detection can see that, because from the server's side an empty room and a
 * full one are identical.
 *
 * # What counts as attention
 *
 * Not "any interaction", which is the obvious rule and the wrong one: it
 * punishes the person watching a two-hour film properly, who touches nothing
 * for two hours and is the most attentive viewer in the house. What is being
 * detected is *unattended autoplay* — the queue advancing on its own, over and
 * over, with nobody choosing any of it.
 *
 * So the counter is per **automatic advance**, and any deliberate act resets
 * it. Choosing the next episode yourself is attention. Seeking, pausing and
 * resuming are attention. Sitting still through a film is not inattention.
 *
 * # Why the count alone, and no clock
 *
 * There was a clock: three advances *and* two hours. It was there so that a run
 * of short cartoons could not ask after eighteen minutes, and it made the
 * feature untestable in practice and nearly unreachable in use — three episodes
 * of an ordinary drama clear the count long before they clear the clock, so the
 * prompt fired on almost nothing.
 *
 * The count is the honest signal on its own. Three things have played with
 * nobody choosing any of them, and the length of those three things says
 * nothing about whether a person is in the room. A short run of cartoons that
 * nobody chose is exactly as unattended as a long one.
 */

/** How many consecutive automatic advances before asking. */
export const UNATTENDED_ITEMS = 3;

/** What the player knows about the current unattended run. */
export type WatchRun = {
  /** Consecutive automatic advances since the last deliberate act. */
  autoAdvances: number;
  /** When the run began, or null when there is no run. */
  since: number | null;
};

export const NO_RUN: WatchRun = { autoAdvances: 0, since: null };

/**
 * advanced records the queue moving on by itself.
 *
 * `now` is passed rather than read so the rule is testable without faking a
 * clock, which is the same reason every other decision in this folder is a pure
 * function over its inputs. It no longer gates the prompt — `since` survives
 * only so the prompt can say how long it has been going.
 */
export function advanced(run: WatchRun, now: number): WatchRun {
  return {
    autoAdvances: run.autoAdvances + 1,
    since: run.since ?? now,
  };
}

/**
 * attended records a deliberate act and clears the run.
 *
 * Anything a person had to do on purpose counts: choosing something, seeking,
 * pausing, answering the prompt. Not `timeupdate`, which fires while a room is
 * empty, and not `waiting`, which is the network rather than a human.
 */
export function attended(): WatchRun {
  return NO_RUN;
}

/**
 * shouldAsk decides whether to stop and put the question.
 *
 * Three automatic advances, and nothing else. What keeps this from firing on an
 * attentive viewer is not a duration but the reset: the person who is there
 * chose something, or paused, or seeked, and any one of those has already
 * cleared the run.
 */
export function shouldAsk(run: WatchRun): boolean {
  return run.autoAdvances >= UNATTENDED_ITEMS;
}

/**
 * describeRun is what the prompt says it has been doing.
 *
 * Naming the reason matters more here than in most dialogs, because the honest
 * complaint about this feature in other players is that it appears to be
 * accusing you of something. "Three episodes have played" is a fact about the
 * machine; "are you still there?" is a question about the person.
 */
export function describeRun(run: WatchRun, now: number): string {
  const hours = run.since === null ? 0 : Math.floor((now - run.since) / 3_600_000);
  const items = `${run.autoAdvances} things have played automatically`;
  return hours > 0
    ? `${items} over about ${hours} hour${hours === 1 ? "" : "s"}.`
    : `${items}.`;
}
