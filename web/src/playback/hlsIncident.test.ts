import { describe, it, expect, beforeEach } from "vitest";
import {
  describeIncident,
  forgetHLSIncidents,
  lastHLSIncident,
  noteHLSIncident,
  noteHLSPlaylistServed,
  readIncident,
  type HLSIncident,
} from "./hlsIncident";

/*
 * The message is the point.
 *
 * Every other field here already existed in some form; `MediaError.message` is
 * the one the player was throwing away, and it is the one that separates
 * hypotheses. Chasing this fallback meant eliminating the playlist, the
 * encoder, the declared level, the MIME types, the URL rewrite and ffmpeg's
 * throughput one at a time from outside the application — while the element
 * held a string saying which of them it was.
 *
 * So these assert that the string survives, including when it is empty, and
 * that the state travels with it: "never got metadata" and "had six seconds of
 * picture and then stopped" are different faults wearing the same code 4.
 */

function incident(over: Partial<HLSIncident> = {}): HLSIncident {
  return {
    code: 4,
    message: "PipelineStatus::DEMUXER_ERROR_COULD_NOT_PARSE",
    readyState: 0,
    networkState: 3,
    buffered: 0,
    at: 1024,
    clock: 1000,
    itemID: 7058,
    title: "The Fifth Element",
    ...over,
  };
}

beforeEach(() => forgetHLSIncidents());

describe("recording why segments were abandoned", () => {
  it("keeps the message the element gave", () => {
    noteHLSIncident(incident());
    expect(lastHLSIncident()?.message).toBe(
      "PipelineStatus::DEMUXER_ERROR_COULD_NOT_PARSE",
    );
  });

  // One incident, because the question is always about this fallback. A list
  // would also grow without bound in a player that runs for days.
  it("keeps only the most recent", () => {
    noteHLSIncident(incident({ at: 100 }));
    noteHLSIncident(incident({ at: 200 }));
    expect(lastHLSIncident()?.at).toBe(200);
  });

  /*
   * The probe's answer arrives later and belongs to one incident.
   *
   * The fallback does not wait for it — the viewer should not sit through a
   * question — so a late answer about a previous failure could land on the
   * current one and describe the wrong event.
   */
  it("attaches the probe answer to the incident it belongs to", () => {
    noteHLSIncident(incident({ clock: 1000 }));
    noteHLSPlaylistServed(1000, true);
    expect(lastHLSIncident()?.playlistServed).toBe(true);
  });

  it("discards a probe answer about an incident that has been replaced", () => {
    noteHLSIncident(incident({ clock: 1000 }));
    noteHLSIncident(incident({ clock: 2000 }));
    noteHLSPlaylistServed(1000, true);

    expect(
      lastHLSIncident()?.playlistServed,
      "an answer about the previous failure was written onto this one",
    ).toBeUndefined();
  });
});

