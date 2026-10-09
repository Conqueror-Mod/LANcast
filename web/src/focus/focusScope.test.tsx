/*
 * Focus scopes: while a menu is open, arrows reach only what is inside it.
 *
 * jsdom performs no layout, so each button is given a position by hand —
 * a menu of two above a corner card's button, which is the arrangement the
 * game menu has over a docked film. The first test is the control: with no
 * scope, Down from the menu's last item lands on the card, which is the bug.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { FocusProvider, useFocusable, useFocusScope } from "./FocusController";

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

function Btn({ label, y }: { label: string; y: number }) {
  const f = useFocusable(() => {});
  return (
    <button
      ref={(el) => {
        f.ref(el);
        if (el) el.getBoundingClientRect = () => new DOMRect(100, y, 120, 30);
      }}
      tabIndex={f.tabIndex}
      data-focus-id={f["data-focus-id"]}
    >
      {label}
    </button>
  );
}

function Menu({ scoped }: { scoped: boolean }) {
  const scope = useFocusScope();
  return (
    <div ref={scoped ? scope : undefined}>
      <Btn label="Resume" y={100} />
      <Btn label="Quit" y={150} />
    </div>
  );
}

async function render(scoped: boolean) {
  await act(async () => {
    root.render(
      <FocusProvider>
        <Menu scoped={scoped} />
        <Btn label="Card size" y={400} />
      </FocusProvider>,
    );
  });
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === label)!;

function key(k: string) {
  act(() => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true }));
  });
}

describe("a focus scope", () => {
  it("without one, Down from the menu reaches the card behind it", async () => {
    await render(false);
    act(() => button("Quit").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(button("Card size"));
  });

  it("keeps Down inside the menu", async () => {
    await render(true);
    act(() => button("Resume").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(button("Quit"));
    key("ArrowDown");
    expect(document.activeElement).toBe(button("Quit"));
  });

  it("brings focus that is outside back into the menu on the next move", async () => {
    await render(true);
    act(() => button("Card size").focus());
    key("ArrowUp");
    expect(document.activeElement).toBe(button("Resume"));
  });
});
