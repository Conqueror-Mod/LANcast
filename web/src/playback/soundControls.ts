/*
 * When the sound controls are offered, in one place.
 *
 * Two surfaces show them: the rows in the playback settings panel, and the
 * quick toggles in the player's control bar. Two copies of "is this track
 * mono" would drift, and the result would be a bar offering dialogue boost
 * on a file whose panel does not, or the other way round.
 */
import type { MediaStream } from "@/api/types";
import { DIALOGUE_LEVELS } from "./prefs";

/**
 * Whether dialogue boost can do anything for the track that is playing.
 *
 * Mono has no dialogue to separate from anything. This is the probe's view of
 * the chosen track (the file's default when none is chosen); the client makes
 * the final call from what mpv is actually decoding, so this only decides
 * whether to offer the control.
 */
export function canBoostDialogue(
  audioTracks: Pick<MediaStream, "index" | "channels" | "default">[],
  audioIndex: number | null,
): boolean {
  const current =
    audioIndex ?? (audioTracks.find((t) => t.default) ?? audioTracks[0])?.index;
  return audioTracks.find((t) => t.index === current)?.channels !== 1;
}

/**
 * Music's night mode, offered from the probe's channel count
 * (PlaybackState.musicChannels; 0 when not music, or not known). The same rule
 * the graph applies (elementAudio.ts, fxApplies), so a control is never shown
 * that the graph would then ignore.
 */
export function canNightMusic(channels: number): boolean {
  return channels === 1 || channels === 2;
}

/** The dialogue level after this one: Off, Low, High, then back to Off. */
export function nextDialogueLevel(level: number): number {
  const n = DIALOGUE_LEVELS.length;
  const i = Number.isInteger(level) && level >= 0 && level < n ? level : 0;
  return (i + 1) % n;
}
