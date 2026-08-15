/*
 * The pop-out's DOM work, tested where it is testable.
 *
 * `documentPictureInPicture` does not exist in jsdom and cannot be
 * meaningfully faked — a real second window with its own document is the
 * browser's job. What *is* ours, and is what breaks, is the two pieces of DOM
 * work either side of that API: adopting the element into another document
 * without losing its identity, and copying the stylesheets so the window is not
 * unstyled.
 *
 * Both are exercised here against a second document made with
 * `implementation.createHTMLDocument`, which is a genuinely separate document
 * with its own node registry — the same condition that makes the move
 * interesting.
 *
 * The React reconciliation risk, which is the larger one, is covered by
 * crossDocumentMove.test.tsx against the real provider.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { copyStyles, moveElement, popoutSupported } from "./popout";

let other: Document;

beforeEach(() => {
  document.head.innerHTML = "";
  document.body.innerHTML = "";
  other = document.implementation.createHTMLDocument("popout");
});

describe("moving the element between documents", () => {
  it("adopts the node rather than copying it", () => {
    const video = document.createElement("video");
    const home = document.createElement("div");
    home.appendChild(video);
    document.body.appendChild(home);

    const stage = other.createElement("div");
    other.body.appendChild(stage);

    moveElement(video, stage);

    // Identity is the whole point: a copy would be a different element with no
    // decoder state, which is the same as unmounting — and unmounting stops the
    // sound, which is what this architecture exists to prevent.
    expect(stage.firstChild).toBe(video);
    expect(video.ownerDocument).toBe(other);
    expect(home.childNodes.length).toBe(0);
  });

  it("brings the same node home again", () => {
    const video = document.createElement("video");
    video.dataset.marker = "the-one";
    const home = document.createElement("div");
    home.appendChild(video);
    document.body.appendChild(home);
    const stage = other.createElement("div");
    other.body.appendChild(stage);

    moveElement(video, stage);
    moveElement(video, home);

    expect(home.firstChild).toBe(video);
    expect(video.ownerDocument).toBe(document);
    expect((home.firstChild as HTMLElement).dataset.marker).toBe("the-one");
  });

  // Moving within one document must not go through adoptNode, and must still
  // work: the return trip after the window closes is exactly this case.
  it("is a no-op-safe move inside one document", () => {
    const video = document.createElement("video");
    const a = document.createElement("div");
    const b = document.createElement("div");
    a.appendChild(video);
    document.body.append(a, b);

    moveElement(video, b);

    expect(b.firstChild).toBe(video);
    expect(a.childNodes.length).toBe(0);
  });
});

describe("copying styles into the window", () => {
  it("carries rules across, because the window has none of its own", () => {
    const style = document.createElement("style");
    style.textContent = ".popout__bar { color: rgb(1, 2, 3); }";
    document.head.appendChild(style);

    copyStyles(document, other);

    expect(other.head.textContent).toContain(".popout__bar");
    // And a floor of its own: a window with no background opens white behind a
    // letterboxed picture, which reads as broken rather than as dark.
    expect(other.head.textContent).toContain("background: #05070f");
  });

  /*
   * A cross-origin sheet throws on `cssRules`, and it must not take the window
   * with it. A pop-out missing one third-party stylesheet is cosmetic; a
   * pop-out that fails to open is a broken feature.
   */
  it("survives a stylesheet it is not allowed to read", () => {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "https://example.invalid/fonts.css";

    // A stand-in source document, because the real styleSheets list is a proxy
    // that cannot be given a throwing entry — and the throwing entry is the
    // whole case under test.
    const hostile = {
      styleSheets: [
        {
          get cssRules(): CSSRuleList {
            throw new DOMException("cross-origin", "SecurityError");
          },
          ownerNode: link,
        },
      ],
    } as unknown as Document;

    expect(() => copyStyles(hostile, other)).not.toThrow();
    // The unreadable sheet is re-linked by href rather than dropped, so a
    // font still arrives even though its rules could not be read.
    const copied = other.head.querySelector<HTMLLinkElement>('link[rel="stylesheet"]');
    expect(copied?.href).toBe("https://example.invalid/fonts.css");
  });
});

describe("feature detection", () => {
  // Chromium-only, and the button is hidden rather than dead where it is
  // missing — the rule the control bar follows everywhere else.
  it("reports absence in a host without the API", () => {
    expect(popoutSupported()).toBe(false);
  });

  it("reports presence when the host has it", () => {
    (window as unknown as { documentPictureInPicture: object }).documentPictureInPicture =
      {};
    expect(popoutSupported()).toBe(true);
    delete (window as unknown as { documentPictureInPicture?: object })
      .documentPictureInPicture;
  });
});
