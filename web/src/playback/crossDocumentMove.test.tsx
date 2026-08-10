/*
 * The acceptance test ADR 0029 asks for, and it is deliberately the first thing
 * built rather than the last.
 *
 * Document picture-in-picture requires the media element to move into another
 * document. That is the one thing this architecture has always avoided:
 * PlaybackProvider keeps a single <video> alive above the router and moves it
 * between the full and docked surfaces with CSS, because re-parenting it in
 * React unmounts it and an unmounted element stops the sound. CSS cannot reach
 * another document, and a portal remounts its children when its container
 * changes, so the move has to be imperative and outside reconciliation.
 *
 * That is safe only for as long as React never needs to touch the node's
 * position among its siblings. These tests find out whether that holds, using
 * the sibling structure PlaybackProvider actually renders.
 *
 * What this can prove: whether React survives, and whether the element is
 * still the same node in the other document afterwards. What it cannot prove:
 * that audio keeps playing across the move — jsdom does not decode media, and
 * that part is the browser's specified behaviour. The React reconciliation risk
 * is the part that is ours, and it is the part under test here.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

/*
 * A faithful reduction of what PlaybackProvider renders around the media
 * element (see PlaybackProvider.tsx, the return at the bottom of the file):
 *
 *   <div ref={containerRef}>
 *     {isAudio && <div className="playback__cover">…</div>}   <- conditional SIBLING, before
 *     <video ref={videoRef}>
 *       {activeSub && <track key={…} />}                      <- conditional CHILD
 *     </video>
 *   </div>
 *
 * Both conditionals are the hazard. The cover mounts and unmounts as the player
 * moves between a film and a track; the track remounts on every subtitle change
 * and on every transcode seek. Either one makes React operate on the video's
 * parent while the video is somewhere else entirely.
 */
function Surface({
  showCover,
  showTrack,
  label,
}: {
  showCover: boolean;
  showTrack: boolean;
  label: string;
}): ReactNode {
  return (
    <div id="container">
      {showCover && <div id="cover">{label}</div>}
      <video id="media" data-label={label}>
        {showTrack && <track kind="subtitles" src="/cues.vtt" />}
      </video>
    </div>
  );
}

let host: HTMLDivElement;
let root: Root;
let pipDoc: Document;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  // Stands in for the picture-in-picture window's document. jsdom's second
  // document behaves the same way for adoption and for the DOM operations
  // React performs; what it does not have is a media stack, which is why the
  // header above is careful about what these tests do and do not prove.
  pipDoc = document.implementation.createHTMLDocument("pip");
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(props: Parameters<typeof Surface>[0]) {
  act(() => {
    root.render(<Surface {...props} />);
  });
}

/** Move the media element into the other document, as the pop-out would. */
function popOut(): HTMLVideoElement {
  const media = host.querySelector<HTMLVideoElement>("#media");
  if (!media) throw new Error("no media element to move");
  act(() => {
    pipDoc.body.append(pipDoc.adoptNode(media));
  });
  return media;
}

describe("moving the media element into another document", () => {
  it("adopts the same node rather than a copy", () => {
    render({ showCover: false, showTrack: false, label: "a" });
    const before = host.querySelector("#media");
    const moved = popOut();

    // Identity is the whole point. A copy would be a new element with no
    // buffered data and no playback position — the thing the CSS-move approach
    // exists to avoid, arrived at by another route.
    expect(moved).toBe(before);
    expect(moved.ownerDocument).toBe(pipDoc);
    expect(host.querySelector("#media")).toBeNull();
  });

  it("survives a re-render that changes only the element's own props", () => {
    render({ showCover: false, showTrack: false, label: "a" });
    const moved = popOut();

    render({ showCover: false, showTrack: false, label: "b" });

    // React updates attributes in place, on whichever node it is holding — it
    // does not need the parent for this, so the element stays put and simply
    // takes the new value.
    expect(moved.dataset.label).toBe("b");
    expect(moved.ownerDocument).toBe(pipDoc);
  });

  it("survives a conditional CHILD mounting while it is away", () => {
    render({ showCover: false, showTrack: false, label: "a" });
    const moved = popOut();

    // A subtitle being switched on, or a transcode seek remounting the track
    // with a freshly offset src. The insert happens inside the video, which
    // travelled with it.
    render({ showCover: false, showTrack: true, label: "a" });

    expect(moved.querySelector("track")).not.toBeNull();
    expect(moved.ownerDocument).toBe(pipDoc);
  });

  /*
   * The one that fails, kept as a characterisation test because the constraint
   * it found is the whole result of this exercise.
   *
   * To place the cover before the video, React calls
   * container.insertBefore(cover, video) — and the video is no longer a child
   * of that container. The DOM throws NotFoundError, and it throws during the
   * commit phase, so it takes down the render pass that did it rather than
   * failing quietly in a corner.
   *
   * PlaybackProvider renders exactly this shape today: the cover block is a
   * conditional sibling immediately before the media element. So the pop-out
   * cannot be built on the current markup, and this test is what says so.
   */
  it("CANNOT survive a conditional sibling mounting before it — the constraint", () => {
    render({ showCover: false, showTrack: false, label: "a" });
    popOut();

    expect(() =>
      render({ showCover: true, showTrack: false, label: "a" }),
    ).toThrow(/child can not be found in the parent/i);
  });

  it("returns the element to its original parent when the window closes", () => {
    render({ showCover: false, showTrack: false, label: "a" });
    const moved = popOut();

    const container = host.querySelector("#container");
    act(() => {
      container?.append(document.adoptNode(moved));
    });

    expect(moved.ownerDocument).toBe(document);
    expect(host.querySelector("#media")).toBe(moved);

    // And React must still be able to drive it afterwards, or closing the
    // pop-out leaves a player that renders but no longer responds.
    render({ showCover: false, showTrack: false, label: "c" });
    expect(moved.dataset.label).toBe("c");
  });
});

