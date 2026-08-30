/*
 * Whether the browser is actually offering a choice of audio output.
 *
 * Chromium will not reveal attached audio hardware to a page without media
 * permission — the list is a fingerprint, so this is a privacy measure and not
 * a fault. What it returns instead is a single entry with an **empty
 * deviceId**, standing for "system default".
 *
 * That single entry is what a numbered fallback name turns into "Output 1", and
 * it is why the picker looked like it had found one device rather than none.
 * Selecting it does nothing: an empty id is what `setSinkId` already uses to
 * mean the default.
 */

export interface AudioOutput {
  id: string;
  label: string;
}

/**
 * outputsWithheld reports that the browser is hiding the real list.
 *
 * The tell is an id rather than a label. A device with a name but no id cannot
 * be routed to, and one with an id and no name can — so the presence of a real
 * `deviceId` is the thing that separates "unnamed" from "not there", and it is
 * the question this control actually cares about.
 */
export function outputsWithheld(devices: AudioOutput[]): boolean {
  if (devices.length === 0) return false;
  return devices.every((d) => d.id === "");
}

/**
 * routableOutputs is what a person can actually pick between.
 *
 * The empty-id placeholder is dropped rather than shown, because "Auto select
 * device" is already the option that means the same thing — offering both is
 * one choice wearing two names.
 */
export function routableOutputs(devices: AudioOutput[]): AudioOutput[] {
  return devices.filter((d) => d.id !== "");
}
