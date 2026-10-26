/*
 * The films a collection holds that the library does not (ADR 0093), drawn
 * dimmed and not pressable, with an announced film saying so.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MissingFilms } from "./MissingFilms";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("missing films", () => {
  it("names each one with its year, or says when it is coming", () => {
    act(() =>
      root.render(
        <MissingFilms
          films={[
            { external_id: "2", title: "The Two Towers", year: 2002, poster: "abc" },
            { external_id: "9", title: "The Next One", year: 2027, upcoming: true },
            { external_id: "10", title: "Untitled", upcoming: true },
          ]}
        />,
      ),
    );
    expect(host.querySelector(".section-label")?.textContent).toBe("Not in your library");
    const years = [...host.querySelectorAll(".missing-film__year")].map((e) => e.textContent);
    expect(years).toEqual(["2002", "Coming 2027", "Not yet released"]);
    expect(host.querySelector("img")?.getAttribute("src")).toBe("/api/artwork/abc?size=poster");
    // Nothing to open, so nothing pressable.
    expect(host.querySelector("button, a")).toBeNull();
  });

  it("draws nothing when every film is here", () => {
    act(() => root.render(<MissingFilms films={[]} />));
    expect(host.innerHTML).toBe("");
  });
});
