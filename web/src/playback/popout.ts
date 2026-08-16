/*
 * Document picture-in-picture: our window, not the browser's (ADR 0029).
 *
 * `video.requestPictureInPicture()` hands the element to the browser, which
 * then draws the window — its frame, its transport, its idea of what captions
 * are. That is why popping out costs every control except play and seek, why
 * our subtitles keep rendering in the parent tab while the picture is in the
 * corner, and why Chrome offers speech-recognition captions in place of the
 * real tracks.
 *
 * `documentPictureInPicture.requestWindow()` opens an always-on-top window
 * containing *our* DOM instead. This module owns the two things that are pure
 * DOM work — opening the window with our styles in it, and moving the media
 * element across and back. Everything React does about it lives in the
 * provider.
 *
 * The move is imperative and outside React's reconciliation, which the ADR
 * establishes is safe only because the element is the sole child of a slot
 * whose children React never varies. That constraint is load-bearing; see
 * crossDocumentMove.test.tsx, which holds both the safe shape and the shape
 * that throws.
 */

// The API is Chromium-only and not in TypeScript's DOM lib.
interface DocumentPiP {
  requestWindow(options?: {
    width?: number;
    height?: number;
    disallowReturnToOpener?: boolean;
    preferInitialWindowPlacement?: boolean;
  }): Promise<Window>;
  window: Window | null;
}

declare global {
  interface Window {
    documentPictureInPicture?: DocumentPiP;
  }
}

export function popoutSupported(): boolean {
  return typeof window !== "undefined" && !!window.documentPictureInPicture;
}

/*
 * Stylesheets do not follow the window; without this it opens unstyled.
 *
 * Two kinds have to be handled separately. A built bundle serves one
 * `<link rel="stylesheet">`, which is cloned. Vite's dev server injects
 * `<style>` elements instead, and those are copied through `cssRules` — which
 * is also why the try/catch is here rather than being an oversight: reading
 * `cssRules` of a cross-origin sheet throws, and a font stylesheet from a CDN
 * would take the whole window down with it. A pop-out missing one third-party
 * sheet is a cosmetic problem; a pop-out that fails to open is a broken
 * feature.
 */
export function copyStyles(source: Document, target: Document): void {
  for (const sheet of Array.from(source.styleSheets)) {
    try {
      const rules = Array.from(sheet.cssRules)
        .map((r) => r.cssText)
        .join("\n");
      const style = target.createElement("style");
      style.textContent = rules;
      target.head.appendChild(style);
    } catch {
      const link = sheet.ownerNode as HTMLLinkElement | null;
      if (link?.href) {
        const copy = target.createElement("link");
        copy.rel = "stylesheet";
        copy.href = link.href;
        target.head.appendChild(copy);
      }
    }
  }

  // The window is its own document with its own background. Without this it
  // opens white behind a letterboxed picture, which reads as a broken window
  // rather than a dark one.
  const base = target.createElement("style");
  base.textContent = `
    html, body { margin: 0; height: 100%; background: #05070f; overflow: hidden;
                 color: #f2f5fb; font-family: Inter, system-ui, sans-serif; }
    * { box-sizing: border-box; }
  `;
  target.head.appendChild(base);
}

/**
 * openPopout opens the window and prepares it. The caller mounts its own React
 * tree into the returned root and moves the media element in.
 *
 * The size is the picture's own aspect where it is known, so the window opens
 * at the shape of what is in it rather than at a default the video is then
 * letterboxed inside.
 */
export async function openPopout(
  aspect: number | undefined,
): Promise<{ win: Window; root: HTMLElement } | null> {
  const api = window.documentPictureInPicture;
  if (!api) return null;

  const width = 480;
  const height = Math.round(width / (aspect && aspect > 0.2 ? aspect : 16 / 9));

  const win = await api.requestWindow({ width, height });
  copyStyles(document, win.document);

  const root = win.document.createElement("div");
  root.className = "popout";
  win.document.body.appendChild(root);
  return { win, root };
}

/**
 * moveElement adopts a node into another document and appends it to a parent.
 *
 * `adoptNode` first, deliberately: appending across documents adopts
 * implicitly, but doing it explicitly is what makes the intent legible at the
 * one point in this codebase where an element changes document. The element
 * keeps its identity and keeps playing across the move — which is the property
 * the whole feature rests on, and the one the acceptance test exists to prove.
 */
export function moveElement(el: HTMLElement, parent: HTMLElement): void {
  const doc = parent.ownerDocument;
  if (el.ownerDocument !== doc) doc.adoptNode(el);
  parent.appendChild(el);
}
