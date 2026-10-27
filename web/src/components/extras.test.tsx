/*
 * Extras on a film's page (ADR 0098). Wiring only: which tiles appear, what a
 * press plays, and what an unplayable file says.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { FocusProvider } from "@/focus/FocusController";
import { ExtraPlayer, ExtrasRow } from "./Extras";
import type { Extra } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

const extras: Extra[] = [
  { key: "VHJhaWxlcnMvVC5tcDQ", kind: "trailer", title: "Teaser", bytes: 10, container: "mp4" },
  { key: "QlRTL00ubWt2", kind: "behind_the_scenes", title: "Making of", bytes: 20, container: "mkv" },
];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(node: React.ReactNode) {
  act(() => {
    root.render(<FocusProvider>{node}</FocusProvider>);
  });
}

describe("the extras row", () => {
  it("shows a tile per extra, named by kind and title", () => {
    render(<ExtrasRow itemID={7} extras={extras} onPlay={() => {}} />);
    const tiles = [...host.querySelectorAll(".extra-tile")].map((t) => t.textContent);
    expect(tiles).toEqual(["TrailerTeaser", "Behind the scenesMaking of"]);
  });

  it("is absent for a film with no extras", () => {
    render(<ExtrasRow itemID={7} extras={[]} onPlay={() => {}} />);
    expect(host.querySelector(".detail__extras")).toBeNull();
  });

  it("plays the extra pressed", () => {
    const played: string[] = [];
    render(<ExtrasRow itemID={7} extras={extras} onPlay={(x) => played.push(x.title)} />);
    act(() => {
      (host.querySelectorAll(".extra-tile")[1] as HTMLButtonElement).click();
    });
    expect(played).toEqual(["Making of"]);
  });
});

describe("the extra player", () => {
  it("plays the file by its key", () => {
    render(<ExtraPlayer itemID={7} extra={extras[0]} filmTitle="Alien" onClose={() => {}} />);
    const video = host.querySelector("video")!;
    expect(video.getAttribute("src")).toBe("/api/items/7/extras/VHJhaWxlcnMvVC5tcDQ");
  });

  it("says when the file cannot play here, with its format", () => {
    render(<ExtraPlayer itemID={7} extra={extras[1]} filmTitle="Alien" onClose={() => {}} />);
    act(() => {
      host.querySelector("video")!.dispatchEvent(new Event("error"));
    });
    expect(host.textContent).toContain("This MKV file can’t be played here.");
  });
});
