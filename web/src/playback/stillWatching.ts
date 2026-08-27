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
 * # Why a count and a clock, not one or the other
 *
 * A count alone lets a run of six-minute cartoons play for half an hour before
 * asking, and a run of feature films ask after six hours. A clock alone
 * interrupts the person half way through a long film they chose. Together they
 * mean: several things have played, *and* it has been going a while, and
 * neither of those alone is enough.
 */

/** How many consecutive automatic advances before asking. */
export const UNATTENDED_ITEMS = 3;

/**
 * How long those advances have to have been running, in milliseconds.
 *
 * Two hours. Long enough that three short episodes back to back do not trigger
 * it — that is an evening, not an absence — and short enough that a television
 * left on overnight is caught within the first few hours rather than at dawn.
 */
export const UNATTENDED_MS = 2 * 60 * 60 * 1000;

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
 * function over its inputs.
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
 * Both conditions, deliberately. Either alone is a rule that interrupts
 * somebody who is plainly there, and an "are you still watching" prompt that
 * fires on an attentive viewer is worse than none — it trains people to reach
 * for the remote to dismiss it, which is exactly the reflex the feature is
 * trying to detect the absence of.
 */
export function shouldAsk(run: WatchRun, now: number): boolean {
  if (run.since === null) return false;
  return run.autoAdvances >= UNATTENDED_ITEMS && now - run.since >= UNATTENDED_MS;
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
