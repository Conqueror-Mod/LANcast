/*
 * Why the segmented path was given up on.
 *
 * # What this cost
 *
 * A film falls back from segments to the progressive stream in under a second,
 * and the only thing anybody could say about it afterwards was that it had
 * happened. Chasing it meant eliminating, one at a time and from outside the
 * application: the playlist's content, the encoder, the level the encoder
 * declares, the MIME types, the URL rewrite, whether ffmpeg can keep up, and
 * whether ffmpeg ever publishes a playlist with nothing in it. Every one of
 * those came back clean — the same output plays to `readyState 4` in two
 * Chromium engines when handed to them directly.
 *
 * The element was holding the answer the entire time. `MediaError.message` is
 * a real string in Chromium — `DEMUXER_ERROR_COULD_NOT_PARSE`,
 * `DEMUXER_ERROR_NO_SUPPORTED_STREAMS`, or empty — and the code threw it away,
 * keeping only `code === 4`. Which is the one fact that cannot distinguish
 * anything, because a `<video>` element reports 4 for "this is not media" and
 * for "I could not fetch the media" alike.
 *
 * This is the same instrument the live path already has, arrived at the same
 * way. A channel that would not start produced three careful theories and no
 * progress until a line that named the transport, whether metadata arrived and
 * what refused to play answered it in one reading.
 *
 * # Why it is kept rather than logged
 *
 * The desktop client has no console anybody can reach — developer tools are off
 * by default and open in a window of their own — so a `console.log` here is a
 * message to nobody. Held in memory and rendered in the playback statistics
 * panel, it can be read by whoever is in front of the television when it
 * happens, which is the person who knows what they were doing at the time.
 *
 * One incident, not a list. The question is always "why did *this* fall back",
 * and a growing history would be a memory leak in a player that may run for
 * days.
 */

/** What the element said, and what state it was in when it said it. */
export interface HLSIncident {
  /** MediaError.code; 4 is MEDIA_ERR_SRC_NOT_SUPPORTED. */
  code: number;
  /**
   * MediaError.message.
   *
   * The load-bearing field, and the one that did not exist before. Chromium
   * puts a `PipelineStatus` name in here; other engines leave it empty, which
   * is itself worth seeing rather than guessing at.
   */
  message: string;
  /** readyState at the moment of failure. 0 means it never got metadata. */
  readyState: number;
  /** networkState. 3 is NETWORK_NO_SOURCE — given up, not still trying. */
  networkState: number;
  /** Seconds buffered when it failed. Above zero means media had arrived. */
  buffered: number;
  /** Where playback was being started from, seconds. */
  at: number;
  /** Wall clock, ms. */
  clock: number;
  /**
   * Whether the playlist endpoint answered with a playlist when asked again.
   *
   * Three states, because the probe has three answers and flattening them would
   * throw away the distinction the probe exists to make. Undefined is "not back
   * yet" — the fallback does not wait for it, since the viewer should not sit
   * through a question. `false` is "the server refused", which means the
   * element's complaint says nothing about this engine. `null` is "asked and
   * could not tell", which is not the same as either and is worth seeing as
   * itself.
   */
  playlistServed?: boolean | null;
}

let last: HLSIncident | null = null;

/** noteHLSIncident records why the segmented path was abandoned. */
export function noteHLSIncident(i: HLSIncident): void {
  last = i;
}

/**
 * noteHLSPlaylistServed attaches the probe's answer to the incident it belongs
 * to.
 *
 * Ignored if another incident has happened in the meantime: a late answer about
 * a previous failure, written over the current one, would be worse than no
 * answer at all.
 */
export function noteHLSPlaylistServed(
  clock: number,
  served: boolean | null,
): void {
  if (last && last.clock === clock) last.playlistServed = served;
}

/** lastHLSIncident returns the most recent fallback, or null. */
export function lastHLSIncident(): HLSIncident | null {
  return last;
}

/** forgetHLSIncidents clears the record. For tests, and for a fresh item. */
export function forgetHLSIncidents(): void {
  last = null;
}

/**
 * describeIncident renders it for the statistics panel.
 *
 * Two lines: what the element said, and what state it was in. The state half
 * matters as much as the message, because "never got metadata" and "had six
 * seconds of picture and then stopped" are different faults that produce the
 * same code.
 */
export function describeIncident(i: HLSIncident): string[] {
  const served =
    i.playlistServed === undefined
      ? "playlist ?"
      : i.playlistServed === null
        ? "playlist unknown"
        : i.playlistServed
          ? "playlist served"
          : "playlist refused";

  return [
    `Segments abandoned at ${i.at.toFixed(0)}s · code ${i.code} · ${
      i.message || "no message"
    }`,
    `ready ${i.readyState} · network ${i.networkState} · buffered ${i.buffered.toFixed(
      1,
    )}s · ${served}`,
  ];
}

/**
 * readIncident pulls the fields out of a media element.
 *
 * Here rather than at the call site so the reading is testable: jsdom performs
 * no media, and the half that has been wrong before in this project is always
 * the reading rather than the arithmetic.
 */
export function readIncident(
  v: HTMLVideoElement,
  at: number,
  clock: number,
): HLSIncident {
  const e = v.error;
  let buffered = 0;
  try {
    if (v.buffered && v.buffered.length > 0) {
      buffered = v.buffered.end(v.buffered.length - 1);
    }
  } catch {
    // An element that has given up can throw on buffered rather than answering
    // an empty range. That is not worth losing the rest of the reading over.
  }
  return {
    code: e?.code ?? 0,
    message: e?.message ?? "",
    readyState: v.readyState,
    networkState: v.networkState,
    buffered,
    at,
    clock,
  };
}