describe("reading the element", () => {
  function element(over: Record<string, unknown>): HTMLVideoElement {
    return {
      error: { code: 4, message: "DEMUXER_ERROR_NO_SUPPORTED_STREAMS" },
      readyState: 1,
      networkState: 3,
      buffered: { length: 1, end: () => 6.5 },
      ...over,
    } as unknown as HTMLVideoElement;
  }

  it("takes the code, the message and the state together", () => {
    const i = readIncident(element({}), 481, 99, 37106, "An Episode");
    expect(i.code).toBe(4);
    expect(i.message).toBe("DEMUXER_ERROR_NO_SUPPORTED_STREAMS");
    expect(i.readyState).toBe(1);
    expect(i.networkState).toBe(3);
    expect(i.buffered).toBe(6.5);
    expect(i.at).toBe(481);
  });

  // An engine that reports no message is a fact worth seeing. Substituting a
  // guess here would be the same mistake as the text that used to say the
  // server was probably busy.
  it("records an absent message as absent rather than inventing one", () => {
    const i = readIncident(element({ error: { code: 4, message: "" } }), 0, 1, 37106, "An Episode");
    expect(i.message).toBe("");
    expect(describeIncident(i)[0]).toContain("no message");
  });

  it("survives an element with no error object at all", () => {
    const i = readIncident(element({ error: null }), 0, 1, 37106, "An Episode");
    expect(i.code).toBe(0);
    expect(i.message).toBe("");
  });

  // An element that has given up can throw on buffered rather than answering
  // an empty range, and the rest of the reading is still worth having.
  it("survives an element that throws when asked what it buffered", () => {
    const v = element({
      buffered: {
        length: 1,
        end: () => {
          throw new Error("InvalidStateError");
        },
      },
    });
    expect(() => readIncident(v, 0, 1, 37106, "An Episode")).not.toThrow();
    expect(readIncident(v, 0, 1, 37106, "An Episode").buffered).toBe(0);
  });

  it("reports nothing buffered as zero rather than as missing", () => {
    const i = readIncident(
      element({ buffered: { length: 0, end: () => 0 } }),
      0,
      1,
      37106,
      "An Episode",
    );
    expect(i.buffered).toBe(0);
  });
});

describe("what it reads like on screen", () => {
  it("leads with the message, because that is the finding", () => {
    const [first] = describeIncident(incident());
    expect(first).toContain("DEMUXER_ERROR_COULD_NOT_PARSE");
    expect(first).toContain("code 4");
  });

  /*
   * The state line separates the two faults that share code 4.
   *
   * `ready 0 / buffered 0` is an element that never got metadata — the playlist
   * itself was unreadable. `ready 4 / buffered 6.0` is one that had picture and
   * stopped anyway, which is a different bug entirely and was the shape the
   * server log implied.
   */
  it("says whether any picture had arrived", () => {
    const none = describeIncident(incident())[1];
    expect(none).toContain("ready 0");
    expect(none).toContain("buffered 0.0s");

    const some = describeIncident(
      incident({ readyState: 4, buffered: 6.02 }),
    )[1];
    expect(some).toContain("ready 4");
    expect(some).toContain("buffered 6.0s");
  });

  it("says whether the playlist endpoint answered, including when unknown", () => {
    expect(describeIncident(incident())[1]).toContain("playlist ?");
    // Asked and could not tell is its own answer, and not the same as either
    // of the other two — it is the state the probe was added to produce.
    expect(
      describeIncident(incident({ playlistServed: null }))[1],
    ).toContain("playlist unknown");
    expect(
      describeIncident(incident({ playlistServed: true }))[1],
    ).toContain("playlist served");
    expect(
      describeIncident(incident({ playlistServed: false }))[1],
    ).toContain("playlist refused");
  });
});

/*
 * The record says what it is about.
 *
 * It outlives the playback it describes on purpose, so a panel opened minutes
 * later still finds it — and that is exactly why it has to name its subject. A
 * film and an episode both fell back near 1300 seconds within a minute of each
 * other, and the line could not say which of them it meant.
 */
describe("which title it happened to", () => {
  it("names the title first, before the numbers", () => {
    const [first] = describeIncident(incident());
    expect(first.startsWith("The Fifth Element — ")).toBe(true);
  });

  // A title is not always in hand — a fallback can happen before the item
  // payload has arrived — and an id is still better than an anonymous line.
  it("falls back to the item id rather than saying nothing", () => {
    const [first] = describeIncident(incident({ title: "" }));
    expect(first).toContain("item 7058");
  });

  it("carries the item through the reading", () => {
    const v = {
      error: { code: 4, message: "x" },
      readyState: 4,
      networkState: 3,
      buffered: { length: 0, end: () => 0 },
    } as unknown as HTMLVideoElement;

    const i = readIncident(v, 1302, 5, 7058, "The Fifth Element");
    expect(i.itemID).toBe(7058);
    expect(i.title).toBe("The Fifth Element");
  });
});
