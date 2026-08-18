import type { SubtitleTrack } from "@/api/types";

/*
 * Whether the player shows its own subtitle button.
 *
 * The button is a one-click toggle — it cycles Off → each usable track → Off,
 * the same action as the `[` and `]` keys — and that is why it survives the
 * Playback settings panel: turning subtitles on mid-scene is a transport
 * action, not a settings change.
 *
 * What it cannot do is offer a choice that does not exist. `cycleSub` builds
 * its order as `[null, ...available]`, so with nothing available the list is
 * one element long and the cycle returns to where it started: the button
 * renders, the click lands, and *nothing happens at all* — no menu, no message,
 * no change of state. On any file without subtitles it is a dead control, which
 * is how it gets read as a leftover from before the settings panel existed.
 *
 * Gated on `available` rather than on the track count, because the two differ
 * exactly where it matters. A bitmap track that cannot become WebVTT is a real
 * track and is listed in the menu with its reason, but `cycleSub` skips it — so
 * a file carrying only bitmap subtitles has tracks to show and none to cycle,
 * and counting them would put the dead button back.
 *
 * The pop-out window asks a different question and keeps its own rule: its
 * button opens the menu rather than cycling, and the menu has something to say
 * about a track it cannot play.
 */
export function showsSubtitleButton(
  isAudio: boolean,
  subtitles: SubtitleTrack[],
): boolean {
  if (isAudio) return false;
  return subtitles.some((t) => t.available);
}
