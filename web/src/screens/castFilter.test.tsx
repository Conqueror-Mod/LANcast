import { describe, it, expect } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { CastRow } from "./CastRow";

/*
 * A cast face is a filter control, and this asserts that it actually goes
 * somewhere.
 *
 * Written because three of the five features in v0.8.18 shipped built but
 * unreachable — a store with no route, an endpoint with no interface, a prompt
 * rendered into hidden chrome — and every one of them passed a test suite that
 * was only ever asked about the half that existed. "Is this wired to anything"
 * is the question that was missing, so it is the question here.
 */
function render(node: React.ReactNode) {
  return renderToStaticMarkup(<MemoryRouter>{node}</MemoryRouter>);
}

describe("clicking a cast member", () => {
  it("links to that actor within the item's own library", () => {
    const html = render(
      <CastRow
        libraryID={4}
        cast={[
          { name: "Ada Vance", role: "actor", person_id: 12, thumb: "abc" },
        ]}
      />,
    );
    expect(html).toContain('href="/library/4?actor=12"');
  });

  /*
   * `actor`, not `person`.
   *
   * Somebody clicking a name in a *cast* list means "the films they are in",
   * not "the films they had any hand in" — otherwise an actor-director brings
   * their own direction back with them. Both filters exist on the server and
   * that difference is the only reason there are two.
   */
  it("filters by the acting credit rather than any credit", () => {
    const html = render(
      <CastRow
        libraryID={4}
        cast={[{ name: "Ada Vance", role: "actor", person_id: 12 }]}
      />,
    );
    expect(html).toContain("actor=12");
    expect(html).not.toContain("person=12");
  });

  /*
   * A provider that gave a name and no id leaves nothing to filter on. The card
   * still renders — the name is worth showing — but it must not look clickable,
   * because a control that goes nowhere is worse than no control.
   */
  it("does not pretend to be clickable without an id", () => {
    const html = render(
      <CastRow libraryID={4} cast={[{ name: "Nobody Known", role: "actor" }]} />,
    );
    expect(html).toContain("Nobody Known");
    expect(html).not.toContain("<a ");
  });

  // The same, for an item whose library is somehow unknown: there is no grid to
  // send anyone to.
  it("does not link when there is no library to filter", () => {
    const html = render(
      <CastRow cast={[{ name: "Ada Vance", role: "actor", person_id: 12 }]} />,
    );
    expect(html).not.toContain("<a ");
  });

  // Initials stand in for a missing headshot, so a row of twelve is one shape
  // rather than three pictures and nine gaps.
  it("falls back to initials, two letters at most", () => {
    const html = render(
      <CastRow
        libraryID={4}
        cast={[{ name: "Jamie Lee Curtis", role: "actor", person_id: 3 }]}
      />,
    );
    expect(html).toContain("JC");
  });
});