/*
 * The shape that satisfies the constraint above, and therefore the shape the
 * implementation has to adopt: the media element is the only child of a slot
 * whose children React never varies. Everything conditional — the cover, and in
 * the real provider the loading overlay and anything else that comes later —
 * lives outside the slot, as a sibling of the slot rather than of the element.
 *
 * Then React can mount and unmount as much as it likes around the player: the
 * only insert that could reference the media element as an anchor is one inside
 * the slot, and nothing else is ever in there.
 */
function SlottedSurface({
  showCover,
  showTrack,
  label,
}: {
  showCover: boolean;
  showTrack: boolean;
  label: string;
}): ReactNode {
  return (
    <div id="container">
      {showCover && <div id="cover">{label}</div>}
      <div id="slot">
        <video id="media" data-label={label}>
          {showTrack && <track kind="subtitles" src="/cues.vtt" />}
        </video>
      </div>
    </div>
  );
}

describe("the slot shape ADR 0029's implementation must use", () => {
  function renderSlotted(props: Parameters<typeof SlottedSurface>[0]) {
    act(() => {
      root.render(<SlottedSurface {...props} />);
    });
  }

  it("survives the sibling change that breaks the current shape", () => {
    renderSlotted({ showCover: false, showTrack: false, label: "a" });
    const moved = popOut();

    // The identical toggle that throws above.
    renderSlotted({ showCover: true, showTrack: false, label: "a" });

    expect(host.querySelector("#cover")).not.toBeNull();
    expect(moved.ownerDocument).toBe(pipDoc);
    expect(host.querySelector("#media")).toBeNull();
    // The empty slot stays behind, which is what the element comes home to.
    expect(host.querySelector("#slot")).not.toBeNull();
  });

  it("survives the cover unmounting again while the element is away", () => {
    renderSlotted({ showCover: true, showTrack: false, label: "a" });
    const moved = popOut();

    renderSlotted({ showCover: false, showTrack: false, label: "a" });

    expect(host.querySelector("#cover")).toBeNull();
    expect(moved.ownerDocument).toBe(pipDoc);
  });

  it("still updates the element's props and children while it is away", () => {
    renderSlotted({ showCover: false, showTrack: false, label: "a" });
    const moved = popOut();

    renderSlotted({ showCover: true, showTrack: true, label: "b" });

    // A subtitle switched on, a cover mounted and the element's own attributes
    // changed, all in one pass, with the element in another document.
    expect(moved.dataset.label).toBe("b");
    expect(moved.querySelector("track")).not.toBeNull();
    expect(moved.ownerDocument).toBe(pipDoc);
  });

  it("comes home to its slot and stays under React's control", () => {
    renderSlotted({ showCover: false, showTrack: false, label: "a" });
    const moved = popOut();
    renderSlotted({ showCover: true, showTrack: false, label: "a" });

    const slot = host.querySelector("#slot");
    act(() => {
      slot?.append(document.adoptNode(moved));
    });

    expect(host.querySelector("#slot #media")).toBe(moved);

    renderSlotted({ showCover: false, showTrack: false, label: "d" });
    expect(moved.dataset.label).toBe("d");
    expect(moved.isConnected).toBe(true);
  });
});
